package graphapi

import (
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strings"
)

// listSpec says what a list accepts besides $top, $skip and $select. A nil
// spec accepts nothing more.
type listSpec[T any] struct {
	// order maps the properties $orderby accepts to their comparison.
	order map[string]func(a, b T) int
	// filter reads a $filter expression into a test. A nil filter means the
	// list has no $filter.
	filter func(expr string) (func(T) bool, *apiError)
}

func notImplementedOption(name string) *apiError {
	return newError(http.StatusNotImplemented, codeNotImplemented, name+" is not supported")
}

// checkOptions answers 501 for the OData options no list serves, so a client
// never gets an unfiltered list in place of a filtered one.
func checkOptions(r *http.Request) *apiError {
	for k := range r.URL.Query() {
		if strings.HasPrefix(k, "$") {
			switch k {
			case "$top", "$skip", "$select", "$orderby", "$filter":
			default:
				return notImplementedOption(k)
			}
		}
	}
	return nil
}

// jsonFields returns the JSON names of the fields of a struct type.
func jsonFields(t reflect.Type) []string {
	var out []string
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		switch name {
		case "-":
			continue
		case "":
			name = f.Name
		}
		out = append(out, name)
	}
	return out
}

// alwaysSelected are the fields every $select answer keeps, because a client
// needs them to address the item and to send If-Match.
var alwaysSelected = []string{"id", "@odata.etag"}

// parseSelect reads $select as a list of top-level fields of T. It returns
// nil when the request has none.
func parseSelect[T any](r *http.Request) ([]string, *apiError) {
	if !r.URL.Query().Has("$select") {
		return nil, nil
	}
	fields := jsonFields(reflect.TypeFor[T]())
	var out []string
	for _, name := range strings.Split(r.URL.Query().Get("$select"), ",") {
		name = strings.TrimSpace(name)
		switch {
		case name == "":
			return nil, badRequest("$select must be a comma-separated list of property names")
		case strings.Contains(name, "/"):
			return nil, notImplementedOption("$select of a nested property")
		case !slices.Contains(fields, name):
			return nil, badRequest("$select names a property the resource does not have: " + name)
		}
		out = append(out, name)
	}
	return out, nil
}

// project keeps the selected fields of v, plus the ones that address it.
func project(v any, sel []string) (any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	out := make(map[string]json.RawMessage, len(sel)+len(alwaysSelected))
	for _, k := range append(slices.Clone(sel), alwaysSelected...) {
		if val, ok := all[k]; ok {
			out[k] = val
		}
	}
	return out, nil
}

// writeItem answers with one item, cut down to $select when the request has
// one.
func writeItem[T any](w http.ResponseWriter, r *http.Request, status int, item T) {
	if aerr := checkOptions(r); aerr != nil {
		writeError(w, aerr)
		return
	}
	if r.URL.Query().Has("$filter") || r.URL.Query().Has("$orderby") {
		writeError(w, notImplementedOption("$filter and $orderby on one item"))
		return
	}
	sel, aerr := parseSelect[T](r)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if sel == nil {
		writeJSON(w, status, item)
		return
	}
	out, err := project(item, sel)
	if err != nil {
		writeError(w, internalError())
		return
	}
	writeJSON(w, status, out)
}

// orderBy is a parsed $orderby: one property and a direction.
type orderBy struct {
	key  string
	desc bool
}

func parseOrderBy[T any](r *http.Request, spec *listSpec[T]) (*orderBy, *apiError) {
	if !r.URL.Query().Has("$orderby") {
		return nil, nil
	}
	expr := strings.TrimSpace(r.URL.Query().Get("$orderby"))
	if strings.Contains(expr, ",") {
		return nil, notImplementedOption("$orderby with more than one property")
	}
	parts := strings.Fields(expr)
	if len(parts) == 0 || len(parts) > 2 {
		return nil, badRequest("$orderby must be a property name with an optional asc or desc")
	}
	ob := &orderBy{key: parts[0]}
	if len(parts) == 2 {
		switch strings.ToLower(parts[1]) {
		case "asc":
		case "desc":
			ob.desc = true
		default:
			return nil, badRequest("$orderby direction must be asc or desc")
		}
	}
	if spec != nil && spec.order[ob.key] != nil {
		return ob, nil
	}
	top, _, _ := strings.Cut(ob.key, "/")
	if spec == nil || !slices.Contains(jsonFields(reflect.TypeFor[T]()), top) {
		if spec == nil {
			return nil, notImplementedOption("$orderby")
		}
		return nil, badRequest("$orderby names a property the resource does not have: " + ob.key)
	}
	return nil, notImplementedOption("$orderby " + ob.key)
}
