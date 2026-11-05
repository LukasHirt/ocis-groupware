package migrate

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// listNotMigrated records what is left behind on purpose. Data that belongs
// to a user is listed when the user is in scope; what belongs to the system
// or to a group is listed only on a run for everyone.
func (r *runner) listNotMigrated(cals []classic.Calendar, books []classic.AddressBook, subs []classic.Subscription,
	shares []classic.Share, inScope map[string]bool, filtered bool) {
	add := func(n NotMigrated) { r.report.NotMigrated = append(r.report.NotMigrated, n) }
	relevant := func(p classic.Principal) bool {
		if p.Kind == classic.PrincipalUser {
			return inScope[p.Name]
		}
		return !filtered
	}
	for _, c := range cals {
		switch {
		case !relevant(c.Owner):
		case c.URI == birthdayCalendar:
			add(NotMigrated{What: "birthday calendar", Owner: c.Owner.Name, Name: c.URI, Detail: "classic generates it from the birthdays on contacts"})
		case c.Owner.Kind != classic.PrincipalUser:
			add(NotMigrated{What: "calendar of a " + string(c.Owner.Kind), Owner: c.Owner.Name, Name: c.URI, Detail: "only users own data here"})
		case !r.knownUser(c.Owner.Name):
			add(NotMigrated{What: "calendar", Owner: c.Owner.Name, Name: c.URI, Detail: "the owner is not an account of the classic installation"})
		}
	}
	for _, b := range books {
		switch {
		case !relevant(b.Owner):
		case b.Owner.Kind == classic.PrincipalSystem:
			add(NotMigrated{What: "system address book", Name: b.URI, Detail: "classic generates it from the list of users"})
		case b.Owner.Kind != classic.PrincipalUser:
			add(NotMigrated{What: "address book of a " + string(b.Owner.Kind), Owner: b.Owner.Name, Name: b.URI, Detail: "only users own data here"})
		case !r.knownUser(b.Owner.Name):
			add(NotMigrated{What: "address book", Owner: b.Owner.Name, Name: b.URI, Detail: "the owner is not an account of the classic installation"})
		}
	}
	for _, s := range subs {
		if relevant(s.Owner) {
			name := s.DisplayName
			if name == "" {
				name = s.URI
			}
			add(NotMigrated{What: "subscription", Owner: s.Owner.Name, Name: name, Detail: s.Source})
		}
	}
	for _, s := range shares {
		if s.PublicURI == "" {
			continue
		}
		name, owner, ok := resourceName(s, cals, books)
		if !ok || !relevant(owner) {
			continue
		}
		add(NotMigrated{What: "public link", Owner: owner.Name, Name: name, Detail: "recreate it in the new service if it is still needed"})
	}
}

func (r *runner) knownUser(id string) bool { _, ok := r.users[id]; return ok }

func resourceName(s classic.Share, cals []classic.Calendar, books []classic.AddressBook) (string, classic.Principal, bool) {
	switch s.Kind {
	case classic.ResourceCalendar:
		for _, c := range cals {
			if c.ID == s.ResourceID {
				return c.URI, c.Owner, true
			}
		}
	case classic.ResourceAddressBook:
		for _, b := range books {
			if b.ID == s.ResourceID {
				return b.URI, b.Owner, true
			}
		}
	}
	return "", classic.Principal{}, false
}

// migrateShares writes the shares of the collections that were copied. A
// share is added to what the collection has: members that someone gave
// access since an earlier run stay.
func (r *runner) migrateShares(ctx context.Context, shares []classic.Share, cals []classic.Calendar, books []classic.AddressBook, inScope map[string]bool) error {
	for _, s := range shares {
		if s.PublicURI != "" {
			continue
		}
		name, owner, ok := resourceName(s, cals, books)
		if !ok || owner.Kind != classic.PrincipalUser || !inScope[owner.Name] {
			continue
		}
		rep := ShareReport{Owner: owner.Name, Kind: string(s.Kind), Collection: name, Access: string(s.Access)}
		var tg target
		var have bool
		if s.Kind == classic.ResourceCalendar {
			tg, have = r.cals[s.ResourceID]
		} else {
			tg, have = r.books[s.ResourceID]
		}

		var member sharing.Member
		switch s.Target.Kind {
		case classic.PrincipalUser:
			rep.Grantee = "user " + s.Target.Name
			member.Type = sharing.User
			member.ID, member.Name = r.ids.Users[s.Target.Name], s.Target.Name
			if u, ok := r.users[s.Target.Name]; ok && u.DisplayName != "" {
				member.Name = u.DisplayName
			}
		case classic.PrincipalGroup:
			rep.Grantee = "group " + s.Target.Name
			member.Type = sharing.Group
			member.ID, member.Name = r.ids.Groups[s.Target.Name], s.Target.Name
		default:
			rep.Grantee = string(s.Target.Kind) + " " + s.Target.Name
			rep.Status, rep.Reason = StatusSkipped, "only users and groups can receive a share"
			r.report.Shares = append(r.report.Shares, rep)
			continue
		}
		member.Access = sharing.Read
		if s.Access == classic.ReadWrite {
			member.Access = sharing.ReadWrite
		}

		switch {
		case !have:
			rep.Status, rep.Reason = StatusSkipped, "the collection was not migrated"
		case member.ID == "":
			rep.Status, rep.Reason = StatusSkipped, "the recipient has no oCIS counterpart"
		default:
			if err := r.grant(ctx, tg, member, &rep); err != nil {
				return err
			}
		}
		r.report.Shares = append(r.report.Shares, rep)
	}
	return nil
}

func (r *runner) grant(ctx context.Context, tg target, m sharing.Member, rep *ShareReport) error {
	status := StatusCreated
	if i := slices.IndexFunc(r.cfg.Shares.Members(tg.ref), func(x sharing.Member) bool { return x.Type == m.Type && x.ID == m.ID }); i >= 0 {
		if r.cfg.Shares.Members(tg.ref)[i].Access == m.Access {
			rep.Status = StatusExisting
			return nil
		}
		status = StatusUpdated
	}
	if r.cfg.DryRun {
		rep.Status = status
		return nil
	}
	err := r.cfg.Shares.Grant(ctx, tg.ref, tg.id, m)
	switch {
	case err == nil:
		rep.Status = status
	case errors.Is(err, sharing.ErrSelfShare):
		rep.Status, rep.Reason = StatusSkipped, "the recipient is the owner"
	case errors.Is(err, sharing.ErrInvalidMember), errors.Is(err, sharing.ErrTooManyMembers):
		rep.Status, rep.Reason = StatusFailed, err.Error()
	default:
		return fmt.Errorf("migrate: share %s/%s: %w", tg.ref.Owner, tg.ref.Name, err)
	}
	return nil
}
