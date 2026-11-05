package graphapi

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/store"
	"github.com/LukasHirt/groupkit/store/filestore"
	"golang.org/x/crypto/bcrypt"

	"github.com/owncloud/ocis-groupware/internal/auth"
	"github.com/owncloud/ocis-groupware/internal/principals"
)

// importedFixture serves the facade for a user whose owner key differs from
// the subject, as for a principal that was imported before the user signed in.
// The token "mig" signs in as that user.
func importedFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	st, err := filestore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rec, err := json.Marshal(map[string]any{
		"owner": "migrated-1", "sub": "sub-mig", "username": "mig", "display_name": "Mig Rated",
		"created_at": time.Now(), "updated_at": time.Now(), "defaults_created": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	od := st.OwnerDir("migrated-1")
	if err := os.MkdirAll(od, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(od, "principal.json"), rec, 0o600); err != nil {
		t.Fatal(err)
	}
	reg, err := principals.NewRegistry(dir, st, st.OwnerDir, testLogger(), principals.WithBcryptCost(bcrypt.MinCost))
	if err != nil {
		t.Fatal(err)
	}
	v := &fakeVerifier{tokens: map[string]auth.Claims{
		"mig": {Subject: "sub-mig", PreferredUsername: "mig", DisplayName: "Mig Rated"},
	}}
	h, err := New(Config{Store: st, Registry: reg, Logger: testLogger()})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle(Prefix+"/", h)
	return &fixture{st: st, reg: reg, handler: auth.Middleware(v, testLogger())(mux)}
}

func TestContactFoldersUseTheStoreOwner(t *testing.T) {
	f := importedFixture(t)
	ctx := context.Background()
	ref := store.CollectionRef{Owner: "migrated-1", Kind: store.KindAddressBook, Name: "main"}
	if _, err := f.st.CreateCollection(ctx, ref, store.Props{DisplayName: "Main"}); err != nil {
		t.Fatal(err)
	}
	putCard(t, f, ref, "a.vcf", "BEGIN:VCARD\nVERSION:3.0\nFN:Ada\nN:;Ada;;;\nUID:a\nEND:VCARD\n")

	rec := f.do(http.MethodGet, "/graph/v1.0/me/contactFolders", "mig", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	list := decodeBody[contactFolderList](t, rec).Value
	if len(list) != 1 || list[0].DisplayName != "Main" || !list[0].IsDefault || !list[0].CanEdit || !list[0].CanShare {
		t.Fatalf("folders = %+v, want the owner's address book as an owned default", list)
	}

	rec = f.do(http.MethodGet, "/graph/v1.0/me/contactFolders/"+list[0].ID+"/contacts", "mig", "")
	if rec.Code != http.StatusOK || len(decodeBody[contactList](t, rec).Value) != 1 {
		t.Errorf("contacts: %d %s", rec.Code, rec.Body.String())
	}

	rec = f.do(http.MethodPost, "/graph/v1.0/me/contactFolders", "mig", `{"displayName":"Second"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	cols, err := f.st.ListCollections(ctx, "migrated-1", store.KindAddressBook)
	if err != nil || len(cols) != 2 {
		t.Fatalf("collections of the owner = %d, %v; want 2", len(cols), err)
	}
	if other, _ := f.st.ListCollections(ctx, "sub-mig", store.KindAddressBook); len(other) != 0 {
		t.Errorf("a collection was created under the sub: %+v", other)
	}
}
