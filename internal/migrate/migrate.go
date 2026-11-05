// Package migrate copies the calendars, address books, shares and app tokens
// of a classic installation into the data directory of the service.
//
// A run reads a classic.Source and writes through the file store, the
// principals registry and the shares of the data directory, so the service
// must not run at the same time. It can be repeated: what a run already
// copied is recognised by owner, collection name, object name and content
// and is left alone, so a second run adds only what is new. Nothing in the
// target is overwritten or deleted.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/LukasHirt/groupkit/store"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
	"github.com/owncloud/ocis-groupware/internal/migrate/idmap"
	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// OwnerPrefix starts the owner of every principal the migration creates. The
// rest is the user's oCIS id, which stays the same however often the
// migration runs.
const OwnerPrefix = "migrated-"

// Source is what a run reads. *classic.Reader implements it.
type Source interface {
	Users(ctx context.Context) ([]classic.User, error)
	Groups(ctx context.Context) ([]classic.Group, error)
	Calendars(ctx context.Context) ([]classic.Calendar, error)
	CalendarObjects(ctx context.Context, calendarID int64, fn func(classic.Object) error) error
	AddressBooks(ctx context.Context) ([]classic.AddressBook, error)
	Cards(ctx context.Context, addressBookID int64, fn func(classic.Card) error) error
	Subscriptions(ctx context.Context) ([]classic.Subscription, error)
	Shares(ctx context.Context) ([]classic.Share, error)
	AppTokens(ctx context.Context) ([]classic.AppToken, error)
}

// Mapper maps classic ids to oCIS ids. *idmap.Mapper implements it.
type Mapper interface {
	Map(ctx context.Context, userIDs, groupIDs []string) (idmap.Result, error)
}

// Config holds what Run needs.
type Config struct {
	Source   Source
	Mapper   Mapper
	Store    store.Store
	Registry *principals.Registry
	Shares   *sharing.Shares
	// Users limits the run to these classic user ids. Empty means everyone.
	Users []string
	// DryRun computes the report and writes nothing.
	DryRun bool
	// LegacyTokensEnabled says whether the service is configured to check
	// imported tokens. Without it they are stored but never work, and the
	// report says so.
	LegacyTokensEnabled bool
	Logger              *slog.Logger
}

type runner struct {
	cfg    Config
	log    *slog.Logger
	report *Report

	users  map[string]classic.User
	groups map[string][]string // user id to the groups they are in
	ids    idmap.Result
	// owners maps a classic user id to the owner the data goes to.
	owners map[string]string
	// colls remembers, per classic calendar or address book, where it went.
	cals  map[int64]target
	books map[int64]target
}

// target is the collection a classic calendar or address book was copied
// to. ID is empty in a dry run for a collection that does not exist yet.
type target struct {
	ref store.CollectionRef
	id  string
}

// Run performs the migration and returns the report. It stops at the first
// error that makes continuing pointless, such as a source that cannot be read
// or a directory that refuses the token. Problems with single items are in
// the report and do not stop the run.
func Run(ctx context.Context, cfg Config) (*Report, error) {
	switch {
	case cfg.Source == nil, cfg.Mapper == nil, cfg.Store == nil, cfg.Registry == nil, cfg.Shares == nil:
		return nil, errors.New("migrate: source, mapper, store, registry and shares are required")
	}
	r := &runner{
		cfg: cfg, log: cfg.Logger, report: &Report{DryRun: cfg.DryRun},
		users: map[string]classic.User{}, groups: map[string][]string{},
		owners: map[string]string{}, cals: map[int64]target{}, books: map[int64]target{},
	}
	if r.log == nil {
		r.log = slog.New(slog.DiscardHandler)
	}
	if err := r.run(ctx); err != nil {
		return nil, err
	}
	r.report.finish()
	return r.report, nil
}

