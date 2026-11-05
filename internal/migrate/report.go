package migrate

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

// Status values in a report. In a dry run they say what a real run would do.
const (
	StatusCreated  = "created"
	StatusExisting = "existing"
	StatusUpdated  = "updated"
	StatusSkipped  = "skipped"
	StatusFailed   = "failed"
)

// Report says what a run did, or with DryRun what it would do.
type Report struct {
	DryRun bool          `json:"dry_run"`
	Users  []*UserReport `json:"users"`
	// Shares lists the shares that were looked at.
	Shares []ShareReport `json:"shares"`
	// NotMigrated lists what classic holds and the service does not take
	// over: public links, subscriptions and generated data.
	NotMigrated []NotMigrated `json:"not_migrated"`
	// Unmapped lists the users and groups that oCIS does not know. What
	// belongs to them is skipped.
	Unmapped []UnmappedID `json:"unmapped"`
	// GroupsByName lists the classic groups mapped to an oCIS group by
	// display name rather than by the CSV file. Shares to these groups
	// reach whoever is in the oCIS group with that name, so check them.
	GroupsByName []string `json:"groups_matched_by_name,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
	Totals       Totals   `json:"totals"`

	index map[string]*UserReport
}

// UserReport is the outcome for one classic user.
type UserReport struct {
	Login string `json:"login"`
	// OCISID and Owner are empty for a user that was skipped before they
	// were known.
	OCISID       string             `json:"ocis_id,omitempty"`
	Owner        string             `json:"owner,omitempty"`
	Status       string             `json:"status"`
	Reason       string             `json:"reason,omitempty"`
	Calendars    []CollectionReport `json:"calendars"`
	AddressBooks []CollectionReport `json:"address_books"`
	Tokens       TokenReport        `json:"app_tokens"`
}

// CollectionReport is the outcome for one calendar or address book.
type CollectionReport struct {
	Name        string       `json:"name"`
	DisplayName string       `json:"display_name,omitempty"`
	Status      string       `json:"status"`
	Reason      string       `json:"reason,omitempty"`
	Objects     ObjectCounts `json:"objects"`
	// Problems names the objects that were not copied and why.
	Problems []string `json:"problems,omitempty"`
}

// ObjectCounts counts what happened to the objects of one collection.
type ObjectCounts struct {
	// Added were new in the target.
	Added int `json:"added"`
	// Unchanged were there already with the same content.
	Unchanged int `json:"unchanged"`
	// Different were there already with other content and were left alone,
	// since the target may have been edited since an earlier run.
	Different int `json:"different"`
	// Skipped are of a kind the service does not hold, such as tasks.
	Skipped int `json:"skipped"`
	// Failed could not be written.
	Failed int `json:"failed"`
}

// TokenReport counts the app tokens of one user.
type TokenReport struct {
	Imported int `json:"imported"`
	// Existing were imported by an earlier run, found by name.
	Existing int      `json:"existing"`
	Failed   int      `json:"failed"`
	Problems []string `json:"problems,omitempty"`
}

// ShareReport is the outcome for one grant.
type ShareReport struct {
	Owner      string `json:"owner"`
	Kind       string `json:"kind"`
	Collection string `json:"collection"`
	Grantee    string `json:"grantee"`
	Access     string `json:"access"`
	Status     string `json:"status"`
	Reason     string `json:"reason,omitempty"`
}

// NotMigrated is one thing that is left behind.
type NotMigrated struct {
	What   string `json:"what"`
	Owner  string `json:"owner,omitempty"`
	Name   string `json:"name,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// UnmappedID is a classic user or group without an oCIS counterpart.
type UnmappedID struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// Totals sums the report up.
type Totals struct {
	Users         int `json:"users"`
	UsersSkipped  int `json:"users_skipped"`
	Collections   int `json:"collections"`
	ObjectsAdded  int `json:"objects_added"`
	ObjectsFailed int `json:"objects_failed"`
	Tokens        int `json:"tokens_imported"`
	Shares        int `json:"shares_written"`
	NotMigrated   int `json:"not_migrated"`
	Problems      int `json:"problems"`
}

// User returns the entry for the classic user, adding it on first use.
func (r *Report) User(login string) *UserReport {
	if r.index == nil {
		r.index = map[string]*UserReport{}
	}
	u := r.index[login]
	if u == nil {
		u = &UserReport{Login: login, Status: StatusCreated}
		r.index[login] = u
		r.Users = append(r.Users, u)
	}
	return u
}

// Skip marks the user as left out.
func (u *UserReport) Skip(reason string) { u.Status, u.Reason = StatusSkipped, reason }

func (r *Report) warn(format string, args ...any) {
	r.Warnings = append(r.Warnings, fmt.Sprintf(format, args...))
}

// finish sorts the lists and computes the totals, so that the same run gives
// the same report.
func (r *Report) finish() {
	slices.SortFunc(r.Users, func(a, b *UserReport) int { return cmp.Compare(a.Login, b.Login) })
	slices.SortFunc(r.Shares, func(a, b ShareReport) int {
		return cmp.Or(cmp.Compare(a.Owner, b.Owner), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Collection, b.Collection), cmp.Compare(a.Grantee, b.Grantee))
	})
	slices.SortFunc(r.NotMigrated, func(a, b NotMigrated) int {
		return cmp.Or(cmp.Compare(a.What, b.What), cmp.Compare(a.Owner, b.Owner), cmp.Compare(a.Name, b.Name))
	})
	var t Totals
	for _, u := range r.Users {
		if u.Calendars == nil {
			u.Calendars = []CollectionReport{}
		}
		if u.AddressBooks == nil {
			u.AddressBooks = []CollectionReport{}
		}
		t.Users++
		if u.Status == StatusSkipped || u.Status == StatusFailed {
			t.UsersSkipped++
		}
		for _, cs := range [][]CollectionReport{u.Calendars, u.AddressBooks} {
			for _, c := range cs {
				t.Collections++
				t.ObjectsAdded += c.Objects.Added
				t.ObjectsFailed += c.Objects.Failed
				if c.Status == StatusFailed {
					t.Problems++
				}
			}
		}
		t.Tokens += u.Tokens.Imported
		t.Problems += u.Tokens.Failed
	}
	for _, s := range r.Shares {
		switch s.Status {
		case StatusCreated, StatusUpdated:
			t.Shares++
		case StatusFailed:
			t.Problems++
		}
	}
	t.NotMigrated = len(r.NotMigrated)
	t.Problems += t.ObjectsFailed
	r.Totals = t
	if r.Shares == nil {
		r.Shares = []ShareReport{}
	}
	if r.NotMigrated == nil {
		r.NotMigrated = []NotMigrated{}
	}
	if r.Unmapped == nil {
		r.Unmapped = []UnmappedID{}
	}
	if r.Users == nil {
		r.Users = []*UserReport{}
	}
}

