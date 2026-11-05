package graphapi

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// specPath is the OpenAPI document that describes the facade.
const specPath = "../../docs/graph-openapi.yaml"

type specDoc map[string]any

func loadSpec(t *testing.T) specDoc {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", specPath, err)
	}
	return specDoc(doc)
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

// at walks nested maps by key.
func (d specDoc) at(keys ...string) any {
	var cur any = map[string]any(d)
	for _, k := range keys {
		cur = asMap(cur)[k]
	}
	return cur
}

func (d specDoc) schema(name string) map[string]any {
	return asMap(d.at("components", "schemas", name))
}

// resolve follows a local $ref.
func (d specDoc) resolve(m map[string]any) (map[string]any, error) {
	ref, ok := m["$ref"].(string)
	if !ok {
		return m, nil
	}
	rest, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return nil, fmt.Errorf("reference %q is not local", ref)
	}
	target := d.at(strings.Split(rest, "/")...)
	if asMap(target) == nil {
		return nil, fmt.Errorf("reference %q does not resolve", ref)
	}
	return asMap(target), nil
}

var httpMethods = []string{"get", "put", "post", "delete", "patch"}

// specOperations returns "METHOD /path" for every operation of the document.
func (d specDoc) operations() []string {
	var out []string
	for p, item := range asMap(d["paths"]) {
		for m := range asMap(item) {
			out = append(out, strings.ToUpper(m)+" "+p)
		}
	}
	slices.Sort(out)
	return out
}

// routedOperations returns "METHOD /path" for every route the handler serves.
func routedOperations(t *testing.T) []string {
	t.Helper()
	sf := newShareFixture(t)
	h, err := New(Config{Store: sf.st, Registry: sf.reg, Shares: sf.shares, Directory: sf.dir})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range h.routes {
		out = append(out, r.method+" "+Prefix+"/"+strings.Join(r.segments, "/"))
	}
	for _, r := range h.extRoutes {
		out = append(out, r.method+" "+GroupwarePrefix+"/"+strings.Join(r.segments, "/"))
	}
	slices.Sort(out)
	return out
}

func TestOpenAPIAndRoutesAgree(t *testing.T) {
	doc := loadSpec(t)
	documented, routed := doc.operations(), routedOperations(t)
	for _, op := range documented {
		if !slices.Contains(routed, op) {
			t.Errorf("%s is in %s but is not routed", op, filepath.Base(specPath))
		}
	}
	for _, op := range routed {
		if !slices.Contains(documented, op) {
			t.Errorf("%s is routed but is not in %s", op, filepath.Base(specPath))
		}
	}
}

// TestOpenAPIIsWellFormed checks what a reader of the document relies on:
// every reference resolves, every path parameter is declared, and every
// operation has an answer.
func TestOpenAPIIsWellFormed(t *testing.T) {
	doc := loadSpec(t)
	if v, _ := doc["openapi"].(string); !strings.HasPrefix(v, "3.0.") {
		t.Errorf("openapi = %v, want 3.0.x", doc["openapi"])
	}
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			if _, ok := x["$ref"]; ok {
				if _, err := doc.resolve(x); err != nil {
					t.Errorf("%s: %v", path, err)
				}
			}
			for k, c := range x {
				walk(path+"/"+k, c)
			}
		case []any:
			for i, c := range x {
				walk(fmt.Sprintf("%s/%d", path, i), c)
			}
		}
	}
	walk("", map[string]any(doc))

	for p, item := range asMap(doc["paths"]) {
		for _, seg := range strings.Split(p, "/") {
			if !strings.HasPrefix(seg, "{") {
				continue
			}
			name := strings.Trim(seg, "{}")
			for _, m := range httpMethods {
				op := asMap(asMap(item)[m])
				if op == nil {
					continue
				}
				found := false
				for _, raw := range asList(op["parameters"]) {
					prm, err := doc.resolve(asMap(raw))
					if err != nil {
						continue
					}
					found = found || prm["in"] == "path" && prm["name"] == name
				}
				if !found {
					t.Errorf("%s %s does not declare the path parameter %s", strings.ToUpper(m), p, name)
				}
			}
		}
		for _, m := range httpMethods {
			if op := asMap(asMap(item)[m]); op != nil && len(asMap(op["responses"])) == 0 {
				t.Errorf("%s %s has no responses", strings.ToUpper(m), p)
			}
		}
	}
}

