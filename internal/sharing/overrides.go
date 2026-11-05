package sharing

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

// OverridesFileName is the name of the file that holds what a recipient
// changed about collections shared with them, inside the recipient's
// directory.
const OverridesFileName = "shared_with_me.json"

// override is what one recipient changed about one shared collection. A nil
// or false field leaves the owner's value in place.
type override struct {
	DisplayName *string `json:"display_name,omitempty"`
	Color       *string `json:"color,omitempty"`
	Order       *int    `json:"order,omitempty"`
	Hidden      bool    `json:"hidden,omitempty"`
}

func (o override) empty() bool {
	return o.DisplayName == nil && o.Color == nil && o.Order == nil && !o.Hidden
}

// overridesFile is the content of shared_with_me.json, by collection id.
type overridesFile struct {
	Version     int                 `json:"version"`
	Collections map[string]override `json:"collections"`
}

// overrides keeps the recipients' overrides. A recipient's file is read when
// it is first needed and kept in memory from then on.
type overrides struct {
	ownerDir func(string) string
	logger   *slog.Logger

	mu     sync.Mutex
	loaded map[string]map[string]override // by recipient, then collection id
}

func newOverrides(ownerDir func(string) string, logger *slog.Logger) *overrides {
	return &overrides{ownerDir: ownerDir, logger: logger, loaded: map[string]map[string]override{}}
}

// forRecipient returns the recipient's map, reading the file on first use.
// An unreadable file counts as empty and is logged: the recipient sees the
// owner's names and colours, and the next change replaces the file.
// Callers hold o.mu.
func (o *overrides) forRecipient(sub string) map[string]override {
	if m, ok := o.loaded[sub]; ok {
		return m
	}
	m := map[string]override{}
	if dir := o.ownerDir(sub); dir != "" {
		data, err := os.ReadFile(filepath.Join(dir, OverridesFileName))
		switch {
		case errors.Is(err, os.ErrNotExist):
		case err != nil:
			o.logger.Warn("cannot read shared collection overrides", "recipient", sub, "error", err)
		default:
			var f overridesFile
			if err := json.Unmarshal(data, &f); err != nil || f.Version != fileVersion {
				o.logger.Warn("cannot understand shared collection overrides", "recipient", sub, "version", f.Version, "error", err)
			} else if f.Collections != nil {
				m = f.Collections
			}
		}
	}
	o.loaded[sub] = m
	return m
}

func (o *overrides) get(sub, id string) override {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.forRecipient(sub)[id]
}

// update applies change to the recipient's override of a collection and
// writes the file. A failed write leaves the old override in place. It
// returns the new override.
func (o *overrides) update(sub, id string, change func(*override)) (override, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	cur := o.forRecipient(sub)
	one := cur[id]
	change(&one)

	next := make(map[string]override, len(cur)+1)
	for k, v := range cur {
		next[k] = v
	}
	if one.empty() {
		delete(next, id)
	} else {
		next[id] = one
	}
	dir := o.ownerDir(sub)
	if dir == "" {
		return override{}, fmt.Errorf("sharing: no directory for %q", sub)
	}
	if len(next) == 0 {
		if err := os.Remove(filepath.Join(dir, OverridesFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return override{}, fmt.Errorf("sharing: remove %s: %w", OverridesFileName, err)
		}
	} else {
		data, err := json.MarshalIndent(overridesFile{Version: fileVersion, Collections: next}, "", "  ")
		if err != nil {
			return override{}, fmt.Errorf("sharing: encode: %w", err)
		}
		if err := writeFileAtomic(dir, OverridesFileName, append(data, '\n'), o.logger); err != nil {
			return override{}, fmt.Errorf("sharing: write %s: %w", OverridesFileName, err)
		}
	}
	o.loaded[sub] = next
	return one, nil
}