// WriteJSON writes the report as indented JSON.
func (r *Report) WriteJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// WriteText writes the report for people.
func (r *Report) WriteText(w io.Writer) error {
	var b strings.Builder
	verb := func(done, would string) string {
		if r.DryRun {
			return would
		}
		return done
	}
	status := func(s string) string {
		if r.DryRun {
			switch s {
			case StatusCreated:
				return "would create"
			case StatusUpdated:
				return "would update"
			}
		}
		return s
	}
	if r.DryRun {
		b.WriteString("Dry run: nothing was written. The report says what a real run would do.\n\n")
	}
	for _, u := range r.Users {
		fmt.Fprintf(&b, "User %s", u.Login)
		if u.Owner != "" {
			fmt.Fprintf(&b, " (owner %s)", u.Owner)
		}
		fmt.Fprintf(&b, ": %s", u.statusText(r.DryRun))
		if u.Reason != "" {
			fmt.Fprintf(&b, ", %s", u.Reason)
		}
		b.WriteString("\n")
		for _, group := range []struct {
			label string
			list  []CollectionReport
		}{{"calendar", u.Calendars}, {"address book", u.AddressBooks}} {
			for _, c := range group.list {
				fmt.Fprintf(&b, "  %s %s: %s", group.label, c.Name, status(c.Status))
				if c.Reason != "" {
					fmt.Fprintf(&b, " (%s)", c.Reason)
				}
				o := c.Objects
				fmt.Fprintf(&b, "; %d %s, %d unchanged", o.Added, verb("added", "to add"), o.Unchanged)
				if o.Different > 0 {
					fmt.Fprintf(&b, ", %d differ and were kept as they are", o.Different)
				}
				if o.Skipped > 0 {
					fmt.Fprintf(&b, ", %d skipped", o.Skipped)
				}
				if o.Failed > 0 {
					fmt.Fprintf(&b, ", %d failed", o.Failed)
				}
				b.WriteString("\n")
				for _, p := range c.Problems {
					fmt.Fprintf(&b, "    %s\n", p)
				}
			}
		}
		if t := u.Tokens; t.Imported+t.Existing+t.Failed > 0 {
			fmt.Fprintf(&b, "  app tokens: %d %s, %d existing", t.Imported, verb("imported", "to import"), t.Existing)
			if t.Failed > 0 {
				fmt.Fprintf(&b, ", %d failed", t.Failed)
			}
			b.WriteString("\n")
			for _, p := range t.Problems {
				fmt.Fprintf(&b, "    %s\n", p)
			}
		}
	}

	if len(r.Shares) > 0 {
		b.WriteString("\nShares\n")
		for _, s := range r.Shares {
			fmt.Fprintf(&b, "  %s %s/%s with %s (%s): %s", s.Kind, s.Owner, s.Collection, s.Grantee, s.Access, status(s.Status))
			if s.Reason != "" {
				fmt.Fprintf(&b, ", %s", s.Reason)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\nNot migrated\n")
	if len(r.NotMigrated) == 0 {
		b.WriteString("  nothing\n")
	}
	for _, n := range r.NotMigrated {
		fmt.Fprintf(&b, "  %s", n.What)
		if n.Owner != "" || n.Name != "" {
			fmt.Fprintf(&b, " %s", strings.Trim(n.Owner+"/"+n.Name, "/"))
		}
		if n.Detail != "" {
			fmt.Fprintf(&b, ": %s", n.Detail)
		}
		b.WriteString("\n")
	}

	if len(r.Unmapped) > 0 {
		b.WriteString("\nWithout an oCIS counterpart\n")
		for _, u := range r.Unmapped {
			fmt.Fprintf(&b, "  %s %s: %s\n", u.Kind, u.ID, u.Reason)
		}
	}
	if len(r.GroupsByName) > 0 {
		b.WriteString("\nGroups matched to oCIS by display name\n")
		for _, g := range r.GroupsByName {
			fmt.Fprintf(&b, "  %s\n", g)
		}
	}
	if len(r.Warnings) > 0 {
		b.WriteString("\nWarnings\n")
		for _, w := range r.Warnings {
			fmt.Fprintf(&b, "  %s\n", w)
		}
	}

	t := r.Totals
	fmt.Fprintf(&b, "\nSummary: %d users (%d skipped), %d collections, %d objects %s, %d app tokens %s, %d shares %s, %d not migrated, %d problems\n",
		t.Users, t.UsersSkipped, t.Collections, t.ObjectsAdded, verb("added", "to add"), t.Tokens, verb("imported", "to import"),
		t.Shares, verb("written", "to write"), t.NotMigrated, t.Problems)
	_, err := io.WriteString(w, b.String())
	return err
}

func (u *UserReport) statusText(dry bool) string {
	if dry && u.Status == StatusCreated {
		return "would be created"
	}
	switch u.Status {
	case StatusCreated:
		return "created"
	case StatusExisting:
		return "already there"
	}
	return u.Status
}