// checkSchema reports where a JSON value does not fit the schema. A property
// the schema does not name is an error, so a field added to the facade but not
// to the document fails the test.
func (d specDoc) check(path string, s map[string]any, v any) []string {
	if v == nil && s["nullable"] == true {
		return nil
	}
	if all := asList(s["allOf"]); all != nil {
		var errs []string
		for _, sub := range all {
			errs = append(errs, d.check(path, asMap(sub), v)...)
		}
		return errs
	}
	s, err := d.resolve(s)
	if err != nil {
		return []string{path + ": " + err.Error()}
	}
	if v == nil {
		if s["nullable"] == true {
			return nil
		}
		return []string{path + ": null where the schema does not allow it"}
	}
	var errs []string
	if enum := asList(s["enum"]); enum != nil && !slices.Contains(enum, v) {
		errs = append(errs, fmt.Sprintf("%s: %v is not one of %v", path, v, enum))
	}
	switch s["type"] {
	case "object":
		m, ok := v.(map[string]any)
		if !ok {
			return append(errs, fmt.Sprintf("%s: %T, want an object", path, v))
		}
		props := asMap(s["properties"])
		for k, val := range m {
			sub, ok := props[k]
			if !ok {
				errs = append(errs, fmt.Sprintf("%s.%s: not in the schema", path, k))
				continue
			}
			errs = append(errs, d.check(path+"."+k, asMap(sub), val)...)
		}
		for _, k := range asList(s["required"]) {
			if _, ok := m[k.(string)]; !ok {
				errs = append(errs, fmt.Sprintf("%s.%v: required but absent", path, k))
			}
		}
	case "array":
		l, ok := v.([]any)
		if !ok {
			return append(errs, fmt.Sprintf("%s: %T, want an array", path, v))
		}
		for i, it := range l {
			errs = append(errs, d.check(fmt.Sprintf("%s[%d]", path, i), asMap(s["items"]), it)...)
		}
	case "string":
		if _, ok := v.(string); !ok {
			errs = append(errs, fmt.Sprintf("%s: %T, want a string", path, v))
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			errs = append(errs, fmt.Sprintf("%s: %T, want a boolean", path, v))
		}
	case "integer":
		if f, ok := v.(float64); !ok || f != math.Trunc(f) {
			errs = append(errs, fmt.Sprintf("%s: %v, want an integer", path, v))
		}
	}
	return errs
}

// contractSchemas names the schema each contract fixture has to fit.
var contractSchemas = map[string]string{
	"account":                  "Account",
	"calendar":                 "Calendar",
	"calendar-shared":          "Calendar",
	"calendars":                "CalendarPage",
	"calendars-page":           "CalendarPage",
	"calendar-permission":      "Permission",
	"calendar-permissions":     "PermissionPage",
	"calendarview":             "EventPage",
	"contact":                  "Contact",
	"contacts":                 "ContactPage",
	"contact-folder":           "ContactFolder",
	"contact-folder-shared":    "ContactFolder",
	"contact-folders":          "ContactFolderPage",
	"contactfolder-permission": "Permission",
	"event":                    "Event",
	"event-occurrence":         "Event",
	"event-series":             "Event",
	"event-zone":               "Event",
	"events":                   "EventPage",
	"import-report":            "ImportReport",
	"import-report-contacts":   "ImportReport",
}

