package graphapi

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LukasHirt/groupkit/vcard"
)

// Contacts are stored as vCard and shown as Graph contacts. Reading never
// changes a card. Writing edits the parsed card: a field the request does not
// send, and every property the mapping has no field for, stays as it was.

const (
	// newCardVersion is the vCard version of cards made through the facade.
	// 3.0 is what every address book client reads.
	newCardVersion = "3.0"
	// omitYearYear is the year Apple clients put in a birthday without a
	// year, flagged with X-APPLE-OMIT-YEAR.
	omitYearYear = 1604
	// noYear is the year a Graph birthday has when the year is unknown.
	noYear = 1

	maxContactText  = 1024
	maxContactNotes = 32 << 10
	maxListItems    = 100
	maxEmailLength  = 320
)

// Property names the mapping reads beyond the ones vCard defines.
const (
	propLabel   = "X-ABLABEL"
	propRelated = "X-ABRELATEDNAMES"
)

// contactFromCard maps a card to a Graph contact, without the ids and times
// that only the store knows.
func contactFromCard(c *vcard.Card) contactJSON {
	n := splitParts(first(c, "N"), 5)
	org := splitParts(first(c, "ORG"), 2)
	out := contactJSON{
		Categories:       cardCategories(c),
		Birthday:         birthdayOf(c),
		DisplayName:      text(c, "FN"),
		Surname:          n[0],
		GivenName:        n[1],
		MiddleName:       n[2],
		Title:            n[3],
		Generation:       n[4],
		NickName:         text(c, "NICKNAME"),
		YomiGivenName:    text(c, "X-PHONETIC-FIRST-NAME"),
		YomiSurname:      text(c, "X-PHONETIC-LAST-NAME"),
		YomiCompany:      text(c, "X-PHONETIC-ORG"),
		JobTitle:         text(c, "TITLE"),
		CompanyName:      org[0],
		Department:       org[1],
		Profession:       text(c, "ROLE"),
		AssistantName:    firstOr(relatedNames(c, "assistant")),
		Manager:          firstOr(relatedNames(c, "manager")),
		SpouseName:       firstOr(relatedNames(c, "spouse")),
		Children:         relatedNames(c, "child"),
		BusinessPhones:   phones(c, "work"),
		HomePhones:       phones(c, "home"),
		MobilePhone:      firstOr(phones(c, "cell")),
		BusinessHomePage: firstOr(values(c, "URL", nil)),
		PersonalNotes:    text(c, "NOTE"),
		EmailAddresses:   []emailAddress{},
		IMAddresses:      imAddresses(c),
		HomeAddress:      addressOf(c, "home"),
		BusinessAddress:  addressOf(c, "work"),
		OtherAddress:     addressOf(c, "other"),
		Groups:           []contactGroup{},
	}
	for _, a := range emailsOf(c) {
		out.EmailAddresses = append(out.EmailAddresses, emailAddress{Name: a, Address: a})
	}
	return out
}

func firstOr(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}

func first(c *vcard.Card, name string) string {
	if p := c.Find(name); len(p) > 0 {
		return p[0].Raw
	}
	return ""
}

func text(c *vcard.Card, name string) string {
	if p := c.Find(name); len(p) > 0 {
		return p[0].Text()
	}
	return ""
}

// values returns the unescaped values of the properties called name that sel
// accepts, in card order. A nil sel accepts all.
func values(c *vcard.Card, name string, sel func(vcard.Property) bool) []string {
	out := []string{}
	for _, p := range c.Find(name) {
		if sel == nil || sel(p) {
			out = append(out, strings.TrimSpace(p.Text()))
		}
	}
	return slices.DeleteFunc(out, func(s string) bool { return s == "" })
}

// splitUnescaped cuts raw at the separator where no backslash escapes it. The
// pieces keep their escapes.
func splitUnescaped(raw string, sep byte) []string {
	var out []string
	start := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			i++
		case sep:
			out = append(out, raw[start:i])
			start = i + 1
		}
	}
	return append(out, raw[start:])
}

func unescape(raw string) string { return vcard.Property{Raw: raw}.Text() }