func (r *runner) run(ctx context.Context) error {
	src := r.cfg.Source
	users, err := src.Users(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read users: %w", err)
	}
	groups, err := src.Groups(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read groups: %w", err)
	}
	cals, err := src.Calendars(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read calendars: %w", err)
	}
	books, err := src.AddressBooks(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read address books: %w", err)
	}
	subs, err := src.Subscriptions(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read subscriptions: %w", err)
	}
	shares, err := src.Shares(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read shares: %w", err)
	}
	tokens, err := src.AppTokens(ctx)
	if err != nil {
		return fmt.Errorf("migrate: read app tokens: %w", err)
	}

	for _, u := range users {
		r.users[u.ID] = u
	}
	for _, g := range groups {
		for _, m := range g.Members {
			r.groups[m] = append(r.groups[m], g.ID)
		}
	}
	selected, err := r.selectUsers()
	if err != nil {
		return err
	}
	inScope := map[string]bool{}
	for _, id := range selected {
		inScope[id] = true
	}
	filtered := len(r.cfg.Users) > 0

	if err := r.mapIDs(ctx, selected, shares, cals, books, inScope); err != nil {
		return err
	}

	r.listNotMigrated(cals, books, subs, shares, inScope, filtered)

	for _, login := range selected {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.migrateUser(ctx, login, cals, books, tokens); err != nil {
			return err
		}
	}
	return r.migrateShares(ctx, shares, cals, books, inScope)
}

// selectUsers returns the classic ids the run covers, sorted. Disabled
// accounts are listed in the report and left out.
func (r *runner) selectUsers() ([]string, error) {
	var want []string
	if len(r.cfg.Users) == 0 {
		for id := range r.users {
			want = append(want, id)
		}
	} else {
		var unknown []string
		for _, id := range r.cfg.Users {
			if _, ok := r.users[id]; !ok {
				unknown = append(unknown, id)
				continue
			}
			want = append(want, id)
		}
		if len(unknown) > 0 {
			return nil, fmt.Errorf("migrate: not a user of the classic installation: %s", strings.Join(unknown, ", "))
		}
	}
	slices.Sort(want)
	want = slices.Compact(want)
	var out []string
	for _, id := range want {
		if !r.users[id].Enabled {
			r.report.User(id).Skip("the account is disabled")
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

// mapIDs maps the users in scope, the recipients of their shares and the
// groups they are in.
func (r *runner) mapIDs(ctx context.Context, selected []string, shares []classic.Share, cals []classic.Calendar, books []classic.AddressBook, inScope map[string]bool) error {
	userIDs := map[string]bool{}
	groupIDs := map[string]bool{}
	for _, id := range selected {
		userIDs[id] = true
		for _, g := range r.groups[id] {
			groupIDs[g] = true
		}
	}
	for _, s := range shares {
		if s.PublicURI != "" || !inScope[ownerOf(s, cals, books)] {
			continue
		}
		switch s.Target.Kind {
		case classic.PrincipalUser:
			userIDs[s.Target.Name] = true
		case classic.PrincipalGroup:
			groupIDs[s.Target.Name] = true
		}
	}
	res, err := r.cfg.Mapper.Map(ctx, sortedKeys(userIDs), sortedKeys(groupIDs))
	if err != nil {
		return fmt.Errorf("migrate: map users and groups: %w", err)
	}
	r.ids = res
	for _, u := range res.Unmapped {
		r.report.Unmapped = append(r.report.Unmapped, UnmappedID{Kind: string(u.Kind), ID: u.ID, Reason: string(u.Reason)})
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// ownerOf returns the classic user id that owns the resource a share points
// at, or "" if it is not a user's.
func ownerOf(s classic.Share, cals []classic.Calendar, books []classic.AddressBook) string {
	var p classic.Principal
	found := false
	switch s.Kind {
	case classic.ResourceCalendar:
		for _, c := range cals {
			if c.ID == s.ResourceID {
				p, found = c.Owner, true
			}
		}
	case classic.ResourceAddressBook:
		for _, b := range books {
			if b.ID == s.ResourceID {
				p, found = b.Owner, true
			}
		}
	}
	if !found || p.Kind != classic.PrincipalUser {
		return ""
	}
	return p.Name
}
