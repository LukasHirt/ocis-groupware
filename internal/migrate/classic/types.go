// Package classic reads the data of a classic ownCloud 10 installation
// straight from its database: users, groups, calendars with their events,
// address books with their cards, calendar subscriptions, shares and app
// tokens. It reads and never writes. MySQL, MariaDB and PostgreSQL are
// supported behind the same Reader.
//
// The package returns what the database holds and decides nothing about it.
// What to skip, how to map users and where data goes is up to the caller.
package classic

import "time"

// User is a row of the accounts table.
type User struct {
	// ID is the classic user id, the login name.
	ID          string
	DisplayName string
	Email       string
	// Enabled is false for disabled and deleted accounts.
	Enabled bool
	// Backend is the user backend class, such as OC\User\Database.
	Backend string
}

// Group is a classic group with the ids of its members.
type Group struct {
	ID      string
	Members []string
}

// Calendar is one calendar. The system principal owns the birthday calendar
// that classic generates from contact birthdays.
type Calendar struct {
	// ID is the row id, used to read the calendar's objects and to match
	// shares.
	ID    int64
	Owner Principal
	URI   string
	// DisplayName, Description, Color and Timezone are empty when the
	// database holds NULL.
	DisplayName string
	Description string
	Color       string
	Timezone    string
	Order       int64
	// Components lists the supported component types, such as VEVENT.
	Components  []string
	Transparent bool
	SyncToken   int64
}

// Object is a calendar object, one iCalendar resource.
type Object struct {
	URI          string
	Data         []byte
	ETag         string
	LastModified time.Time
	// Type is the component type the object is indexed under, such as VEVENT.
	Type string
	UID  string
	// Classification is 0 public, 1 private and 2 confidential.
	Classification int
	// Size is the size column; it equals len(Data) in a healthy database.
	Size int64
}

// AddressBook is one address book.
type AddressBook struct {
	ID          int64
	Owner       Principal
	URI         string
	DisplayName string
	Description string
	SyncToken   int64
}

// Card is one vCard resource.
type Card struct {
	URI          string
	Data         []byte
	ETag         string
	LastModified time.Time
	Size         int64
}

// Subscription is a calendar subscription to an external iCalendar feed.
type Subscription struct {
	Owner       Principal
	URI         string
	Source      string
	DisplayName string
	Color       string
	Order       int64
	RefreshRate string
}

// ResourceKind says what a share points at.
type ResourceKind string

const (
	ResourceCalendar    ResourceKind = "calendar"
	ResourceAddressBook ResourceKind = "addressbook"
)

// Access is the level of a share.
type Access string

const (
	ReadOnly  Access = "read-only"
	ReadWrite Access = "read-write"
)

// Share is a row of the shares table. A share with a PublicURI is a public
// link and has no recipient; its Target is the owner.
type Share struct {
	ID         int64
	Kind       ResourceKind
	ResourceID int64
	Target     Principal
	Access     Access
	PublicURI  string
}

// AppToken is an app password. The database keeps only the digest, which is
// the hex SHA-512 of the token followed by the instance secret.
type AppToken struct {
	User      string
	LoginName string
	Name      string
	Digest    string
	// LastActivity is zero when the token was never used.
	LastActivity time.Time
}