// escapeCardText writes s as a vCard text value.
func escapeCardText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	r := strings.NewReplacer(`\`, `\\`, `;`, `\;`, `,`, `\,`, "\n", `\n`)
	return r.Replace(s)
}

// splitParts returns the first n components of a structured value, unescaped.
func splitParts(raw string, n int) []string {
	out := make([]string, n)
	for i, p := range splitUnescaped(raw, ';') {
		if i >= n {
			break
		}
		out[i] = unescape(p)
	}
	return out
}

func cardCategories(c *vcard.Card) []string {
	out := []string{}
	for _, p := range c.Find("CATEGORIES") {
		for _, part := range splitUnescaped(p.Raw, ',') {
			s := strings.TrimSpace(unescape(part))
			if s != "" && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// typesOf returns the upper-case TYPE values of a property. A quoted value
// such as "work,voice" holds several.
func typesOf(p vcard.Property) []string {
	var out []string
	for _, v := range p.Params["TYPE"] {
		for _, t := range strings.Split(v, ",") {
			if t = strings.ToUpper(strings.TrimSpace(t)); t != "" {
				out = append(out, t)
			}
		}
	}
	return out
}

// labelOf returns the lower-case label Apple clients give a grouped property
// in X-ABLabel, without the _$!< >!$_ wrapper of the built-in ones.
func labelOf(c *vcard.Card, p vcard.Property) string {
	if p.Group == "" {
		return ""
	}
	for _, l := range c.Props {
		if l.Group == p.Group && l.Name == propLabel {
			s := strings.TrimSpace(l.Text())
			s = strings.TrimPrefix(s, "_$!<")
			s = strings.TrimSuffix(s, ">!$_")
			return strings.ToLower(s)
		}
	}
	return ""
}

// place says where a phone number or address belongs: "cell", "work",
// "home", or "other" when nothing says. The TYPE parameter wins over the
// label. It is "" for a number that is not a voice number, such as a fax.
func place(c *vcard.Card, p vcard.Property) string {
	types := typesOf(p)
	label := labelOf(c, p)
	if p.Name == "TEL" {
		for _, t := range []string{"FAX", "PAGER", "VIDEO", "TEXTPHONE"} {
			if slices.Contains(types, t) {
				return ""
			}
		}
		if strings.Contains(label, "fax") || strings.Contains(label, "pager") {
			return ""
		}
		if slices.Contains(types, "CELL") {
			return "cell"
		}
	}
	switch {
	case slices.Contains(types, "WORK"):
		return "work"
	case slices.Contains(types, "HOME"):
		return "home"
	case p.Name == "TEL" && (strings.Contains(label, "mobile") || strings.Contains(label, "iphone") || label == "cell"):
		return "cell"
	case label == "work":
		return "work"
	case label == "home":
		return "home"
	}
	return "other"
}

func telValue(p vcard.Property) string {
	s := strings.TrimSpace(p.Text())
	if len(s) > 4 && strings.EqualFold(s[:4], "tel:") {
		s = s[4:]
	}
	return s
}

// phones lists the numbers of a Graph field: "cell" is the mobile phone,
// "work" the business phones, and "home" the home phones, which also take
// every number without a place.
func phones(c *vcard.Card, field string) []string {
	out := []string{}
	for _, p := range c.Find("TEL") {
		if telField(c, p) != field {
			continue
		}
		if v := telValue(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// telField is the Graph field a TEL property belongs to, or "".
func telField(c *vcard.Card, p vcard.Property) string {
	switch place(c, p) {
	case "cell":
		return "cell"
	case "work":
		return "work"
	case "home", "other":
		return "home"
	}
	return ""
}

func emailsOf(c *vcard.Card) []string {
	var out []string
	for _, a := range values(c, "EMAIL", nil) {
		if !slices.ContainsFunc(out, func(o string) bool { return strings.EqualFold(o, a) }) {
			out = append(out, a)
		}
	}
	return out
}

func imAddresses(c *vcard.Card) []string {
	out := []string{}
	for _, p := range c.Props {
		if p.Name == "IMPP" || p.Name == "X-SOCIALPROFILE" {
			if v := strings.TrimSpace(p.Raw); v != "" && !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

func addressOf(c *vcard.Card, where string) physicalAddress {
	for _, p := range c.Find("ADR") {
		if place(c, p) == where {
			return addressParts(p)
		}
	}
	return physicalAddress{}
}

func addressParts(p vcard.Property) physicalAddress {
	a := splitParts(p.Raw, 7)
	return physicalAddress{Street: a[2], City: a[3], State: a[4], PostalCode: a[5], CountryOrRegion: a[6]}
}

// relatedNames lists the names of related people of one kind: "spouse",
// "child", "assistant" or "manager". Apple clients write them as
// X-ABRELATEDNAMES with a label, vCard 4 as RELATED with a TYPE, and some
// clients as X-SPOUSE, X-MANAGER and X-ASSISTANT.
func relatedNames(c *vcard.Card, kind string) []string {
	out := []string{}
	for _, p := range c.Props {
		if relatedKind(c, p) != kind {
			continue
		}
		if v := strings.TrimSpace(p.Text()); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func relatedKind(c *vcard.Card, p vcard.Property) string {
	switch p.Name {
	case "X-SPOUSE", "X-EVOLUTION-SPOUSE":
		return "spouse"
	case "X-MANAGER", "X-EVOLUTION-MANAGER":
		return "manager"
	case "X-ASSISTANT", "X-EVOLUTION-ASSISTANT":
		return "assistant"
	case propRelated:
		switch labelOf(c, p) {
		case "spouse", "partner":
			return "spouse"
		case "child", "son", "daughter":
			return "child"
		case "assistant":
			return "assistant"
		case "manager":
			return "manager"
		}
	case "RELATED":
		if isLink(p.Raw) {
			return ""
		}
		types := typesOf(p)
		switch {
		case slices.Contains(types, "SPOUSE"):
			return "spouse"
		case slices.Contains(types, "CHILD"):
			return "child"
		}
	}
	return ""
}

// isLink reports whether a RELATED value points at another card instead of
// giving a name.
func isLink(raw string) bool {
	l := strings.ToLower(strings.TrimSpace(raw))
	for _, s := range []string{"urn:", "http:", "https:", "mailto:"} {
		if strings.HasPrefix(l, s) {
			return true
		}
	}
	return false
}

// birthdayOf returns the Graph birthday of the card, or nil when the card has
// none or its BDAY is not a date. A birthday without a year has the year 1.
func birthdayOf(c *vcard.Card) *string {
	for _, p := range c.Find("BDAY") {
		y, m, d, ok := parseBirthday(p)
		if !ok {
			continue
		}
		if y == 0 {
			y = noYear
		}
		s := fmt.Sprintf("%04d-%02d-%02dT00:00:00Z", y, m, d)
		return &s
	}
	return nil
}

// parseBirthday reads the date of a BDAY. The year is 0 when the card leaves
// it out. A time of day is ignored.
func parseBirthday(p vcard.Property) (year, month, day int, ok bool) {
	s := strings.TrimSpace(p.Raw)
	if i := strings.IndexByte(s, 'T'); i >= 0 {
		s = s[:i]
	}
	var ys, ms, ds string
	switch {
	case strings.HasPrefix(s, "--"):
		s = strings.ReplaceAll(s[2:], "-", "")
		if len(s) != 4 {
			return 0, 0, 0, false
		}
		ms, ds = s[:2], s[2:]
	default:
		s = strings.ReplaceAll(s, "-", "")
		if len(s) != 8 {
			return 0, 0, 0, false
		}
		ys, ms, ds = s[:4], s[4:6], s[6:]
	}
	m, err1 := strconv.Atoi(ms)
	d, err2 := strconv.Atoi(ds)
	if err1 != nil || err2 != nil || m < 1 || m > 12 || d < 1 || d > 31 {
		return 0, 0, 0, false
	}
	if ys != "" {
		y, err := strconv.Atoi(ys)
		if err != nil || y < 1 {
			return 0, 0, 0, false
		}
		year = y
		if slices.Contains(p.Params["X-APPLE-OMIT-YEAR"], ys) {
			year = 0
		}
	}
	if t := time.Date(max(year, noYear), time.Month(m), d, 0, 0, 0, 0, time.UTC); t.Day() != d {
		return 0, 0, 0, false
	}
	return year, m, d, true
}

// Group cards

// groupCard is a card that stands for a group of contacts.
type groupCard struct {
	UID     string
	Name    string
	Members []string
}

// groupCardInfo reports whether the card is a group card, and lists the UIDs
// of its members. vCard 4 marks a group with KIND:group and MEMBER; Apple
// clients use X-ADDRESSBOOKSERVER-KIND and X-ADDRESSBOOKSERVER-MEMBER.
func groupCardInfo(c *vcard.Card) (bool, []string) {
	isGroup := false
	var members []string
	for _, p := range c.Props {
		switch p.Name {
		case "KIND", "X-ADDRESSBOOKSERVER-KIND":
			if p.Group == "" && strings.EqualFold(strings.TrimSpace(p.Raw), "group") {
				isGroup = true
			}
		case "MEMBER", "X-ADDRESSBOOKSERVER-MEMBER":
			if uid := trimURN(p.Raw); uid != "" {
				members = append(members, uid)
			}
		}
	}
	if !isGroup {
		return false, nil
	}
	return true, members
}

func trimURN(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 9 && strings.EqualFold(s[:9], "urn:uuid:") {
		return s[9:]
	}
	return ""
}

// normalizeUID makes the UID of a card comparable with a group's member.
func normalizeUID(uid string) string {
	if u := trimURN(uid); u != "" {
		return u
	}
	return strings.TrimSpace(uid)
}

// memberships lists the groups that have the contact with the given UID as a
// member, in the order of groups.
func memberships(uid string, groups []groupCard) []contactGroup {
	out := []contactGroup{}
	uid = normalizeUID(uid)
	if uid == "" {
		return out
	}
	for _, g := range groups {
		if slices.ContainsFunc(g.Members, func(m string) bool { return normalizeUID(m) == uid }) {
			out = append(out, contactGroup{ID: g.UID, DisplayName: g.Name})
		}
	}
	return out
}

// Writing

type cardEditor struct {
	c       *vcard.Card
	changed bool
}

func (e *cardEditor) remove(match func(vcard.Property) bool) {
	n := len(e.c.Props)
	e.c.Props = slices.DeleteFunc(e.c.Props, match)
	if len(e.c.Props) != n {
		e.changed = true
	}
}

func (e *cardEditor) add(p ...vcard.Property) {
	e.c.Props = append(e.c.Props, p...)
	e.changed = true
}

func named(name string) func(vcard.Property) bool {
	return func(p vcard.Property) bool { return p.Name == name }
}

// setText sets a property that holds one text value. An empty value removes
// the property, unless keep is set, which leaves it empty.
func (e *cardEditor) setText(name, val string, keep bool) {
	for i, p := range e.c.Props {
		if p.Name != name {
			continue
		}
		switch {
		case p.Text() == val:
		case val == "" && !keep:
			e.remove(named(name))
		default:
			e.c.Props[i].Raw = escapeCardText(val)
			e.changed = true
		}
		return
	}
	if val != "" || keep {
		e.add(vcard.Property{Name: name, Raw: escapeCardText(val)})
	}
}

// setPart sets one component of a structured property such as N or ORG. The
// property is removed when all its components end up empty.
func (e *cardEditor) setPart(name string, idx, size int, val string) {
	for i, p := range e.c.Props {
		if p.Name != name {
			continue
		}
		parts := splitUnescaped(p.Raw, ';')
		for len(parts) < size {
			parts = append(parts, "")
		}
		if unescape(parts[idx]) == val {
			return
		}
		parts[idx] = escapeCardText(val)
		if !slices.ContainsFunc(parts, func(s string) bool { return s != "" }) {
			e.remove(named(name))
			return
		}
		e.c.Props[i].Raw = strings.Join(parts, ";")
		e.changed = true
		return
	}
	if val == "" {
		return
	}
	parts := make([]string, size)
	parts[idx] = escapeCardText(val)
	e.add(vcard.Property{Name: name, Raw: strings.Join(parts, ";")})
}

// syncList makes the properties that sel accepts stand for exactly the
// wanted values. A property that already carries a wanted value stays as it
// is, one that does not is removed, and a value without a property gets one
// from mk.
func (e *cardEditor) syncList(sel func(vcard.Property) bool, value func(vcard.Property) string, key func(string) string, want []string, mk func(string) []vcard.Property) {
	wantKeys := map[string]bool{}
	for _, w := range want {
		wantKeys[key(w)] = true
	}
	seen := map[string]bool{}
	e.remove(func(p vcard.Property) bool {
		if !sel(p) {
			return false
		}
		k := key(value(p))
		if !wantKeys[k] || seen[k] {
			return true
		}
		seen[k] = true
		return false
	})
	for _, w := range want {
		if k := key(w); !seen[k] {
			seen[k] = true
			e.add(mk(w)...)
		}
	}
}

// setSingle sets the value of the first property that sel accepts, keeping
// its parameters. An empty value removes every property sel accepts. With no
// such property it adds one from mk.
func (e *cardEditor) setSingle(sel func(vcard.Property) bool, value func(vcard.Property) string, raw func(old vcard.Property, val string) string, val string, mk func(string) []vcard.Property) {
	for i, p := range e.c.Props {
		if !sel(p) {
			continue
		}
		switch {
		case val == "":
			e.remove(sel)
		case value(p) != val:
			e.c.Props[i].Raw = raw(p, val)
			e.changed = true
		}
		return
	}
	if val != "" {
		e.add(mk(val)...)
	}
}

func identity(s string) string { return s }

func (e *cardEditor) nextGroup() string {
	for n := 1; ; n++ {
		g := "ITEM" + strconv.Itoa(n)
		if !slices.ContainsFunc(e.c.Props, func(p vcard.Property) bool { return p.Group == g }) {
			return g
		}
	}
}

// dropOrphanLabels removes X-ABLabel lines whose property is gone.
func (e *cardEditor) dropOrphanLabels() {
	e.c.Props = slices.DeleteFunc(e.c.Props, func(l vcard.Property) bool {
		return l.Name == propLabel && l.Group != "" &&
			!slices.ContainsFunc(e.c.Props, func(p vcard.Property) bool { return p.Group == l.Group && p.Name != propLabel })
	})
}

func (e *cardEditor) setPhones(field string, want []string) {
	e.syncList(func(p vcard.Property) bool { return p.Name == "TEL" && telField(e.c, p) == field },
		telValue, identity, want, func(v string) []vcard.Property {
			t := "HOME"
			if field == "work" {
				t = "WORK"
			}
			return []vcard.Property{{Name: "TEL", Params: map[string][]string{"TYPE": {t}}, Raw: escapeCardText(v)}}
		})
}

func (e *cardEditor) setMobile(val string) {
	e.setSingle(func(p vcard.Property) bool { return p.Name == "TEL" && telField(e.c, p) == "cell" },
		telValue, func(old vcard.Property, v string) string {
			if len(old.Raw) > 4 && strings.EqualFold(old.Raw[:4], "tel:") {
				return old.Raw[:4] + v
			}
			return escapeCardText(v)
		}, val, func(v string) []vcard.Property {
			return []vcard.Property{{Name: "TEL", Params: map[string][]string{"TYPE": {"CELL"}}, Raw: escapeCardText(v)}}
		})
}

func (e *cardEditor) setRelated(kind string, want []string) {
	label := map[string]string{"spouse": "Spouse", "child": "Child", "assistant": "Assistant", "manager": "Manager"}[kind]
	e.syncList(func(p vcard.Property) bool { return relatedKind(e.c, p) == kind },
		func(p vcard.Property) string { return strings.TrimSpace(p.Text()) }, identity, want,
		func(v string) []vcard.Property {
			g := e.nextGroup()
			return []vcard.Property{
				{Group: g, Name: propRelated, Raw: escapeCardText(v)},
				{Group: g, Name: propLabel, Raw: "_$!<" + label + ">!$_"},
			}
		})
}

func (e *cardEditor) setAddress(where string, a physicalAddress) {
	sel := func(p vcard.Property) bool { return p.Name == "ADR" && place(e.c, p) == where }
	for i, p := range e.c.Props {
		if !sel(p) {
			continue
		}
		switch {
		case a.empty():
			e.remove(sel)
		case addressParts(p) != a:
			parts := splitUnescaped(p.Raw, ';')
			for len(parts) < 7 {
				parts = append(parts, "")
			}
			e.c.Props[i].Raw = joinAddress(parts[0], parts[1], a)
			e.changed = true
		}
		return
	}
	if a.empty() {
		return
	}
	var params map[string][]string
	switch where {
	case "work":
		params = map[string][]string{"TYPE": {"WORK"}}
	case "home":
		params = map[string][]string{"TYPE": {"HOME"}}
	}
	e.add(vcard.Property{Name: "ADR", Params: params, Raw: joinAddress("", "", a)})
}

// joinAddress writes an ADR value. The post office box and the extended
// address are the raw, still escaped, components of the old value.
func joinAddress(pobox, ext string, a physicalAddress) string {
	return strings.Join([]string{pobox, ext, escapeCardText(a.Street), escapeCardText(a.City),
		escapeCardText(a.State), escapeCardText(a.PostalCode), escapeCardText(a.CountryOrRegion)}, ";")
}

func (e *cardEditor) setBirthday(val *string) {
	cur := birthdayOf(e.c)
	switch {
	case val == nil && cur == nil:
		return
	case val == nil:
		e.remove(named("BDAY"))
		return
	case cur != nil && *cur == *val:
		return
	}
	y, m, d, _ := parseBirthday(vcard.Property{Raw: (*val)[:10]})
	p := vcard.Property{Name: "BDAY"}
	switch {
	case y != 0 && y != noYear:
		p.Raw = fmt.Sprintf("%04d-%02d-%02d", y, m, d)
	case e.c.Version == "4.0":
		p.Raw = fmt.Sprintf("--%02d%02d", m, d)
	default:
		p.Params = map[string][]string{"X-APPLE-OMIT-YEAR": {strconv.Itoa(omitYearYear)}}
		p.Raw = fmt.Sprintf("%04d-%02d-%02d", omitYearYear, m, d)
	}
	for i, old := range e.c.Props {
		if old.Name == "BDAY" {
			e.c.Props[i] = p
			e.remove(func(q vcard.Property) bool { return q.Name == "BDAY" && !reflectEqualProp(q, p) })
			e.changed = true
			return
		}
	}
	e.add(p)
}

func reflectEqualProp(a, b vcard.Property) bool {
	return a.Group == b.Group && a.Name == b.Name && a.Raw == b.Raw
}

func fullName(given, middle, surname string) string {
	var parts []string
	for _, s := range []string{given, middle, surname} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " ")
}

// applyContact applies the fields of a request to the card. It reports
// whether the card changed. REV is refreshed only then.
func applyContact(c *vcard.Card, p contactPatch, now time.Time) (bool, *apiError) {
	birthday, aerr := validateContact(p)
	if aerr != nil {
		return false, aerr
	}
	e := &cardEditor{c: c}
	cur := contactFromCard(c)

	// Names. A display name that was made from the name parts follows them.
	nameSet := p.GivenName.Set || p.MiddleName.Set || p.Surname.Set
	e.setPart("N", 0, 5, pick(p.Surname, cur.Surname))
	e.setPart("N", 1, 5, pick(p.GivenName, cur.GivenName))
	e.setPart("N", 2, 5, pick(p.MiddleName, cur.MiddleName))
	e.setPart("N", 3, 5, pick(p.Title, cur.Title))
	e.setPart("N", 4, 5, pick(p.Generation, cur.Generation))
	switch {
	case p.DisplayName.Set:
		e.setText("FN", p.DisplayName.Val, true)
	case nameSet && cur.DisplayName == fullName(cur.GivenName, cur.MiddleName, cur.Surname):
		now := contactFromCard(c)
		if n := fullName(now.GivenName, now.MiddleName, now.Surname); n != "" {
			e.setText("FN", n, true)
		}
	}

	for _, f := range []struct {
		name string
		o    opt[string]
	}{
		{"NICKNAME", p.NickName}, {"TITLE", p.JobTitle}, {"ROLE", p.Profession}, {"NOTE", p.PersonalNotes},
		{"X-PHONETIC-FIRST-NAME", p.YomiGivenName}, {"X-PHONETIC-LAST-NAME", p.YomiSurname}, {"X-PHONETIC-ORG", p.YomiCompany},
	} {
		if f.o.Set {
			e.setText(f.name, f.o.Val, false)
		}
	}
	if p.CompanyName.Set {
		e.setPart("ORG", 0, 2, p.CompanyName.Val)
	}
	if p.Department.Set {
		e.setPart("ORG", 1, 2, p.Department.Val)
	}
	if p.BusinessHomePage.Set {
		e.setSingle(named("URL"), func(p vcard.Property) string { return strings.TrimSpace(p.Text()) },
			func(_ vcard.Property, v string) string { return v }, p.BusinessHomePage.Val,
			func(v string) []vcard.Property { return []vcard.Property{{Name: "URL", Raw: v}} })
	}
	if p.EmailAddresses.Set {
		want := make([]string, len(p.EmailAddresses.Val))
		for i, a := range p.EmailAddresses.Val {
			want[i] = strings.TrimSpace(a.Address)
		}
		e.syncList(named("EMAIL"), func(p vcard.Property) string { return strings.TrimSpace(p.Text()) }, strings.ToLower, want,
			func(v string) []vcard.Property { return []vcard.Property{{Name: "EMAIL", Raw: escapeCardText(v)}} })
	}
	if p.BusinessPhones.Set {
		e.setPhones("work", p.BusinessPhones.Val)
	}
	if p.HomePhones.Set {
		e.setPhones("home", p.HomePhones.Val)
	}
	if p.MobilePhone.Set {
		e.setMobile(p.MobilePhone.Val)
	}
	if p.IMAddresses.Set {
		e.syncList(func(p vcard.Property) bool { return p.Name == "IMPP" || p.Name == "X-SOCIALPROFILE" },
			func(p vcard.Property) string { return strings.TrimSpace(p.Raw) }, identity, p.IMAddresses.Val,
			func(v string) []vcard.Property { return []vcard.Property{{Name: "IMPP", Raw: v}} })
	}
	if p.Categories.Set {
		if !slices.Equal(cur.Categories, dedupe(p.Categories.Val)) {
			e.remove(named("CATEGORIES"))
			if want := dedupe(p.Categories.Val); len(want) > 0 {
				esc := make([]string, len(want))
				for i, w := range want {
					esc[i] = escapeCardText(w)
				}
				e.add(vcard.Property{Name: "CATEGORIES", Raw: strings.Join(esc, ",")})
			}
		}
	}
	for _, f := range []struct {
		where string
		o     opt[physicalAddress]
	}{{"home", p.HomeAddress}, {"work", p.BusinessAddress}, {"other", p.OtherAddress}} {
		if f.o.Set {
			e.setAddress(f.where, f.o.Val)
		}
	}
	if p.Birthday.Set {
		e.setBirthday(birthday)
	}
	for _, f := range []struct {
		kind string
		o    opt[string]
	}{{"spouse", p.SpouseName}, {"manager", p.Manager}, {"assistant", p.AssistantName}} {
		if f.o.Set {
			var want []string
			if v := strings.TrimSpace(f.o.Val); v != "" {
				want = []string{v}
			}
			e.setRelated(f.kind, want)
		}
	}
	if p.Children.Set {
		e.setRelated("child", p.Children.Val)
	}

	if e.changed {
		e.dropOrphanLabels()
		setRev(c, now)
	}
	return e.changed, nil
}

func pick(o opt[string], cur string) string {
	if o.Set {
		return o.Val
	}
	return cur
}

func dedupe(s []string) []string {
	out := []string{}
	for _, v := range s {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}

func setRev(c *vcard.Card, now time.Time) {
	rev := now.UTC().Format("20060102T150405Z")
	for i, p := range c.Props {
		if p.Name == "REV" {
			c.Props[i].Raw = rev
			return
		}
	}
	c.Props = append(c.Props, vcard.Property{Name: "REV", Raw: rev})
}

// newCard makes the card for a POST. It has the given UID, and a display name
// made from the name parts, the first e-mail address or the company when the
// request has none.
func newCard(p contactPatch, uid string, now time.Time) (*vcard.Card, *apiError) {
	c := &vcard.Card{Version: newCardVersion, Props: []vcard.Property{{Name: "FN"}, {Name: "UID", Raw: uid}}}
	if _, aerr := applyContact(c, p, now); aerr != nil {
		return nil, aerr
	}
	if text(c, "FN") == "" {
		got := contactFromCard(c)
		name := fullName(got.GivenName, got.MiddleName, got.Surname)
		if name == "" && len(got.EmailAddresses) > 0 {
			name = got.EmailAddresses[0].Address
		}
		if name == "" {
			name = got.CompanyName
		}
		c.Props[0].Raw = escapeCardText(name)
	}
	setRev(c, now)
	return c, nil
}

// validateContact checks the fields of a request and returns the birthday it
// carries, if any, as the Graph form the card is compared in.
func validateContact(p contactPatch) (*string, *apiError) {
	for _, f := range []struct {
		name string
		o    opt[string]
		max  int
		nl   bool
	}{
		{"displayName", p.DisplayName, maxContactText, false}, {"givenName", p.GivenName, maxContactText, false},
		{"middleName", p.MiddleName, maxContactText, false}, {"surname", p.Surname, maxContactText, false},
		{"nickName", p.NickName, maxContactText, false}, {"title", p.Title, maxContactText, false},
		{"generation", p.Generation, maxContactText, false}, {"yomiGivenName", p.YomiGivenName, maxContactText, false},
		{"yomiSurname", p.YomiSurname, maxContactText, false}, {"yomiCompany", p.YomiCompany, maxContactText, false},
		{"jobTitle", p.JobTitle, maxContactText, false}, {"companyName", p.CompanyName, maxContactText, false},
		{"department", p.Department, maxContactText, false}, {"profession", p.Profession, maxContactText, false},
		{"assistantName", p.AssistantName, maxContactText, false}, {"manager", p.Manager, maxContactText, false},
		{"spouseName", p.SpouseName, maxContactText, false}, {"mobilePhone", p.MobilePhone, maxContactText, false},
		{"businessHomePage", p.BusinessHomePage, maxContactText, false}, {"personalNotes", p.PersonalNotes, maxContactNotes, true},
	} {
		if aerr := checkCardText(f.name, f.o.Val, f.max, f.nl); aerr != nil {
			return nil, aerr
		}
	}
	for _, f := range []struct {
		name string
		o    opt[[]string]
	}{
		{"categories", p.Categories}, {"children", p.Children}, {"businessPhones", p.BusinessPhones},
		{"homePhones", p.HomePhones}, {"imAddresses", p.IMAddresses},
	} {
		if len(f.o.Val) > maxListItems {
			return nil, badRequest(f.name + " has too many entries")
		}
		for _, v := range f.o.Val {
			if aerr := checkCardText(f.name, v, maxContactText, false); aerr != nil {
				return nil, aerr
			}
		}
	}
	if len(p.EmailAddresses.Val) > maxListItems {
		return nil, badRequest("emailAddresses has too many entries")
	}
	for _, a := range p.EmailAddresses.Val {
		addr := strings.TrimSpace(a.Address)
		if addr == "" || len(addr) > maxEmailLength || strings.ContainsFunc(addr, func(r rune) bool { return r <= ' ' || r == 0x7f }) || !utf8.ValidString(addr) {
			return nil, badRequest("an e-mail address is empty or not valid")
		}
	}
	for _, a := range []struct {
		name string
		o    opt[physicalAddress]
	}{{"homeAddress", p.HomeAddress}, {"businessAddress", p.BusinessAddress}, {"otherAddress", p.OtherAddress}} {
		for _, v := range []string{a.o.Val.Street, a.o.Val.City, a.o.Val.State, a.o.Val.CountryOrRegion, a.o.Val.PostalCode} {
			if aerr := checkCardText(a.name, v, maxContactText, true); aerr != nil {
				return nil, aerr
			}
		}
	}
	if !p.Birthday.Set || p.Birthday.Null || p.Birthday.Val == "" {
		return nil, nil
	}
	s := p.Birthday.Val
	if len(s) < 10 || (len(s) > 10 && s[10] != 'T') {
		return nil, badRequest("birthday must be a date")
	}
	if _, err := time.Parse("2006-01-02", s[:10]); err != nil {
		return nil, badRequest("birthday must be a date")
	}
	norm := s[:10] + "T00:00:00Z"
	return &norm, nil
}

func checkCardText(name, v string, max int, newlines bool) *apiError {
	switch {
	case len(v) > max || !utf8.ValidString(v):
		return badRequest(name + " is too long or not valid text")
	case strings.ContainsFunc(v, func(r rune) bool {
		allowed := newlines && (r == '\n' || r == '\r' || r == '\t')
		return (r < 0x20 && !allowed) || r == 0x7f
	}):
		return badRequest(name + " must not contain control characters")
	}
	return nil
}
