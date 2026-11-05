package migrate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LukasHirt/groupkit/ical"
	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/vcard"

	"github.com/owncloud/ocis-groupware/internal/migrate/classic"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

// maxProblems caps the problems listed for one collection. The counts stay
// exact.
const maxProblems = 20

// birthdayCalendar is the URI of the calendar classic generates from the
// birthdays on contacts.
const birthdayCalendar = "contact_birthdays"

func (r *runner) migrateUser(ctx context.Context, login string, cals []classic.Calendar, books []classic.AddressBook, tokens []classic.AppToken) error {
	rep := r.report.User(login)
	ocisID, ok := r.ids.Users[login]
	if !ok {
		rep.Skip("no oCIS user for this login, its data is not migrated")
		return nil
	}
	rep.OCISID = ocisID
	owner, created, reason, err := r.principalFor(ctx, r.users[login], ocisID)
	if err != nil {
		return fmt.Errorf("migrate: user %s: %w", login, err)
	}
	if reason != "" {
		rep.Skip(reason)
		return nil
	}
	rep.Owner = owner
	r.owners[login] = owner
	rep.Status = StatusExisting
	if created {
		rep.Status = StatusCreated
	}

	for _, c := range cals {
		if c.Owner.Kind != classic.PrincipalUser || c.Owner.Name != login || c.URI == birthdayCalendar {
			continue
		}
		props := store.Props{
			DisplayName: c.DisplayName, Description: c.Description, Color: c.Color,
			Order: int(c.Order), Timezone: c.Timezone,
			// The service holds events only.
			Components: []string{"VEVENT"},
		}
		ref := store.CollectionRef{Owner: owner, Kind: store.KindCalendar, Name: c.URI}
		cr, tg, err := r.copyCollection(ctx, ref, props, func(yield func(string, []byte) error) error {
			return r.cfg.Source.CalendarObjects(ctx, c.ID, func(o classic.Object) error { return yield(o.URI, o.Data) })
		}, func(data []byte) (string, string, error) {
			info, err := ical.Validate(data, []string{"VEVENT"})
			if errors.Is(err, ical.ErrUnsupportedComponent) {
				return "", "not an event", nil
			}
			return info.UID, "", err
		})
		if err != nil {
			return err
		}
		rep.Calendars = append(rep.Calendars, cr)
		if cr.Status != StatusFailed {
			r.cals[c.ID] = tg
		}
	}
	for _, b := range books {
		if b.Owner.Kind != classic.PrincipalUser || b.Owner.Name != login {
			continue
		}
		props := store.Props{DisplayName: b.DisplayName, Description: b.Description}
		ref := store.CollectionRef{Owner: owner, Kind: store.KindAddressBook, Name: b.URI}
		cr, tg, err := r.copyCollection(ctx, ref, props, func(yield func(string, []byte) error) error {
			return r.cfg.Source.Cards(ctx, b.ID, func(c classic.Card) error { return yield(c.URI, c.Data) })
		}, func(data []byte) (string, string, error) {
			uid, err := vcard.Validate(data)
			return uid, "", err
		})
		if err != nil {
			return err
		}
		rep.AddressBooks = append(rep.AddressBooks, cr)
		if cr.Status != StatusFailed {
			r.books[b.ID] = tg
		}
	}
	return r.importTokens(ctx, login, owner, tokens)
}

// principalFor finds or creates the principal the user's data goes to. A
// user who signed in to the service already is found by oCIS id, so the data
// lands next to what they have. reason is set, and nothing is done, when the
// user cannot be migrated.
func (r *runner) principalFor(ctx context.Context, u classic.User, ocisID string) (owner string, created bool, reason string, err error) {
	reg := r.cfg.Registry
	candidate := OwnerPrefix + ocisID
	found := ""
	if _, err := reg.Get(candidate); err == nil {
		found = candidate
	} else {
		for _, p := range reg.ListPrincipals(ctx) {
			if id, err := reg.Identity(p.Owner); err == nil && id.OCISID == ocisID {
				found = p.Owner
				break
			}
		}
	}
	if found != "" {
		p, err := reg.Get(found)
		if err != nil {
			return "", false, "", err
		}
		if p.Disabled {
			return "", false, "the user is disabled in the service", nil
		}
		return found, false, "", nil
	}
	if holder, ok := reg.LookupUsername(u.ID); ok {
		return "", false, fmt.Sprintf("the name %q belongs to another user of the service (owner %s)", u.ID, holder), nil
	}
	if r.cfg.DryRun {
		return candidate, true, "", nil
	}
	var groups []principals.Group
	for _, g := range r.groups[u.ID] {
		if id, ok := r.ids.Groups[g]; ok {
			groups = append(groups, principals.Group{ID: id, Name: g})
		}
	}
	name := u.DisplayName
	if name == "" {
		name = u.ID
	}
	_, created, err = reg.Import(ctx, candidate, u.ID, name, principals.Identity{OCISID: ocisID, Groups: groups, At: time.Now()})
	switch {
	case errors.Is(err, principals.ErrUsernameTaken):
		return "", false, err.Error(), nil
	case err != nil:
		return "", false, "", err
	}
	return candidate, created, "", nil
}

