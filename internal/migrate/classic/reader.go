package classic

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // registers "mysql"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx"
)

// Driver names a supported database.
type Driver string

const (
	// MySQL covers MySQL and MariaDB.
	MySQL    Driver = "mysql"
	Postgres Driver = "postgres"
)

// ParseDriver accepts the names an administrator is likely to use, including
// the classic configuration's own (mysql, pgsql).
func ParseDriver(s string) (Driver, error) {
	switch strings.ToLower(s) {
	case "mysql", "mariadb":
		return MySQL, nil
	case "postgres", "postgresql", "pgsql":
		return Postgres, nil
	}
	return "", fmt.Errorf("classic: unsupported database %q (use mysql or postgres; export SQLite and Oracle installations to MariaDB first)", s)
}

// DefaultTablePrefix is the table prefix of a classic installation unless
// its configuration says otherwise.
const DefaultTablePrefix = "oc_"

var prefixPattern = regexp.MustCompile(`^[A-Za-z0-9_]*$`)

// Option configures a Reader.
type Option func(*Reader)

// WithTablePrefix sets the table prefix, for installations that changed
// dbtableprefix.
func WithTablePrefix(p string) Option { return func(r *Reader) { r.prefix = p } }

// Reader reads a classic database. It is safe for concurrent use.
type Reader struct {
	db     *sql.DB
	driver Driver
	prefix string
}

// Open connects to the database and checks the connection. dsn is the
// go-sql-driver DSN for MySQL (user:pass@tcp(host:3306)/db) or a PostgreSQL
// URL (postgres://user:pass@host:5432/db).
func Open(ctx context.Context, driver Driver, dsn string, opts ...Option) (*Reader, error) {
	name := "mysql"
	switch driver {
	case MySQL:
	case Postgres:
		name = "pgx"
	default:
		return nil, fmt.Errorf("classic: unsupported driver %q", driver)
	}
	db, err := sql.Open(name, dsn)
	if err != nil {
		return nil, fmt.Errorf("classic: %w", err)
	}
	r, err := NewReader(db, driver, opts...)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("classic: connect: %w", err)
	}
	return r, nil
}

// NewReader wraps an open database handle. Close closes the handle.
func NewReader(db *sql.DB, driver Driver, opts ...Option) (*Reader, error) {
	r := &Reader{db: db, driver: driver, prefix: DefaultTablePrefix}
	for _, o := range opts {
		o(r)
	}
	if !prefixPattern.MatchString(r.prefix) {
		return nil, fmt.Errorf("classic: table prefix %q has characters other than letters, digits and underscore", r.prefix)
	}
	return r, nil
}

// Close releases the database handle.
func (r *Reader) Close() error {
	if r.db == nil {
		return nil
	}
	return r.db.Close()
}

// q expands {name} to the prefixed table name and ? to the driver's
// placeholder.
func (r *Reader) q(query string) string {
	for {
		i := strings.IndexByte(query, '{')
		if i < 0 {
			break
		}
		j := strings.IndexByte(query[i:], '}')
		query = query[:i] + r.prefix + query[i+1:i+j] + query[i+j+1:]
	}
	if r.driver == Postgres {
		var b strings.Builder
		n := 0
		for _, c := range query {
			if c == '?' {
				n++
				fmt.Fprintf(&b, "$%d", n)
				continue
			}
			b.WriteRune(c)
		}
		query = b.String()
	}
	return query
}

func (r *Reader) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	rows, err := r.db.QueryContext(ctx, r.q(query), args...)
	if err != nil {
		return nil, fmt.Errorf("classic: %w", err)
	}
	return rows, nil
}

// each runs query and calls fn for every row. fn scans the row.
func (r *Reader) each(ctx context.Context, query string, args []any, fn func(*sql.Rows) error) error {
	rows, err := r.query(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("classic: %w", err)
	}
	return nil
}

func unixTime(v sql.NullInt64) time.Time {
	if !v.Valid || v.Int64 <= 0 {
		return time.Time{}
	}
	return time.Unix(v.Int64, 0).UTC()
}

