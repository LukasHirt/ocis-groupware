package main

import (
	"context"
	"log/slog"
	"testing"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"

	"github.com/owncloud/ocis-groupware/internal/principals"
	"github.com/owncloud/ocis-groupware/internal/sharing"
)

// The service's wiring gives a recipient the owner's collection under the
// recipient's own home, named after the owner, and picks the shares up again
// after a restart.
func TestOpenSharingConnectsPrincipalsAndStore(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)

	open := func() (*filestore.Store, *principals.Registry, *sharing.Shares, store.Store) {
		st, err := filestore.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = st.Close() })
		reg, err := principals.NewRegistry(dir, st, st.OwnerDir, logger)
		if err != nil {
			t.Fatal(err)
		}
		shares, view, err := openSharing(st, reg, logger)
		if err != nil {
			t.Fatal(err)
		}
		return st, reg, shares, view
	}

	st, reg, shares, view := open()
	for sub, name := range map[string]string{"sub-alice": "Alice", "sub-bob": "Bob"} {
		if _, _, err := reg.Provision(ctx, sub, name, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.SetIdentity(ctx, "sub-bob", principals.Identity{OCISID: "u-bob"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.SetIdentity(ctx, "sub-alice", principals.Identity{OCISID: "u-alice"}); err != nil {
		t.Fatal(err)
	}
	cols, err := st.ListCollections(ctx, "sub-alice", store.KindAddressBook)
	if err != nil || len(cols) == 0 {
		t.Fatalf("alice's address books = %v, %v", cols, err)
	}
	book := cols[0]
	if err := shares.Grant(ctx, book.Ref, book.ID, sharing.Member{Type: sharing.User, ID: "u-bob", Access: sharing.Read}); err != nil {
		t.Fatal(err)
	}
	// Sharing with oneself is refused through the owner's recorded id.
	if err := shares.Grant(ctx, book.Ref, book.ID, sharing.Member{Type: sharing.User, ID: "u-alice", Access: sharing.Read}); err == nil {
		t.Error("alice shared with herself")
	}

	check := func(view store.Store) {
		t.Helper()
		got, err := view.ListCollections(ctx, "sub-bob", store.KindAddressBook)
		if err != nil || len(got) != 2 {
			t.Fatalf("bob's address books = %+v, %v", got, err)
		}
		shared := got[1]
		if shared.Ref.Name != "shared-"+book.ID || shared.Access != store.AccessRead || shared.Props.DisplayName != book.Props.DisplayName+" (Alice)" {
			t.Errorf("shared address book = %+v", shared)
		}
	}
	check(view)

	// A restart reads the share back.
	_ = st.Close()
	_, _, _, view = open()
	check(view)
}

// Purging an owner removes the shares on its collections and reports them,
// and a share whose collection vanished while the service was down is gone
// after the next start.
func TestSharesEndWithTheirOwnerAndTheirCollections(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	logger := slog.New(slog.DiscardHandler)
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, logger)
	if err != nil {
		t.Fatal(err)
	}
	shares, _, err := openSharing(st, reg, logger)
	if err != nil {
		t.Fatal(err)
	}
	for sub, name := range map[string]string{"sub-alice": "Alice", "sub-carol": "Carol", "sub-bob": "Bob"} {
		if _, _, err := reg.Provision(ctx, sub, name, name); err != nil {
			t.Fatal(err)
		}
	}
	grant := func(owner string, kind store.Kind, name, to string) store.Collection {
		t.Helper()
		c, err := st.GetCollection(ctx, store.CollectionRef{Owner: owner, Kind: kind, Name: name})
		if err != nil {
			t.Fatal(err)
		}
		if err := shares.Grant(ctx, c.Ref, c.ID, sharing.Member{Type: sharing.User, ID: to, Access: sharing.Read}); err != nil {
			t.Fatal(err)
		}
		return c
	}
	aliceCal := grant("sub-alice", store.KindCalendar, "personal", "u-bob")
	grant("sub-alice", store.KindAddressBook, "contacts", "u-bob")
	grant("sub-alice", store.KindAddressBook, "contacts", "u-eve")
	grant("sub-carol", store.KindCalendar, "personal", "u-bob")

	// Alice's calendar vanishes behind the service's back, then it restarts.
	if err := st.DeleteCollection(ctx, aliceCal.Ref); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	reg, err = principals.NewRegistry(dir, st, st.OwnerDir, logger)
	if err != nil {
		t.Fatal(err)
	}
	shares, _, err = openSharing(st, reg, logger)
	if err != nil {
		t.Fatal(err)
	}
	if got := shares.Count("sub-alice"); got != 2 {
		t.Fatalf("alice's grants after the restart = %d, want the 2 on her address book", got)
	}

	// Purge: carol's shares stay, alice's go.
	if _, err := reg.SetDisabled(ctx, "sub-alice", true); err != nil {
		t.Fatal(err)
	}
	report, err := reg.Purge(ctx, "sub-alice", false)
	if err != nil || report.Shares != 2 {
		t.Fatalf("purge = %+v, %v; want 2 shares", report, err)
	}
	if shares.Count("sub-alice") != 0 || shares.Count("sub-carol") != 1 {
		t.Fatalf("grants after the purge: alice %d, carol %d", shares.Count("sub-alice"), shares.Count("sub-carol"))
	}
}
