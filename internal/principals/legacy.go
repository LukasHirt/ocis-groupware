package principals

import (
	"context"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// defaultLegacyTokenLifetime is how long an imported app token stays valid
// without being used, unless WithLegacyTokens says otherwise.
const defaultLegacyTokenLifetime = 180 * 24 * time.Hour

var (
	// ErrInvalidLegacyToken means the digest given to ImportLegacyToken is
	// not the hex of a SHA-512 digest.
	ErrInvalidLegacyToken = errors.New("principals: invalid legacy token digest")
	// ErrRateLimited means a legacy token check was refused by the limiter
	// set with WithLegacyGate. The token was not looked at.
	ErrRateLimited = errors.New("principals: too many attempts")
)

// WithLegacyTokens turns on the check of imported classic app tokens. A
// classic installation stores a token as the hex of sha512(token . secret),
// where secret is the instance secret; the same secret is needed to check
// one. ttl is how long an imported token is valid counted from its last use
// (or from the import if it was never used); 0 means 180 days. Without a
// secret, imported tokens are kept but never verify.
func WithLegacyTokens(secret string, ttl time.Duration) Option {
	return func(r *Registry) {
		r.legacySecret = secret
		if ttl > 0 {
			r.legacyTTL = ttl
		}
	}
}

// WithLegacyGate sets a check that VerifyBasic runs before it compares a
// password with an imported token's digest, which is the only kind of check
// that costs a guesser no bcrypt compare. If it returns an error, VerifyBasic
// returns that error and does not look at the password. Logins found in the
// credential cache, and checks that reach no imported token, do not call it.
func WithLegacyGate(gate func(ctx context.Context, username string) error) Option {
	return func(r *Registry) { r.legacyGate = gate }
}

func (r *Registry) legacyEnabled() bool { return r.legacySecret != "" }

// ImportLegacyToken stores a classic app token for the principal, given the
// hex of its sha512(token . secret) digest. The token is shown in the
// principal's app password list under name and signs in with HTTP Basic like
// any app password. Its first successful use replaces the digest with a
// bcrypt hash. It expires the configured lifetime after lastUsed, or after
// now if lastUsed is zero. A token counts toward MaxAppPasswords.
//
// Importing a digest the principal already has returns the stored record and
// changes nothing. A digest whose token was used already is not known any
// more and is imported again.
func (r *Registry) ImportLegacyToken(ctx context.Context, owner, name, digest string, lastUsed time.Time) (AppPassword, error) {
	if err := ctx.Err(); err != nil {
		return AppPassword{}, err
	}
	digest = strings.ToLower(digest)
	if raw, err := hex.DecodeString(digest); err != nil || len(raw) != sha512.Size {
		return AppPassword{}, ErrInvalidLegacyToken
	}
	if err := validateAppPasswordName(name); err != nil {
		return AppPassword{}, err
	}
	if err := r.enabled(owner); err != nil {
		return AppPassword{}, fmt.Errorf("principals: import legacy token: %w", err)
	}
	id, err := newID()
	if err != nil {
		return AppPassword{}, err
	}
	now := r.now().UTC()
	rec := appRecord{ID: id, Name: name, LegacySHA512: digest, CreatedAt: now, ExpiresAt: now.Add(r.legacyTTL)}
	if !lastUsed.IsZero() {
		used := lastUsed.UTC()
		rec.LastUsedAt = &used
		rec.ExpiresAt = used.Add(r.legacyTTL)
	}

	set := r.appSet(owner)
	set.mu.Lock()
	defer set.mu.Unlock()
	if err := r.loadAppPasswords(owner, set); err != nil {
		return AppPassword{}, err
	}
	if i := slices.IndexFunc(set.records, func(old appRecord) bool { return old.LegacySHA512 == digest }); i >= 0 {
		return set.records[i].appPassword(), nil
	}
	if len(set.records) >= MaxAppPasswords {
		return AppPassword{}, ErrTooManyAppPasswords
	}
	records := append(slices.Clone(set.records), rec)
	if err := r.writeAppPasswords(owner, records); err != nil {
		return AppPassword{}, err
	}
	set.records = records
	return rec.appPassword(), nil
}

// matchLegacy returns the id of the imported token whose digest matches
// password, or "". It compares every imported token, each in constant time.
func (r *Registry) matchLegacy(records []appRecord, password string) string {
	sum := sha512.Sum512([]byte(password + r.legacySecret))
	var id string
	for _, rec := range records {
		if rec.LegacySHA512 == "" {
			continue
		}
		want, err := hex.DecodeString(rec.LegacySHA512)
		if err != nil || len(want) != sha512.Size {
			continue
		}
		if subtle.ConstantTimeCompare(sum[:], want) == 1 {
			id = rec.ID
		}
	}
	return id
}

// swapLegacy replaces the digest of the imported token id with a bcrypt hash
// of password, gives it a normal lifetime from now and returns the record.
// When another sign-in swapped the token first, it returns that record and
// writes nothing, so a token is hashed once however many requests present it
// at the same time. It leaves the credential cache alone: no entry can name
// a token that was never verified, and VerifyBasic caches the swapped one.
func (r *Registry) swapLegacy(owner, id, password string, now time.Time) (appRecord, error) {
	set := r.appSet(owner)
	set.mu.Lock()
	defer set.mu.Unlock()
	if err := r.loadAppPasswords(owner, set); err != nil {
		return appRecord{}, err
	}
	i := slices.IndexFunc(set.records, func(rec appRecord) bool { return rec.ID == id })
	if i < 0 {
		return appRecord{}, ErrInvalidCredentials
	}
	if set.records[i].LegacySHA512 == "" {
		return set.records[i], nil
	}
	hash, err := r.hash([]byte(password), r.bcryptCost)
	if err != nil {
		return appRecord{}, fmt.Errorf("principals: hash legacy token: %w", err)
	}
	used := now.UTC()
	records := slices.Clone(set.records)
	records[i].LegacySHA512 = ""
	records[i].Hash = string(hash)
	records[i].Hint = secretHint(password)
	records[i].ExpiresAt = used.Add(r.appLifetime)
	records[i].LastUsedAt = &used
	if err := r.writeAppPasswords(owner, records); err != nil {
		return appRecord{}, err
	}
	set.records = records
	return records[i], nil
}
