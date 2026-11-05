package graphapi

import (
	"net/http"
	"net/url"
	"strconv"
)

const (
	// defaultPageSize is the page size when the request has no $top.
	defaultPageSize = 100
	// maxPageSize is the largest $top.
	maxPageSize = 1000
)

// collectionBody is the Graph shape of a list.
type collectionBody[T any] struct {
	Value    []T    `json:"value"`
	NextLink string `json:"@odata.nextLink,omitempty"`
}

// writeCollection answers with one page of items and a nextLink when more
// follow.
func writeCollection[T any](w http.ResponseWriter, r *http.Request, items []T) {
	page, next, aerr := paginate(r, items)
	if aerr != nil {
		writeError(w, aerr)
		return
	}
	if page == nil {
		page = []T{}
	}
	writeJSON(w, http.StatusOK, collectionBody[T]{Value: page, NextLink: next})
}

// paginate returns the page of items that $top and $skip select, and the
// link to the next page, or "" on the last one.
func paginate[T any](r *http.Request, items []T) ([]T, string, *apiError) {
	q := r.URL.Query()
	top, skip := defaultPageSize, 0
	if v := q.Get("$top"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			return nil, "", badRequest("$top must be a number from 1 to " + strconv.Itoa(maxPageSize))
		}
		top = n
	}
	if v := q.Get("$skip"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, "", badRequest("$skip must be a number of 0 or more")
		}
		skip = n
	}
	if skip >= len(items) {
		return nil, "", nil
	}
	end := skip + top
	if end >= len(items) {
		return items[skip:], "", nil
	}
	return items[skip:end], nextLink(r, q, top, end), nil
}

// nextLink is the request's own URL with $skip moved to the next page.
func nextLink(r *http.Request, q url.Values, top, skip int) string {
	if r.Host == "" {
		return ""
	}
	q = cloneValues(q)
	q.Set("$skip", strconv.Itoa(skip))
	q.Set("$top", strconv.Itoa(top))
	return requestOrigin(r) + (&url.URL{Path: r.URL.Path, RawQuery: q.Encode()}).String()
}

func cloneValues(q url.Values) url.Values {
	out := make(url.Values, len(q)+2)
	for k, v := range q {
		out[k] = append([]string(nil), v...)
	}
	return out
}