// Users returns the accounts, ordered by id.
func (r *Reader) Users(ctx context.Context) ([]User, error) {
	var out []User
	err := r.each(ctx, `SELECT user_id, display_name, email, state, backend FROM {accounts} ORDER BY user_id`, nil, func(rows *sql.Rows) error {
		var u User
		var name, mail sql.NullString
		var state int
		if err := rows.Scan(&u.ID, &name, &mail, &state, &u.Backend); err != nil {
			return err
		}
		u.DisplayName, u.Email = name.String, mail.String
		// 0 is an account that has not signed in yet, 1 is enabled.
		u.Enabled = state == 0 || state == 1
		out = append(out, u)
		return nil
	})
	return out, err
}

// Groups returns the groups with their members, ordered by id. Members that
// the group tables list but no account matches are still returned.
func (r *Reader) Groups(ctx context.Context) ([]Group, error) {
	var out []Group
	idx := map[string]int{}
	err := r.each(ctx, `SELECT gid FROM {groups} ORDER BY gid`, nil, func(rows *sql.Rows) error {
		var g Group
		if err := rows.Scan(&g.ID); err != nil {
			return err
		}
		idx[g.ID] = len(out)
		out = append(out, g)
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = r.each(ctx, `SELECT gid, uid FROM {group_user} ORDER BY gid, uid`, nil, func(rows *sql.Rows) error {
		var gid, uid string
		if err := rows.Scan(&gid, &uid); err != nil {
			return err
		}
		i, ok := idx[gid]
		if !ok {
			idx[gid] = len(out)
			i = len(out)
			out = append(out, Group{ID: gid})
		}
		out[i].Members = append(out[i].Members, uid)
		return nil
	})
	return out, err
}

// Calendars returns every calendar, ordered by row id.
func (r *Reader) Calendars(ctx context.Context) ([]Calendar, error) {
	var out []Calendar
	err := r.each(ctx, `SELECT id, principaluri, uri, displayname, description, calendarcolor, timezone, calendarorder, components, transparent, synctoken
		FROM {calendars} ORDER BY id`, nil, func(rows *sql.Rows) error {
		var c Calendar
		var principal string
		var name, desc, color, tz, comps sql.NullString
		var transparent int
		if err := rows.Scan(&c.ID, &principal, &c.URI, &name, &desc, &color, &tz, &c.Order, &comps, &transparent, &c.SyncToken); err != nil {
			return err
		}
		owner, err := ParsePrincipal(principal)
		if err != nil {
			return fmt.Errorf("calendar %d: %w", c.ID, err)
		}
		c.Owner, c.DisplayName, c.Description, c.Color, c.Timezone = owner, name.String, desc.String, color.String, tz.String
		c.Transparent = transparent != 0
		if comps.String != "" {
			c.Components = strings.Split(comps.String, ",")
		}
		out = append(out, c)
		return nil
	})
	return out, err
}

// CalendarObjects calls fn for each object of the calendar, in row order,
// without holding them all in memory. A non-nil error from fn stops the
// read and is returned.
func (r *Reader) CalendarObjects(ctx context.Context, calendarID int64, fn func(Object) error) error {
	return r.each(ctx, `SELECT uri, calendardata, etag, lastmodified, componenttype, uid, classification, size
		FROM {calendarobjects} WHERE calendarid = ? ORDER BY id`, []any{calendarID}, func(rows *sql.Rows) error {
		var o Object
		var etag, typ, uid sql.NullString
		var mod sql.NullInt64
		var class sql.NullInt64
		if err := rows.Scan(&o.URI, &o.Data, &etag, &mod, &typ, &uid, &class, &o.Size); err != nil {
			return err
		}
		o.ETag, o.Type, o.UID = etag.String, typ.String, uid.String
		o.LastModified, o.Classification = unixTime(mod), int(class.Int64)
		return fn(o)
	})
}

// AddressBooks returns every address book, ordered by row id. The system
// address book, which lists all users, belongs to the system principal.
func (r *Reader) AddressBooks(ctx context.Context) ([]AddressBook, error) {
	var out []AddressBook
	err := r.each(ctx, `SELECT id, principaluri, uri, displayname, description, synctoken FROM {addressbooks} ORDER BY id`, nil, func(rows *sql.Rows) error {
		var b AddressBook
		var principal string
		var name, desc sql.NullString
		if err := rows.Scan(&b.ID, &principal, &b.URI, &name, &desc, &b.SyncToken); err != nil {
			return err
		}
		owner, err := ParsePrincipal(principal)
		if err != nil {
			return fmt.Errorf("address book %d: %w", b.ID, err)
		}
		b.Owner, b.DisplayName, b.Description = owner, name.String, desc.String
		out = append(out, b)
		return nil
	})
	return out, err
}

// Cards calls fn for each card of the address book, in row order.
func (r *Reader) Cards(ctx context.Context, addressBookID int64, fn func(Card) error) error {
	return r.each(ctx, `SELECT uri, carddata, etag, lastmodified, size FROM {cards} WHERE addressbookid = ? ORDER BY id`,
		[]any{addressBookID}, func(rows *sql.Rows) error {
			var c Card
			var etag sql.NullString
			var mod sql.NullInt64
			if err := rows.Scan(&c.URI, &c.Data, &etag, &mod, &c.Size); err != nil {
				return err
			}
			c.ETag, c.LastModified = etag.String, unixTime(mod)
			return fn(c)
		})
}

// Subscriptions returns the calendar subscriptions, ordered by row id.
func (r *Reader) Subscriptions(ctx context.Context) ([]Subscription, error) {
	var out []Subscription
	err := r.each(ctx, `SELECT principaluri, uri, source, displayname, calendarcolor, calendarorder, refreshrate
		FROM {calendarsubscriptions} ORDER BY id`, nil, func(rows *sql.Rows) error {
		var s Subscription
		var principal string
		var src, name, color, rate sql.NullString
		if err := rows.Scan(&principal, &s.URI, &src, &name, &color, &s.Order, &rate); err != nil {
			return err
		}
		owner, err := ParsePrincipal(principal)
		if err != nil {
			return fmt.Errorf("subscription %q: %w", s.URI, err)
		}
		s.Owner, s.Source, s.DisplayName, s.Color, s.RefreshRate = owner, src.String, name.String, color.String, rate.String
		out = append(out, s)
		return nil
	})
	return out, err
}

// accessFromClassic maps the access column. Classic stores 2 for read-write
// and 3 for read-only; anything else is treated as read-only so that an
// unknown value never widens access.
func accessFromClassic(v int) Access {
	if v == 2 {
		return ReadWrite
	}
	return ReadOnly
}

// Shares returns the calendar and address book shares, ordered by row id.
func (r *Reader) Shares(ctx context.Context) ([]Share, error) {
	var out []Share
	err := r.each(ctx, `SELECT id, principaluri, type, access, resourceid, publicuri FROM {dav_shares} ORDER BY id`, nil, func(rows *sql.Rows) error {
		var s Share
		var principal, kind string
		var access int
		var public sql.NullString
		if err := rows.Scan(&s.ID, &principal, &kind, &access, &s.ResourceID, &public); err != nil {
			return err
		}
		target, err := ParsePrincipal(principal)
		if err != nil {
			return fmt.Errorf("share %d: %w", s.ID, err)
		}
		s.Kind, s.Target, s.Access, s.PublicURI = ResourceKind(kind), target, accessFromClassic(access), public.String
		out = append(out, s)
		return nil
	})
	return out, err
}

// permanentToken is the type value of an app password. Browser sessions are
// stored in the same table with type 0.
const permanentToken = 1

// AppTokens returns the app passwords, ordered by row id.
func (r *Reader) AppTokens(ctx context.Context) ([]AppToken, error) {
	var out []AppToken
	err := r.each(ctx, `SELECT uid, login_name, name, token, last_activity FROM {authtoken} WHERE type = ? ORDER BY id`,
		[]any{permanentToken}, func(rows *sql.Rows) error {
			var t AppToken
			var last sql.NullInt64
			if err := rows.Scan(&t.User, &t.LoginName, &t.Name, &t.Digest, &last); err != nil {
				return err
			}
			t.LastActivity = unixTime(last)
			out = append(out, t)
			return nil
		})
	return out, err
}
