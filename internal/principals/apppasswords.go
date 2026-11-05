package principals

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

// appPasswordsFileName is the file, in the owner's directory, that holds a
// principal's app passwords.
const appPasswordsFileName = "app_passwords.json"

const (
	// defaultBcryptCost is the bcrypt cost of new app passwords.
	defaultBcryptCost = 10
	// defaultAppPasswordLifetime applies when IssueAppPassword gets no
	// lifetime.
	defaultAppPasswordLifetime = 365 * 24 * time.Hour
	// lastUsedInterval is the least time between two writes of an app
	// password's last use.
	lastUsedInterval = 10 * time.Minute
	// maxAppPasswordName is the longest name of an app password, in bytes.
	maxAppPasswordName = 100
	// secretBytes is the number of random bytes in an app password.
	secretBytes = 20
	// maxPasswordLen is the longest password VerifyBasic looks at. bcrypt
	// does not take more.
	maxPasswordLen = 72
	// MaxAppPasswords is the most app passwords a principal can have,
	// expired ones included.
	MaxAppPasswords = 50
)

var (
	// ErrInvalidCredentials means the username or the password is wrong.
	// The two cases are not told apart.
	ErrInvalidCredentials = errors.New("principals: invalid credentials")
	// ErrExpired means the password was right but its lifetime is over.
	ErrExpired = errors.New("principals: app password expired")
	// ErrIdentityStale means the password was right, but oCIS has not
	// confirmed the user within the identity lease, see WithIdentityMaxAge.
	// The user renews the lease by signing in with a token.
	ErrIdentityStale = errors.New("principals: oCIS has not confirmed the user recently")
	// ErrInvalidAppPasswordName means the name given to IssueAppPassword is
	// empty, too long or contains characters that are not allowed.
	ErrInvalidAppPasswordName = errors.New("principals: invalid app password name")
	// ErrTooManyAppPasswords means the principal has MaxAppPasswords app
	// passwords already and must revoke one before issuing another.
	ErrTooManyAppPasswords = fmt.Errorf("principals: a principal can have at most %d app passwords", MaxAppPasswords)
)

// secretEncoding writes app passwords and ids: lowercase base32 without
// padding, which is easy to type on a phone.
var secretEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// AppPassword describes an app password. The secret itself is shown once,
// by IssueAppPassword, and kept only as a bcrypt hash.
type AppPassword struct {
	ID, Name   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastUsedAt *time.Time
}

