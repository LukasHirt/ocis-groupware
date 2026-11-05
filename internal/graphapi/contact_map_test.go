package graphapi

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/LukasHirt/groupkit/vcard"
)

func loadCard(t *testing.T, name string) *vcard.Card {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "contacts", name))
	if err != nil {
		t.Fatal(err)
	}
	c, err := vcard.Parse(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return c
}

func indentJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestContactGolden(t *testing.T) {
	for _, name := range []string{"apple-v3", "thunderbird-v3", "v4"} {
		t.Run(name, func(t *testing.T) {
			got := indentJSON(t, contactFromCard(loadCard(t, name+".vcf")))
			path := filepath.Join("testdata", "contacts", name+".json")
			if *update {
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(want, got) {
				t.Errorf("differs from %s; run with -update if intended.\ngot:\n%s\nwant:\n%s", path, got, want)
			}
		})
	}
}

func TestContactMappingSpotChecks(t *testing.T) {
	apple := contactFromCard(loadCard(t, "apple-v3.vcf"))
	if apple.MobilePhone != "+33 1 55 55 01 01" {
		t.Errorf("mobile = %q", apple.MobilePhone)
	}
	if !reflect.DeepEqual(apple.HomePhones, []string{"+33 1 55 55 02 02", "+33 1 55 55 03 03"}) {
		t.Errorf("home phones = %v (a custom label counts as home)", apple.HomePhones)
	}
	if len(apple.BusinessPhones) != 0 {
		t.Errorf("a fax number is not a business phone: %v", apple.BusinessPhones)
	}
	if apple.Birthday == nil || *apple.Birthday != "0001-11-07T00:00:00Z" {
		t.Errorf("an omitted year shows as year 1: %v", apple.Birthday)
	}
	if !reflect.DeepEqual(apple.IMAddresses, []string{"https://twitter.com/mariecurie", "skype:marie.curie"}) {
		t.Errorf("imAddresses = %v", apple.IMAddresses)
	}
	if apple.SpouseName != "Pierre Curie" || apple.AssistantName != "Bronia Dluska" || !reflect.DeepEqual(apple.Children, []string{"Irene"}) {
		t.Errorf("related names: %q %q %v", apple.SpouseName, apple.AssistantName, apple.Children)
	}
	if !reflect.DeepEqual(apple.Categories, []string{"Physicists", "Nobel, laureates"}) {
		t.Errorf("categories = %q", apple.Categories)
	}
	if apple.PersonalNotes != "Two Nobel prizes\nRadium, polonium" {
		t.Errorf("notes = %q", apple.PersonalNotes)
	}
	if apple.BusinessAddress.Street != "12 Rue Pierre et Marie Curie" || apple.HomeAddress.City != "Sceaux" {
		t.Errorf("addresses: %+v %+v", apple.BusinessAddress, apple.HomeAddress)
	}

	tb := contactFromCard(loadCard(t, "thunderbird-v3.vcf"))
	if tb.MobilePhone != "+41 79 000 00 03" || !reflect.DeepEqual(tb.BusinessPhones, []string{"+41 31 000 00 01"}) {
		t.Errorf("phones: %q %v", tb.MobilePhone, tb.BusinessPhones)
	}
	if tb.OtherAddress.City != "Princeton" || tb.BusinessAddress.City != "Bern" {
		t.Errorf("an address without a type is the other one: %+v", tb)
	}
	if tb.Birthday == nil || *tb.Birthday != "1879-03-14T00:00:00Z" {
		t.Errorf("birthday = %v", tb.Birthday)
	}
	if tb.JobTitle != "Technical Expert, Class III" || tb.CompanyName != "Patent Office" || tb.Department != "Examination" {
		t.Errorf("work: %q %q %q", tb.JobTitle, tb.CompanyName, tb.Department)
	}

	v4 := contactFromCard(loadCard(t, "v4.vcf"))
	if v4.MobilePhone != "+44-20-7946-0000" || !reflect.DeepEqual(v4.BusinessPhones, []string{"+44-20-7946-0001"}) {
		t.Errorf("tel URIs: %q %v", v4.MobilePhone, v4.BusinessPhones)
	}
	if v4.Birthday == nil || *v4.Birthday != "0001-12-10T00:00:00Z" {
		t.Errorf("--MMDD birthday = %v", v4.Birthday)
	}
	if v4.Manager != "Charles Babbage" || !reflect.DeepEqual(v4.Children, []string{"Byron"}) {
		t.Errorf("v4 related: %q %v", v4.Manager, v4.Children)
	}
	if v4.SpouseName != "" {
		t.Errorf("a urn:uuid RELATED is a link, not a name: %q", v4.SpouseName)
	}
}

func TestGroupCards(t *testing.T) {
	for _, tc := range []struct {
		file    string
		members []string
	}{
		{"group-v4.vcf", []string{"apple-1", "tb-1"}},
		{"group-apple.vcf", []string{"apple-1"}},
	} {
		isGroup, members := groupCardInfo(loadCard(t, tc.file))
		if !isGroup || !reflect.DeepEqual(members, tc.members) {
			t.Errorf("%s: %v %v", tc.file, isGroup, members)
		}
	}
	for _, f := range []string{"apple-v3.vcf", "thunderbird-v3.vcf", "v4.vcf"} {
		if isGroup, _ := groupCardInfo(loadCard(t, f)); isGroup {
			t.Errorf("%s is not a group card", f)
		}
	}
}

func cardLines(t *testing.T, c *vcard.Card) []string {
	t.Helper()
	out := strings.Split(strings.TrimSuffix(string(encodeCard(c)), "\r\n"), "\r\n")
	// Unfold for readability of assertions.
	var lines []string
	for _, l := range out {
		if strings.HasPrefix(l, " ") && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

func mustPatch(t *testing.T, body string) contactPatch {
	t.Helper()
	var p contactPatch
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

var fixedNow = time.Date(2027, 2, 3, 4, 5, 6, 0, time.UTC)

func TestEncodeCardRoundTrip(t *testing.T) {
	for _, f := range []string{"apple-v3.vcf", "thunderbird-v3.vcf", "v4.vcf"} {
		c := loadCard(t, f)
		again, err := vcard.Parse(encodeCard(c))
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if !reflect.DeepEqual(c, again) {
			t.Errorf("%s: parse(encode(card)) differs", f)
		}
		if !bytes.Equal(encodeCard(c), encodeCard(again)) {
			t.Errorf("%s: encoding is not stable", f)
		}
	}
}

func TestEncodeCardFoldsLongLines(t *testing.T) {
	c := &vcard.Card{Version: "3.0", Props: []vcard.Property{{Name: "NOTE", Raw: strings.Repeat("é", 100)}}}
	data := encodeCard(c)
	for _, l := range strings.Split(strings.TrimSuffix(string(data), "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Errorf("line of %d octets: %q", len(l), l)
		}
	}
	got, err := vcard.Parse(data)
	if err != nil || got.Props[0].Raw != c.Props[0].Raw {
		t.Errorf("round trip: %v", err)
	}
}

func TestPatchKeepsUntouchedProperties(t *testing.T) {
	c := loadCard(t, "apple-v3.vcf")
	before := append([]vcard.Property(nil), c.Props...)
	changed, aerr := applyContact(c, mustPatch(t, `{"jobTitle":"Chair","mobilePhone":"+33 1 55 55 01 01"}`), fixedNow)
	if aerr != nil || !changed {
		t.Fatalf("%v %v", changed, aerr)
	}
	for _, p := range before {
		if p.Name == "TITLE" || p.Name == "REV" {
			continue
		}
		found := false
		for _, q := range c.Props {
			if reflect.DeepEqual(p, q) {
				found = true
			}
		}
		if !found {
			t.Errorf("property lost or changed: %+v", p)
		}
	}
	if got := contactFromCard(c); got.JobTitle != "Chair" {
		t.Errorf("jobTitle = %q", got.JobTitle)
	}
	lines := cardLines(t, c)
	if !contains(lines, "REV:20270203T040506Z") {
		t.Errorf("REV not refreshed: %v", lines)
	}
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func TestPatchWithNoChangeTouchesNothing(t *testing.T) {
	c := loadCard(t, "apple-v3.vcf")
	before := encodeCard(c)
	read := contactFromCard(c)
	body, _ := json.Marshal(read)
	changed, aerr := applyContact(c, mustPatch(t, string(body)), fixedNow)
	if aerr != nil {
		t.Fatal(aerr)
	}
	if changed || !bytes.Equal(before, encodeCard(c)) {
		t.Errorf("sending back what was read changed the card (changed=%v):\n%s", changed, encodeCard(c))
	}
}

func TestPatchLists(t *testing.T) {
	c := loadCard(t, "thunderbird-v3.vcf")
	_, aerr := applyContact(c, mustPatch(t, `{
		"emailAddresses":[{"address":"A.Einstein@patent.example"},{"address":"new@example.org"}],
		"businessPhones":["+41 31 000 00 09"],
		"categories":["Friends","Physicists"],
		"imAddresses":["xmpp:albert@chat.example"]}`), fixedNow)
	if aerr != nil {
		t.Fatal(aerr)
	}
	lines := cardLines(t, c)
	for _, want := range []string{
		"EMAIL:a.einstein@patent.example", // kept as written, the case of the address does not matter
		"EMAIL:new@example.org",
		"TEL;TYPE=WORK:+41 31 000 00 09",
		"TEL;TYPE=HOME:+41 31 000 00 02",
		"TEL;TYPE=CELL:+41 79 000 00 03",
		"TEL;TYPE=CELL:+41 79 000 00 04",
		"TEL;TYPE=PAGER:+41 31 000 00 05",
		"CATEGORIES:Friends,Physicists",
		"IMPP:xmpp:albert@chat.example",
		"X-CUSTOM-THING:kept",
	} {
		if !contains(lines, want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(lines, "\n"))
		}
	}
	for _, gone := range []string{"EMAIL;TYPE=OTHER:albert@example.org", "TEL;TYPE=WORK,VOICE:+41 31 000 00 01"} {
		if contains(lines, gone) {
			t.Errorf("%q should be gone", gone)
		}
	}
}

func TestPatchRemovesOrphanedLabels(t *testing.T) {
	c := loadCard(t, "apple-v3.vcf")
	_, aerr := applyContact(c, mustPatch(t, `{"businessAddress":null,"spouseName":"","homePhones":["+33 1 55 55 02 02"]}`), fixedNow)
	if aerr != nil {
		t.Fatal(aerr)
	}
	for _, l := range cardLines(t, c) {
		if strings.HasPrefix(l, "ITEM4.") || strings.HasPrefix(l, "ITEM6.") || strings.HasPrefix(l, "ITEM3.") {
			t.Errorf("a group is left behind: %q", l)
		}
	}
}

func TestPatchNamesAndDisplayName(t *testing.T) {
	c := loadCard(t, "thunderbird-v3.vcf")
	if _, aerr := applyContact(c, mustPatch(t, `{"surname":"Mileva-Einstein","title":"Prof."}`), fixedNow); aerr != nil {
		t.Fatal(aerr)
	}
	lines := cardLines(t, c)
	if !contains(lines, "N:Mileva-Einstein;Albert;;Prof.;") || !contains(lines, "FN:Albert Mileva-Einstein") {
		t.Errorf("names: %v", lines)
	}
	// An FN the user chose is not regenerated.
	c = loadCard(t, "thunderbird-v3.vcf")
	if _, aerr := applyContact(c, mustPatch(t, `{"displayName":"Prof. A.","givenName":"Al"}`), fixedNow); aerr != nil {
		t.Fatal(aerr)
	}
	if lines := cardLines(t, c); !contains(lines, "FN:Prof. A.") || !contains(lines, "N:Einstein;Al;;;") {
		t.Errorf("names: %v", lines)
	}
}

func TestPatchBirthday(t *testing.T) {
	for _, tc := range []struct {
		card, body, want string
	}{
		{"thunderbird-v3.vcf", `{"birthday":"1880-01-02T00:00:00Z"}`, "BDAY:1880-01-02"},
		{"thunderbird-v3.vcf", `{"birthday":"0001-07-04T00:00:00Z"}`, "BDAY;X-APPLE-OMIT-YEAR=1604:1604-07-04"},
		{"v4.vcf", `{"birthday":"0001-07-04T00:00:00Z"}`, "BDAY:--0704"},
		{"v4.vcf", `{"birthday":"1815-12-10"}`, "BDAY:1815-12-10"},
	} {
		c := loadCard(t, tc.card)
		if _, aerr := applyContact(c, mustPatch(t, tc.body), fixedNow); aerr != nil {
			t.Fatalf("%s: %v", tc.body, aerr)
		}
		if !contains(cardLines(t, c), tc.want) {
			t.Errorf("%s on %s: want %q in %v", tc.body, tc.card, tc.want, cardLines(t, c))
		}
	}
	c := loadCard(t, "thunderbird-v3.vcf")
	if _, aerr := applyContact(c, mustPatch(t, `{"birthday":null}`), fixedNow); aerr != nil {
		t.Fatal(aerr)
	}
	if contactFromCard(c).Birthday != nil {
		t.Error("null did not clear the birthday")
	}
	for _, bad := range []string{`{"birthday":"yesterday"}`, `{"birthday":"2026-13-40"}`} {
		if _, aerr := applyContact(loadCard(t, "v4.vcf"), mustPatch(t, bad), fixedNow); aerr == nil || aerr.status != 400 {
			t.Errorf("%s: %v", bad, aerr)
		}
	}
}

func TestPatchRejectsBadValues(t *testing.T) {
	for _, body := range []string{
		`{"displayName":"a\u0000b"}`,
		`{"emailAddresses":[{"address":""}]}`,
		`{"emailAddresses":[{"address":"a\nb@example.org"}]}`,
		`{"categories":["` + strings.Repeat("x", 5000) + `"]}`,
		`{"businessPhones":[` + strings.Repeat(`"1",`, 200) + `"1"]}`,
	} {
		if _, aerr := applyContact(loadCard(t, "v4.vcf"), mustPatch(t, body), fixedNow); aerr == nil || aerr.status != 400 {
			t.Errorf("%.60s: %v", body, aerr)
		}
	}
}

func TestNewCard(t *testing.T) {
	p := mustPatch(t, `{"givenName":"Grace","surname":"Hopper","emailAddresses":[{"address":"grace@example.org"}],
		"mobilePhone":"+1 555 0100","companyName":"Navy","personalNotes":"COBOL; \"debugging\"\nbug"}`)
	c, aerr := newCard(p, "uid-1", fixedNow)
	if aerr != nil {
		t.Fatal(aerr)
	}
	if c.Version != "3.0" || c.UID() != "uid-1" {
		t.Errorf("version %q uid %q", c.Version, c.UID())
	}
	got := contactFromCard(c)
	if got.DisplayName != "Grace Hopper" || got.MobilePhone != "+1 555 0100" || got.CompanyName != "Navy" ||
		got.PersonalNotes != "COBOL; \"debugging\"\nbug" || len(got.EmailAddresses) != 1 {
		t.Errorf("new card reads back as %+v", got)
	}
	if _, err := vcard.Validate(encodeCard(c)); err != nil {
		t.Errorf("not a valid card: %v", err)
	}
	if !contains(cardLines(t, c), "REV:20270203T040506Z") {
		t.Errorf("no REV: %v", cardLines(t, c))
	}
	// Without a name the display name falls back to the e-mail address, then the company.
	c, _ = newCard(mustPatch(t, `{"emailAddresses":[{"address":"x@example.org"}]}`), "u2", fixedNow)
	if contactFromCard(c).DisplayName != "x@example.org" {
		t.Errorf("fallback name = %q", contactFromCard(c).DisplayName)
	}
}

func TestMemberships(t *testing.T) {
	groups := []groupCard{
		{UID: "g1", Name: "Physicists", Members: []string{"apple-1", "tb-1"}},
		{UID: "g2", Name: "Family", Members: []string{"4B8A0C1E-5C2C-4F7E-9A3D-2F1C1C1C0001"}},
		{UID: "g3", Name: "Empty"},
	}
	got := memberships("apple-1", groups)
	if len(got) != 1 || got[0].ID != "g1" || got[0].DisplayName != "Physicists" {
		t.Errorf("apple-1: %+v", got)
	}
	if got := memberships("urn:uuid:tb-1", groups); len(got) != 1 {
		t.Errorf("a urn:uuid UID matches its bare form: %+v", got)
	}
	if got := memberships("", groups); len(got) != 0 {
		t.Errorf("no uid: %+v", got)
	}
	if got := memberships("nobody", groups); got == nil || len(got) != 0 {
		t.Errorf("no groups must be an empty list: %#v", got)
	}
}
