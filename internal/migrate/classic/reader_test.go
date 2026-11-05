package classic

import (
	"context"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

func TestReaderUsersAndGroups(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Reader, fx fixtureInfo) {
		ctx := context.Background()
		users, err := r.Users(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(users) != fx.Rows["oc_accounts"] {
			t.Fatalf("%d users, want %d", len(users), fx.Rows["oc_accounts"])
		}
		by := map[string]User{}
		for _, u := range users {
			by[u.ID] = u
		}
		alice := by["alice"]
		if alice.Email != "alice@example.org" || alice.DisplayName != "alice" || !alice.Enabled {
			t.Errorf("alice = %+v", alice)
		}

		groups, err := r.Groups(ctx)
		if err != nil {
			t.Fatal(err)
		}
		members := map[string]string{}
		for _, g := range groups {
			m := append([]string(nil), g.Members...)
			sort.Strings(m)
			members[g.ID] = strings.Join(m, ",")
		}
		want := map[string]string{"admin": "admin", "staff": "alice,bob", "team": "bob,carol"}
		if fmt.Sprint(members) != fmt.Sprint(want) {
			t.Errorf("groups = %v, want %v", members, want)
		}
	})
}

func TestReaderCalendars(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Reader, fx fixtureInfo) {
		ctx := context.Background()
		cals, err := r.Calendars(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(cals) != fx.Rows["oc_calendars"] {
			t.Fatalf("%d calendars, want %d", len(cals), fx.Rows["oc_calendars"])
		}
		byKey := map[string]Calendar{}
		for _, c := range cals {
			byKey[c.Owner.Name+"/"+c.URI] = c
		}
		work := byKey["alice/work"]
		if work.DisplayName != "Work" || work.Color != "#1d2d44" || work.Owner.Kind != PrincipalUser ||
			fmt.Sprint(work.Components) != "[VEVENT]" {
			t.Errorf("alice/work = %+v", work)
		}
		if got := byKey["alice/archive"].DisplayName; got != "Archiv 2024 (ä)" {
			t.Errorf("archive name = %q", got)
		}
		if sys := byKey["system/contact_birthdays"]; sys.Owner.Kind != PrincipalSystem {
			t.Errorf("system calendar = %+v", sys)
		}

		// Every calendar holds the number of events the description gives,
		// each one byte for byte as stored.
		// The description lists the birthday calendars of all owners under one key.
		birthdays := 0
		for _, c := range cals {
			n := 0
			err := r.CalendarObjects(ctx, c.ID, func(o Object) error {
				n++
				if int64(len(o.Data)) != o.Size {
					return fmt.Errorf("%s: %d bytes, size column says %d", o.URI, len(o.Data), o.Size)
				}
				if !strings.HasPrefix(string(o.Data), "BEGIN:VCALENDAR") || o.ETag == "" || o.URI == "" {
					return fmt.Errorf("odd object %+v", o)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("%s/%s: %v", c.Owner.Name, c.URI, err)
			}
			if c.URI == "contact_birthdays" {
				birthdays += n
				continue
			}
			key := c.Owner.Name + "/" + c.URI
			if want, ok := fx.Calendars[key]; ok && n != want {
				t.Errorf("%s holds %d events, want %d", key, n, want)
			}
		}
		if want := fx.Calendars["system/contact_birthdays"]; birthdays != want {
			t.Errorf("birthday calendars hold %d events, want %d", birthdays, want)
		}
	})
}

func TestReaderContacts(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Reader, fx fixtureInfo) {
		ctx := context.Background()
		books, err := r.AddressBooks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != fx.Rows["oc_addressbooks"] {
			t.Fatalf("%d address books, want %d", len(books), fx.Rows["oc_addressbooks"])
		}
		want := map[string]int{"alice/contacts": 120, "alice/friends": 15, "bob/contacts": 10}
		total := 0
		for _, b := range books {
			n := 0
			err := r.Cards(ctx, b.ID, func(c Card) error {
				n++
				if !strings.HasPrefix(string(c.Data), "BEGIN:VCARD") || int64(len(c.Data)) != c.Size {
					return fmt.Errorf("odd card %s", c.URI)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			total += n
			if w, ok := want[b.Owner.Name+"/"+b.URI]; ok && w != n {
				t.Errorf("%s/%s holds %d cards, want %d", b.Owner.Name, b.URI, n, w)
			}
		}
		if total != fx.Rows["oc_cards"] {
			t.Errorf("%d cards, want %d", total, fx.Rows["oc_cards"])
		}
		if books[0].Owner.Kind == "" {
			t.Error("address book owner missing")
		}
	})
}

func TestReaderSharesAndSubscriptions(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Reader, fx fixtureInfo) {
		ctx := context.Background()
		cals, _ := r.Calendars(ctx)
		name := map[int64]string{}
		for _, c := range cals {
			name[c.ID] = c.Owner.Name + "/" + c.URI
		}
		books, _ := r.AddressBooks(ctx)
		for _, b := range books {
			name[-b.ID] = b.Owner.Name + "/" + b.URI
		}
		shares, err := r.Shares(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(shares) != fx.Rows["oc_dav_shares"] {
			t.Fatalf("%d shares, want %d", len(shares), fx.Rows["oc_dav_shares"])
		}
		got := map[string]bool{}
		for _, s := range shares {
			key := name[s.ResourceID]
			if s.Kind == ResourceAddressBook {
				key = name[-s.ResourceID]
			}
			desc := fmt.Sprintf("%s %s -> %s:%s %s", s.Kind, key, s.Target.Kind, s.Target.Name, s.Access)
			if s.PublicURI != "" {
				desc = fmt.Sprintf("%s %s public %s", s.Kind, key, s.Access)
				if len(s.PublicURI) < 8 {
					t.Errorf("public uri %q", s.PublicURI)
				}
			}
			got[desc] = true
		}
		for _, want := range []string{
			"calendar alice/work -> user:bob read-write",
			"calendar alice/work -> group:team read-only",
			"calendar alice/personal -> user:carol read-only",
			"calendar bob/projects -> user:alice read-write",
			"addressbook bob/contacts -> user:alice read-only",
			"addressbook alice/friends -> user:bob read-write",
			"addressbook alice/friends -> group:team read-only",
		} {
			if !got[want] {
				t.Errorf("share %q not read; got %v", want, got)
			}
		}
		public := 0
		for _, s := range shares {
			if s.PublicURI != "" {
				public++
			}
		}
		if public != 1 {
			t.Errorf("%d public links, want 1", public)
		}

		subs, err := r.Subscriptions(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(subs) != 2 || subs[0].Source != "https://calendar.invalid/holidays.ics" || subs[0].DisplayName != "Holidays" {
			t.Errorf("subscriptions = %+v", subs)
		}
	})
}

func TestReaderAppTokens(t *testing.T) {
	eachDatabase(t, func(t *testing.T, r *Reader, fx fixtureInfo) {
		toks, err := r.AppTokens(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(toks) != len(fx.AppTokens) {
			t.Fatalf("%d tokens, want %d", len(toks), len(fx.AppTokens))
		}
		for _, want := range fx.AppTokens {
			found := false
			for _, got := range toks {
				if got.User == want.User && got.Name == want.Name {
					found = true
					if got.Digest != want.Digest {
						t.Errorf("%s/%s digest %s, want %s", want.User, want.Name, got.Digest, want.Digest)
					}
					sum := sha512.Sum512([]byte(want.Token + fx.Secret))
					if hex.EncodeToString(sum[:]) != got.Digest {
						t.Errorf("%s/%s digest is not sha512(token . secret)", want.User, want.Name)
					}
				}
			}
			if !found {
				t.Errorf("token %s/%s not read", want.User, want.Name)
			}
		}
	})
}