func TestOpenAPIFitsTheContractFixtures(t *testing.T) {
	doc := loadSpec(t)
	files, err := filepath.Glob(filepath.Join("testdata", "contract", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		schema, ok := contractSchemas[name]
		if !ok {
			t.Errorf("fixture %s has no schema in contractSchemas", name)
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		s := doc.schema(schema)
		if s == nil {
			t.Errorf("%s: no schema %s in the document", name, schema)
			continue
		}
		for _, e := range doc.check(name, s, v) {
			t.Errorf("fixture %s against %s: %s", name, schema, e)
		}
	}
	for name := range contractSchemas {
		if _, err := os.Stat(filepath.Join("testdata", "contract", name+".json")); err != nil {
			t.Errorf("contractSchemas names %s, which has no fixture", name)
		}
	}
}

// fieldInfo returns the JSON names of a struct's fields and which of them
// are always written.
func fieldInfo(typ reflect.Type) (all, always []string) {
	for i := range typ.NumField() {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		all = append(all, name)
		if !strings.Contains(opts, "omitempty") {
			always = append(always, name)
		}
	}
	slices.Sort(all)
	slices.Sort(always)
	return all, always
}

func TestOpenAPISchemasMatchTheGoTypes(t *testing.T) {
	doc := loadSpec(t)
	for schema, typ := range map[string]reflect.Type{
		"Event":             reflect.TypeFor[eventJSON](),
		"Body":              reflect.TypeFor[bodyJSON](),
		"DateTimeZone":      reflect.TypeFor[dateTimeZone](),
		"Location":          reflect.TypeFor[locationJSON](),
		"Attendee":          reflect.TypeFor[attendeeJSON](),
		"ResponseStatus":    reflect.TypeFor[responseStatus](),
		"Recipient":         reflect.TypeFor[recipientJSON](),
		"EmailAddress":      reflect.TypeFor[emailAddress](),
		"Recurrence":        reflect.TypeFor[recurrenceJSON](),
		"RecurrencePattern": reflect.TypeFor[recurrencePattern](),
		"RecurrenceRange":   reflect.TypeFor[recurrenceRange](),
		"Calendar":          reflect.TypeFor[calendarJSON](),
		"ContactFolder":     reflect.TypeFor[contactFolderJSON](),
		"Contact":           reflect.TypeFor[contactJSON](),
		"PhysicalAddress":   reflect.TypeFor[physicalAddress](),
		"ContactGroup":      reflect.TypeFor[contactGroup](),
		"Permission":        reflect.TypeFor[permissionJSON](),
		"Grantee":           reflect.TypeFor[granteeJSON](),
		"Photo":             reflect.TypeFor[photoJSON](),
		"Account":           reflect.TypeFor[accountJSON](),
		"ImportResult":      reflect.TypeFor[importResult](),
		"ImportReport":      reflect.TypeFor[importReport](),
	} {
		s := doc.schema(schema)
		if s == nil {
			t.Errorf("no schema %s", schema)
			continue
		}
		var props, required []string
		for k := range asMap(s["properties"]) {
			props = append(props, k)
		}
		for _, k := range asList(s["required"]) {
			required = append(required, k.(string))
		}
		slices.Sort(props)
		slices.Sort(required)
		all, always := fieldInfo(typ)
		if !slices.Equal(props, all) {
			t.Errorf("%s: properties %v, Go fields %v", schema, props, all)
		}
		if !slices.Equal(required, always) {
			t.Errorf("%s: required %v, always written %v", schema, required, always)
		}
	}
}

// TestOpenAPIInputsMatchTheRequestTypes checks the fields a write accepts.
func TestOpenAPIInputsMatchTheRequestTypes(t *testing.T) {
	doc := loadSpec(t)
	for schema, typ := range map[string]reflect.Type{
		"EventInput":      reflect.TypeFor[eventPatch](),
		"ContactInput":    reflect.TypeFor[contactPatch](),
		"CalendarInput":   reflect.TypeFor[calendarChange](),
		"PermissionInput": reflect.TypeFor[permissionChange](),
	} {
		s := doc.schema(schema)
		if s == nil {
			t.Errorf("no schema %s", schema)
			continue
		}
		var props []string
		for k := range asMap(s["properties"]) {
			props = append(props, k)
		}
		slices.Sort(props)
		all, _ := fieldInfo(typ)
		if !slices.Equal(props, all) {
			t.Errorf("%s: properties %v, request fields %v", schema, props, all)
		}
	}
	// A contact folder is written with displayName only.
	if s := doc.schema("ContactFolderInput"); asMap(asMap(s["properties"])["displayName"]) == nil {
		t.Error("ContactFolderInput has no displayName")
	}
}

// TestOpenAPIListsTheQueryOptionsOfEachList checks that the lists that take
// $orderby and $filter say so, and the others do not.
func TestOpenAPIListsTheQueryOptionsOfEachList(t *testing.T) {
	doc := loadSpec(t)
	has := func(p, name string) bool {
		op := asMap(asMap(asMap(doc["paths"])[p])["get"])
		for _, raw := range asList(op["parameters"]) {
			if ref, _ := asMap(raw)["$ref"].(string); strings.HasSuffix(ref, "/"+name) {
				return true
			}
		}
		return false
	}
	for _, tc := range []struct {
		path            string
		orderby, filter bool
	}{
		{Prefix + "/me/events", true, true},
		{Prefix + "/me/calendar/events", true, true},
		{Prefix + "/me/calendars/{id}/events", true, true},
		{Prefix + "/me/calendarView", true, true},
		{Prefix + "/me/contacts", true, false},
		{Prefix + "/me/contactFolders/{id}/contacts", true, false},
		{Prefix + "/me/calendars", false, false},
		{Prefix + "/me/contactFolders", false, false},
	} {
		if got := has(tc.path, "orderby"); got != tc.orderby {
			t.Errorf("%s: $orderby documented = %v, want %v", tc.path, got, tc.orderby)
		}
		if got := has(tc.path, "filter"); got != tc.filter {
			t.Errorf("%s: $filter documented = %v, want %v", tc.path, got, tc.filter)
		}
		if !has(tc.path, "select") || !has(tc.path, "top") || !has(tc.path, "skip") {
			t.Errorf("%s: $select, $top and $skip must be documented", tc.path)
		}
	}
	// The routes are real: the same options are accepted or refused.
	f := newFixture(t)
	for _, tc := range []struct {
		target string
		want   int
	}{
		{q(Prefix+"/me/events", "$filter", "subject eq 'x'"), http.StatusOK},
		{q(Prefix+"/me/contacts", "$filter", "displayName eq 'x'"), http.StatusNotImplemented},
		{q(Prefix+"/me/calendars", "$orderby", "name"), http.StatusNotImplemented},
		{q(Prefix+"/me/contacts", "$orderby", "surname"), http.StatusOK},
	} {
		if rec := f.do(http.MethodGet, tc.target, "einstein", ""); rec.Code != tc.want {
			t.Errorf("%s = %d, want %d", tc.target, rec.Code, tc.want)
		}
	}
}
