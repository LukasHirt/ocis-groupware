package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// adminGuide is the page that documents every setting.
const adminGuide = "../../docs/administration.md"

var envKey = regexp.MustCompile(`GROUPWARE_[A-Z0-9_]+`)

// codeKeys returns every GROUPWARE_* name that a non-test Go file of the
// service mentions.
func codeKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := map[string]string{}
	for _, root := range []string{"../../cmd", "../../internal"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, key := range envKey.FindAllString(string(src), -1) {
				if _, ok := keys[key]; !ok {
					keys[key] = path
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(keys) < 10 {
		t.Fatalf("found only %d GROUPWARE_ names in the code, the scan is broken", len(keys))
	}
	return keys
}

func readGuide(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(adminGuide)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEveryEnvironmentVariableIsDocumented(t *testing.T) {
	guide := readGuide(t)
	for key, file := range codeKeys(t) {
		if !strings.Contains(guide, "`"+key+"`") {
			t.Errorf("%s (used in %s) is missing from docs/administration.md", key, file)
		}
	}
}

func TestEveryYAMLKeyIsDocumented(t *testing.T) {
	guide := readGuide(t)
	typ := reflect.TypeFor[Config]()
	for i := range typ.NumField() {
		tag, _, _ := strings.Cut(typ.Field(i).Tag.Get("yaml"), ",")
		if tag == "" || tag == "-" {
			continue
		}
		if !strings.Contains(guide, "`"+tag+"`") {
			t.Errorf("YAML key %s is missing from docs/administration.md", tag)
		}
	}
}

// A documented variable that the code does not read is a typo or a setting
// that was removed.
func TestDocumentedEnvironmentVariablesExist(t *testing.T) {
	known := codeKeys(t)
	for _, key := range regexp.MustCompile("`(GROUPWARE_[A-Z0-9_]+)`").FindAllStringSubmatch(readGuide(t), -1) {
		if _, ok := known[key[1]]; !ok {
			t.Errorf("docs/administration.md documents %s, which no code reads", key[1])
		}
	}
}