// appRecord is one app password in app_passwords.json.
type appRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Hash is the bcrypt hash of the secret.
	Hash string `json:"hash"`
	// Hint is the first two bytes of the secret's SHA-256, in hex. A check
	// compares the bcrypt hash only of the records whose hint matches, so it
	// costs one bcrypt compare however many app passwords the principal has,
	// the same as for an unknown user. Sixteen bits of a hash of a 160 bit
	// random secret give nothing useful away.
	//
	// The hint is part of the file format: a record without a hint, or with
	// a wrong one, is never compared, so its password can never be verified.
	Hint string `json:"hint"`
	// LegacySHA512 is set instead of Hash and Hint for an app token imported
	// from a classic installation: the hex of sha512(token . secret). The
	// first successful sign-in replaces it with a bcrypt hash and a hint.
	LegacySHA512 string     `json:"legacy_sha512,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	LastUsedAt   *time.Time `json:"last_used_at"`
}

func (rec appRecord) appPassword() AppPassword {
	ap := AppPassword{ID: rec.ID, Name: rec.Name, CreatedAt: rec.CreatedAt, ExpiresAt: rec.ExpiresAt}
	if rec.LastUsedAt != nil {
		t := *rec.LastUsedAt
		ap.LastUsedAt = &t
	}
	return ap
}

// appPasswordsFile is the content of app_passwords.json.
type appPasswordsFile struct {
	AppPasswords []appRecord `json:"app_passwords"`
}

// appSet holds one principal's app passwords in memory. mu serializes the
// changes to the principal's file. records is replaced on every change and
// never modified in place, so a copy of the slice taken under mu stays valid.
type appSet struct {
	mu      sync.Mutex
	loaded  bool
	records []appRecord
}

// WithClock sets the clock the registry reads the time from.
func WithClock(now func() time.Time) Option {
	return func(r *Registry) {
		if now != nil {
			r.now = now
		}
	}
}

// WithBcryptCost sets the bcrypt cost of new app passwords and of the
// comparison made for unknown users. The default is 10. NewRegistry rejects
// a cost outside bcrypt.MinCost to bcrypt.MaxCost.
//
// Stored hashes keep the cost they were made with; only the comparison for
// unknown users follows a changed cost. Until the old app passwords are
// replaced, a wrong guess that happens to share a stored password's hint
// can take a different time than a guess for an unknown user.
func WithBcryptCost(cost int) Option {
	return func(r *Registry) {
		r.bcryptCost = cost
	}
}

// WithIdentityMaxAge sets the identity lease: a Basic sign-in succeeds only
// if the principal's identity snapshot is younger than maxAge, otherwise
// VerifyBasic returns ErrIdentityStale. The service learns about a disabled
// or deleted oCIS user or a changed group membership only through the user's
// own sign-ins, so the lease bounds how long a password outlives the user's
// standing in oCIS. A principal without a snapshot has no lease. A maxAge of
// 0, the default, turns the check off.
func WithIdentityMaxAge(maxAge time.Duration) Option {
	return func(r *Registry) { r.identityMaxAge = maxAge }
}

// IdentityMaxAge returns the identity lease, 0 if it is off.
func (r *Registry) IdentityMaxAge() time.Duration { return r.identityMaxAge }

// leaseEnd returns when the principal's identity lease ends. ok is false if
// the lease is off. A principal without a snapshot has a lease that ended
// long ago.
func (r *Registry) leaseEnd(owner string) (end time.Time, ok bool) {
	if r.identityMaxAge <= 0 {
		return time.Time{}, false
	}
	r.mu.RLock()
	at := r.principals[owner].IdentityAt
	r.mu.RUnlock()
	if at.IsZero() {
		return time.Time{}, true
	}
	return at.Add(r.identityMaxAge), true
}

// leaseValid reports whether the principal's lease covers now.
func (r *Registry) leaseValid(owner string, now time.Time) bool {
	end, ok := r.leaseEnd(owner)
	return !ok || now.Before(end)
}

// IssueAppPassword creates an app password for the principal and returns it
// together with its secret. The secret is not stored and cannot be shown
// again. A ttl of 0 means 365 days. It returns ErrTooManyAppPasswords when
// the principal has MaxAppPasswords already.
func (r *Registry) IssueAppPassword(ctx context.Context, owner, name string, ttl time.Duration) (AppPassword, string, error) {
	if err := ctx.Err(); err != nil {
		return AppPassword{}, "", err
	}
	if err := r.enabled(owner); err != nil {
		return AppPassword{}, "", fmt.Errorf("principals: issue app password: %w", err)
	}
	if err := validateAppPasswordName(name); err != nil {
		return AppPassword{}, "", err
	}
	switch {
	case ttl < 0:
		return AppPassword{}, "", errors.New("principals: app password lifetime is negative")
	case ttl == 0:
		ttl = defaultAppPasswordLifetime
	}

	secret, err := newSecret()
	if err != nil {
		return AppPassword{}, "", err
	}
	hash, err := r.hash([]byte(secret), r.bcryptCost)
	if err != nil {
		return AppPassword{}, "", fmt.Errorf("principals: hash app password: %w", err)
	}
	id, err := newID()
	if err != nil {
		return AppPassword{}, "", err
	}
	now := r.now().UTC()
	rec := appRecord{
		ID:        id,
		Name:      name,
		Hash:      string(hash),
		Hint:      secretHint(secret),
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}

	set := r.appSet(owner)
	set.mu.Lock()
	defer set.mu.Unlock()
	if err := r.loadAppPasswords(owner, set); err != nil {
		return AppPassword{}, "", err
	}
	if len(set.records) >= MaxAppPasswords {
		return AppPassword{}, "", ErrTooManyAppPasswords
	}
	records := append(slices.Clone(set.records), rec)
	if err := r.writeAppPasswords(owner, records); err != nil {
		return AppPassword{}, "", err
	}
	set.records = records
	return rec.appPassword(), secret, nil
}

// ListAppPasswords returns the principal's app passwords, oldest first,
// expired ones included. It returns ErrNotFound for an unknown principal
// and ErrDisabled for a disabled one.
func (r *Registry) ListAppPasswords(owner string) ([]AppPassword, error) {
	if err := r.enabled(owner); err != nil {
		return nil, fmt.Errorf("principals: list app passwords: %w", err)
	}
	set := r.appSet(owner)
	set.mu.Lock()
	defer set.mu.Unlock()
	if err := r.loadAppPasswords(owner, set); err != nil {
		return nil, err
	}
	list := make([]AppPassword, 0, len(set.records))
	for _, rec := range set.records {
		list = append(list, rec.appPassword())
	}
	return list, nil
}

// RevokeAppPassword deletes an app password of the principal. Credentials
// of the principal that were verified recently are forgotten, so the next
// request with the revoked password fails. It returns ErrNotFound if the
// principal has no app password with that id and ErrDisabled for a disabled
// principal.
func (r *Registry) RevokeAppPassword(ctx context.Context, owner, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.enabled(owner); err != nil {
		return fmt.Errorf("principals: revoke app password: %w", err)
	}
	set := r.appSet(owner)
	set.mu.Lock()
	err := r.removeAppPassword(owner, id, set)
	set.mu.Unlock()
	if err != nil {
		return err
	}
	r.cache.invalidate(owner)
	return nil
}

// enabled returns nil if owner is a principal that is not disabled, else
// ErrNotFound or ErrDisabled.
func (r *Registry) enabled(owner string) error {
	p, err := r.Get(owner)
	if err != nil {
		return err
	}
	if p.Disabled {
		return ErrDisabled
	}
	return nil
}

// removeAppPassword deletes an app password. The caller holds set.mu.
func (r *Registry) removeAppPassword(owner, id string, set *appSet) error {
	if err := r.loadAppPasswords(owner, set); err != nil {
		return err
	}
	i := slices.IndexFunc(set.records, func(rec appRecord) bool { return rec.ID == id })
	if i < 0 {
		return fmt.Errorf("principals: app password: %w", ErrNotFound)
	}
	records := slices.Delete(slices.Clone(set.records), i, i+1)
	if err := r.writeAppPasswords(owner, records); err != nil {
		return err
	}
	set.records = records
	return nil
}

// VerifyBasic checks the username and app password of an HTTP Basic
// Authorization header and returns the principal.
//
// It returns ErrInvalidCredentials for an unknown username and for a wrong
// password alike, and both cost one bcrypt compare. ErrExpired means the
// password was right but has expired, ErrDisabled that it was right but the
// principal is disabled, and ErrIdentityStale that it was right but oCIS has
// not confirmed the user within the identity lease. All three are found out
// after the compare, in that order, so they cost the same as a wrong
// password. Other errors mean the app passwords could
// not be read. No error contains the password.
//
// A credential that was verified in the last five minutes is accepted
// without a bcrypt compare, unless its app password expired or was revoked
// since, the username moved to another principal or the principal was
// disabled, or its identity lease ran out.
func (r *Registry) VerifyBasic(ctx context.Context, username, password string) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	now := r.now()
	key := r.cache.key(basicHeader(username, password))
	if e, ok := r.cache.get(key, now); ok {
		if p, ok := r.principalByName(e.sub, username); ok && !p.Disabled && r.leaseValid(e.sub, now) && r.touch(e.sub, e.id, now) {
			return p, nil
		}
	}

	owner, ok := r.LookupUsername(username)
	if !ok || password == "" || len(password) > maxPasswordLen {
		r.dummyCompare()
		return Principal{}, ErrInvalidCredentials
	}
	// Read the generation before the app passwords, so that a revoke that
	// happens after the read keeps this verification out of the cache.
	gen := r.cache.generation(owner)
	records, err := r.appPasswordRecords(owner)
	if err != nil {
		r.dummyCompare()
		return Principal{}, err
	}

	hint := secretHint(password)
	var (
		match    *appRecord
		compared bool
	)
	for i := range records {
		if records[i].Hint != hint {
			continue
		}
		compared = true
		if r.compare([]byte(records[i].Hash), []byte(password)) == nil {
			match = &records[i]
			break
		}
	}
	if match == nil && r.legacyEnabled() && slices.ContainsFunc(records, func(rec appRecord) bool { return rec.LegacySHA512 != "" }) {
		if r.legacyGate != nil {
			if err := r.legacyGate(ctx, username); err != nil {
				return Principal{}, err
			}
		}
		id := r.matchLegacy(records, password)
		if id == "" {
			if !compared {
				r.dummyCompare()
			}
			return Principal{}, ErrInvalidCredentials
		}
		// The password is right. Expiry and a disabled principal are
		// checked before the token is used up.
		for i := range records {
			if records[i].ID == id {
				match = &records[i]
			}
		}
		if !now.Before(match.ExpiresAt) {
			return Principal{}, ErrExpired
		}
		if p, err := r.Get(owner); err != nil || p.Username != username {
			return Principal{}, ErrInvalidCredentials
		} else if p.Disabled {
			return Principal{}, ErrDisabled
		}
		// The lease is checked before the token is used up, so that a
		// refused token still works once the user is back.
		if !r.leaseValid(owner, now) {
			return Principal{}, ErrIdentityStale
		}
		swapped, err := r.swapLegacy(owner, id, password, now)
		if err != nil {
			return Principal{}, err
		}
		match = &swapped
	} else if !compared {
		r.dummyCompare()
	}
	if match == nil {
		return Principal{}, ErrInvalidCredentials
	}
	if !now.Before(match.ExpiresAt) {
		return Principal{}, ErrExpired
	}
	// The username may have moved, or the password been revoked, while the
	// hash was compared.
	p, ok := r.principalByName(owner, username)
	if !ok {
		return Principal{}, ErrInvalidCredentials
	}
	if p.Disabled {
		return Principal{}, ErrDisabled
	}
	expires := match.ExpiresAt
	if end, ok := r.leaseEnd(owner); ok {
		if !now.Before(end) {
			return Principal{}, ErrIdentityStale
		}
		if end.Before(expires) {
			expires = end
		}
	}
	if !r.touch(owner, match.ID, now) {
		return Principal{}, ErrInvalidCredentials
	}
	if r.beforeCachePut != nil {
		r.beforeCachePut()
	}
	r.cache.put(key, cacheEntry{sub: owner, id: match.ID, gen: gen, expires: expires}, now)
	return p, nil
}

// principalByName returns the principal for owner if it still holds username.
func (r *Registry) principalByName(owner, username string) (Principal, bool) {
	p, err := r.Get(owner)
	if err != nil || p.Username != username {
		return Principal{}, false
	}
	return p, true
}

// touch records a use of the app password and reports whether it still
// exists. last_used_at is written at most every lastUsedInterval. A failed
// write is logged: the time is kept in memory and saved with the next write.
func (r *Registry) touch(owner, id string, now time.Time) bool {
	set := r.appSet(owner)
	set.mu.Lock()
	defer set.mu.Unlock()
	if !set.loaded {
		return false
	}
	i := slices.IndexFunc(set.records, func(rec appRecord) bool { return rec.ID == id })
	if i < 0 {
		return false
	}
	if last := set.records[i].LastUsedAt; last != nil && now.Sub(*last) < lastUsedInterval {
		return true
	}
	used := now.UTC()
	records := slices.Clone(set.records)
	records[i].LastUsedAt = &used
	set.records = records
	if err := r.writeAppPasswords(owner, records); err != nil {
		r.logger.Warn("could not save the last use of an app password", "owner", owner, "id", id, "error", err)
	}
	return true
}

// appPasswordRecords returns the principal's app passwords. The slice must
// not be modified.
func (r *Registry) appPasswordRecords(owner string) ([]appRecord, error) {
	set := r.appSet(owner)
	set.mu.Lock()
	defer set.mu.Unlock()
	if err := r.loadAppPasswords(owner, set); err != nil {
		return nil, err
	}
	return set.records, nil
}

// appSet returns the in-memory app passwords of owner, creating an empty,
// unloaded set the first time.
func (r *Registry) appSet(owner string) *appSet {
	r.appsMu.Lock()
	defer r.appsMu.Unlock()
	set, ok := r.apps[owner]
	if !ok {
		set = &appSet{}
		r.apps[owner] = set
	}
	return set
}

// loadAppPasswords reads the principal's file into set unless that was done
// already. A missing file means no app passwords. A file that cannot be read
// or decoded is an error, and set stays unloaded so that nothing overwrites
// the file. The caller holds set.mu.
func (r *Registry) loadAppPasswords(owner string, set *appSet) error {
	if set.loaded {
		return nil
	}
	dir := r.ownerDir(owner)
	if dir == "" {
		return fmt.Errorf("principals: no owner directory for owner %q", owner)
	}
	data, err := os.ReadFile(filepath.Join(dir, appPasswordsFileName))
	switch {
	case errors.Is(err, os.ErrNotExist):
		set.records, set.loaded = nil, true
		return nil
	case err != nil:
		return fmt.Errorf("principals: read app passwords: %w", err)
	}
	var file appPasswordsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("principals: decode app passwords of owner %q: %w", owner, err)
	}
	set.records, set.loaded = file.AppPasswords, true
	return nil
}

// writeAppPasswords replaces the principal's file atomically.
func (r *Registry) writeAppPasswords(owner string, records []appRecord) error {
	dir := r.ownerDir(owner)
	if dir == "" {
		return fmt.Errorf("principals: no owner directory for owner %q", owner)
	}
	if records == nil {
		records = []appRecord{}
	}
	data, err := json.MarshalIndent(appPasswordsFile{AppPasswords: records}, "", "  ")
	if err != nil {
		return fmt.Errorf("principals: encode app passwords: %w", err)
	}
	data = append(data, '\n')
	if err := r.mkdirAll(dir); err != nil {
		return fmt.Errorf("principals: create owner directory: %w", err)
	}
	if err := r.writeFileAtomic(dir, appPasswordsFileName, data); err != nil {
		return fmt.Errorf("principals: write app passwords: %w", err)
	}
	return nil
}

// dummyCompare spends the time of one bcrypt compare, for a check that has
// no hash to compare against.
func (r *Registry) dummyCompare() {
	_ = r.compare(r.dummyHash, []byte("not an app password"))
}

// newDummyHash returns the bcrypt hash of a random secret at cost. NewRegistry
// makes it once, so that no check pays for making it.
func newDummyHash(cost int) ([]byte, error) {
	secret, err := newSecret()
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), cost)
	if err != nil {
		return nil, fmt.Errorf("principals: hash for unknown users: %w", err)
	}
	return hash, nil
}

func validateAppPasswordName(name string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return fmt.Errorf("%w: it is empty", ErrInvalidAppPasswordName)
	case len(name) > maxAppPasswordName:
		return fmt.Errorf("%w: it is longer than %d bytes", ErrInvalidAppPasswordName, maxAppPasswordName)
	case !utf8.ValidString(name):
		return fmt.Errorf("%w: it is not valid UTF-8", ErrInvalidAppPasswordName)
	case strings.ContainsFunc(name, func(c rune) bool { return unicode.IsControl(c) || unicode.Is(unicode.Cf, c) }):
		return fmt.Errorf("%w: it contains a control or format character", ErrInvalidAppPasswordName)
	}
	return nil
}

// newSecret returns a new app password: 20 random bytes in lowercase base32.
func newSecret() (string, error) {
	b := make([]byte, secretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("principals: random secret: %w", err)
	}
	return secretEncoding.EncodeToString(b), nil
}

// newID returns a new app password id.
func newID() (string, error) {
	b := make([]byte, 10)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("principals: random id: %w", err)
	}
	return secretEncoding.EncodeToString(b), nil
}

func secretHint(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:2])
}

// basicHeader returns the Authorization header that carries the credentials
// (RFC 7617). It is the input of the cache key and is not kept.
func basicHeader(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}