// copyCollection creates the collection if it is missing and copies the
// objects that are not there yet. read yields the classic objects and check
// validates one the way the DAV engine does: it returns the UID, a reason to
// skip the object, or an error for an object that is not valid.
func (r *runner) copyCollection(ctx context.Context, ref store.CollectionRef, props store.Props,
	read func(yield func(name string, data []byte) error) error,
	check func(data []byte) (uid, skip string, err error)) (CollectionReport, target, error) {

	rep := CollectionReport{Name: ref.Name, DisplayName: props.DisplayName}
	have := map[string]string{}
	coll, err := r.cfg.Store.GetCollection(ctx, ref)
	switch {
	case err == nil:
		rep.Status = StatusExisting
		objs, err := r.cfg.Store.ListObjects(ctx, ref, store.ListOptions{})
		if err != nil {
			return rep, target{}, fmt.Errorf("migrate: list %s/%s: %w", ref.Owner, ref.Name, err)
		}
		for _, o := range objs {
			have[o.Name] = o.ETag
		}
	case errors.Is(err, store.ErrNotFound):
		rep.Status = StatusCreated
		if !r.cfg.DryRun {
			coll, err = r.cfg.Store.CreateCollection(ctx, ref, props)
			if err != nil {
				if errors.Is(err, store.ErrInvalidName) {
					rep.Status, rep.Reason = StatusFailed, err.Error()
					return rep, target{}, nil
				}
				return rep, target{}, fmt.Errorf("migrate: create %s/%s: %w", ref.Owner, ref.Name, err)
			}
		}
	default:
		return rep, target{}, fmt.Errorf("migrate: look up %s/%s: %w", ref.Owner, ref.Name, err)
	}

	problem := func(name, why string) {
		if len(rep.Problems) < maxProblems {
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s: %s", name, why))
		} else if len(rep.Problems) == maxProblems {
			rep.Problems = append(rep.Problems, "more problems are not listed")
		}
	}
	err = read(func(name string, data []byte) error {
		etag := store.ETag(data)
		if old, ok := have[name]; ok {
			if old == etag {
				rep.Objects.Unchanged++
			} else {
				rep.Objects.Different++
			}
			return nil
		}
		uid, skip, err := check(data)
		switch {
		case err != nil:
			rep.Objects.Failed++
			problem(name, err.Error())
			return nil
		case skip != "":
			rep.Objects.Skipped++
			problem(name, skip)
			return nil
		}
		if r.cfg.DryRun {
			rep.Objects.Added++
			return nil
		}
		_, err = r.cfg.Store.PutObject(ctx, ref, name, data, store.PutOptions{IfNoneMatch: true, UID: uid})
		var conflict *store.ConflictError
		if errors.As(err, &conflict) {
			// Two objects with one UID: keep both, the second without an
			// entry in the UID index.
			problem(name, fmt.Sprintf("uid %q is also used by %q; copied anyway", uid, conflict.Name))
			_, err = r.cfg.Store.PutObject(ctx, ref, name, data, store.PutOptions{IfNoneMatch: true})
		}
		switch {
		case err == nil:
			rep.Objects.Added++
		case errors.Is(err, store.ErrInvalidName), errors.Is(err, store.ErrExists):
			rep.Objects.Failed++
			problem(name, err.Error())
		default:
			return fmt.Errorf("write %s/%s/%s: %w", ref.Owner, ref.Name, name, err)
		}
		return nil
	})
	if err != nil {
		return rep, target{}, fmt.Errorf("migrate: %s/%s: %w", ref.Owner, ref.Name, err)
	}
	return rep, target{ref: ref, id: coll.ID}, nil
}

// importTokens hands the user's app tokens to the registry. An earlier run's
// tokens are found by name, because a token that was used once no longer has
// the digest it was imported with. Names can repeat, so they are counted.
func (r *runner) importTokens(ctx context.Context, login, owner string, tokens []classic.AppToken) error {
	rep := &r.report.User(login).Tokens
	have := map[string]int{}
	if existing, err := r.cfg.Registry.ListAppPasswords(owner); err == nil {
		for _, ap := range existing {
			have[ap.Name]++
		}
	} else if !errors.Is(err, principals.ErrNotFound) {
		return fmt.Errorf("migrate: list app passwords of %s: %w", login, err)
	}
	for _, t := range tokens {
		if t.User != login {
			continue
		}
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = "Classic app token"
		}
		if have[name] > 0 {
			have[name]--
			rep.Existing++
			continue
		}
		if r.cfg.DryRun {
			rep.Imported++
			continue
		}
		_, err := r.cfg.Registry.ImportLegacyToken(ctx, owner, name, t.Digest, t.LastActivity)
		switch {
		case err == nil:
			rep.Imported++
		case errors.Is(err, principals.ErrInvalidLegacyToken), errors.Is(err, principals.ErrInvalidAppPasswordName),
			errors.Is(err, principals.ErrTooManyAppPasswords):
			rep.Failed++
			rep.Problems = append(rep.Problems, fmt.Sprintf("%s: %v", name, err))
		default:
			return fmt.Errorf("migrate: import app token of %s: %w", login, err)
		}
	}
	if rep.Imported > 0 && !r.cfg.LegacyTokensEnabled {
		r.report.warn("app tokens were imported but GROUPWARE_LEGACY_TOKEN_SECRET is not set; they will not sign in until the service is started with the instance secret of the classic installation")
	}
	return nil
}
