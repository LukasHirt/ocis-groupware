package classic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureInfo is the part of the description next to a dump that the reader
// tests compare against.
type fixtureInfo struct {
	Secret    string `json:"secret"`
	AppTokens []struct {
		User   string `json:"user"`
		Name   string `json:"name"`
		Token  string `json:"token"`
		Digest string `json:"sha512"`
	} `json:"app_tokens"`
	Rows      map[string]int `json:"rows"`
	Calendars map[string]int `json:"calendars"`
}

func readFixture(t *testing.T, name string) fixtureInfo {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "classic", "dumps", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var f fixtureInfo
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f
}
