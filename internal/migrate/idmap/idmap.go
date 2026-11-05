// Package idmap maps classic user ids and group ids to oCIS ids. An id is
// looked up in CSV files first and, for the ones not listed there, in the
// oCIS Graph API with an administrator's access token. Ids that no source
// knows are reported, not guessed.
package idmap

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/owncloud/ocis-groupware/internal/ocisgraph"
)

// Kind says whether an id names a user or a group.
type Kind string

const (
	User  Kind = "user"
	Group Kind = "group"
)

// Reason says why an id has no mapping.
type Reason string

const (
	// ReasonNotFound means no source knows the id.
	ReasonNotFound Reason = "not found"
	// ReasonAmbiguous means the directory returned more than one match, and
	// the mapper does not choose between them.
	ReasonAmbiguous Reason = "more than one match"
)

// Unmapped is an id that was not mapped.
type Unmapped struct {
	Kind   Kind
	ID     string
	Reason Reason
}

// Result is the outcome of one Map call. Users and Groups map classic ids to
// oCIS ids. GroupsByName lists the classic group ids whose mapping came from
// a lookup of the display name in oCIS rather than from a CSV file, sorted.
// Unmapped lists the rest in the order the ids were given, users first.
type Result struct {
	Users        map[string]string
	Groups       map[string]string
	GroupsByName []string
	Unmapped     []Unmapped
}

// Directory looks users and groups up in oCIS. *ocisgraph.Client implements
// it. The token is the administrator's access token.
type Directory interface {
	UsersByLogin(ctx context.Context, token, login string) ([]ocisgraph.User, error)
	GroupsByName(ctx context.Context, token, name string) ([]ocisgraph.Group, error)
}

// Mapper maps ids. Configure it with WithDirectory and the CSV loaders
// before calling Map.
type Mapper struct {
	dir    Directory
	token  string
	users  map[string]string
	groups map[string]string
}

// Option configures a Mapper.
type Option func(*Mapper)

// WithDirectory looks ids up in the directory using the given token.
func WithDirectory(d Directory, token string) Option {
	return func(m *Mapper) { m.dir, m.token = d, token }
}

// New returns a Mapper.
func New(opts ...Option) *Mapper {
	m := &Mapper{users: map[string]string{}, groups: map[string]string{}}
	for _, o := range opts {
		o(m)
	}
	return m
}

// LoadUsersCSV adds user mappings from a CSV stream. See LoadGroupsCSV for
// the format.
func (m *Mapper) LoadUsersCSV(r io.Reader) error { return loadCSV(r, m.users) }

// LoadGroupsCSV adds group mappings from a CSV stream with two columns: the
// classic id and the oCIS id. Blank lines and lines starting with # are
// ignored, and so is a first line whose first field is classic_id. An id
// listed twice is an error. The ids in the file win over the directory.
func (m *Mapper) LoadGroupsCSV(r io.Reader) error { return loadCSV(r, m.groups) }

func loadCSV(r io.Reader, into map[string]string) error {
	cr := csv.NewReader(r)
	cr.Comment = '#'
	cr.FieldsPerRecord = 2
	cr.TrimLeadingSpace = true
	first := true
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("idmap: %w", err)
		}
		from, to := strings.TrimSpace(rec[0]), strings.TrimSpace(rec[1])
		if first {
			first = false
			if from == "classic_id" {
				continue
			}
		}
		line, _ := cr.FieldPos(0)
		if from == "" || to == "" {
			return fmt.Errorf("idmap: line %d: both ids are required", line)
		}
		if _, dup := into[from]; dup {
			return fmt.Errorf("idmap: line %d: %q is listed twice", line, from)
		}
		into[from] = to
	}
}

// Map resolves the given classic user ids and group ids. A directory that
// cannot be asked, for example because it refuses the token, is an error and
// stops the run, since every remaining id would otherwise be reported as
// missing.
func (m *Mapper) Map(ctx context.Context, userIDs, groupIDs []string) (Result, error) {
	res := Result{Users: map[string]string{}, Groups: map[string]string{}}
	for _, id := range userIDs {
		to, why, err := m.resolve(id, m.users, func() ([]string, error) {
			us, err := m.dir.UsersByLogin(ctx, m.token, id)
			ids := make([]string, 0, len(us))
			for _, u := range us {
				ids = append(ids, u.ID)
			}
			return ids, err
		})
		if err != nil {
			return Result{}, fmt.Errorf("idmap: user %q: %w", id, err)
		}
		if why != "" {
			res.Unmapped = append(res.Unmapped, Unmapped{User, id, why})
			continue
		}
		res.Users[id] = to
	}
	for _, id := range groupIDs {
		to, why, err := m.resolve(id, m.groups, func() ([]string, error) {
			gs, err := m.dir.GroupsByName(ctx, m.token, id)
			ids := make([]string, 0, len(gs))
			for _, g := range gs {
				ids = append(ids, g.ID)
			}
			return ids, err
		})
		if err != nil {
			return Result{}, fmt.Errorf("idmap: group %q: %w", id, err)
		}
		if why != "" {
			res.Unmapped = append(res.Unmapped, Unmapped{Group, id, why})
			continue
		}
		res.Groups[id] = to
		if _, fromCSV := m.groups[id]; !fromCSV {
			res.GroupsByName = append(res.GroupsByName, id)
		}
	}
	slices.Sort(res.GroupsByName)
	return res, nil
}

func (m *Mapper) resolve(id string, csvMap map[string]string, ask func() ([]string, error)) (string, Reason, error) {
	if to, ok := csvMap[id]; ok {
		return to, "", nil
	}
	if m.dir == nil {
		return "", ReasonNotFound, nil
	}
	ids, err := ask()
	if err != nil {
		return "", "", err
	}
	switch len(ids) {
	case 0:
		return "", ReasonNotFound, nil
	case 1:
		return ids[0], "", nil
	}
	return "", ReasonAmbiguous, nil
}
