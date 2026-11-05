package migrate

import (
	"strings"
	"testing"
)

func TestWriteTextEscapesControlCharacters(t *testing.T) {
	const evil = "\x1b[2K\x1b]0;owned\x07 and\nUser fake: created"
	r := &Report{
		Users: []*UserReport{{
			Login:  "ann" + evil,
			Status: StatusCreated,
			Reason: evil,
			Calendars: []CollectionReport{{
				Name: "cal" + evil, Status: StatusCreated, Reason: evil, Problems: []string{evil},
			}},
			Tokens: TokenReport{Imported: 1, Problems: []string{evil}},
		}},
		Shares:      []ShareReport{{Kind: "calendar", Owner: "o", Collection: "c", Grantee: evil, Access: "read", Status: StatusCreated, Reason: evil}},
		NotMigrated: []NotMigrated{{What: "subscription", Owner: "o", Name: evil, Detail: evil}},
		Unmapped:    []UnmappedID{{Kind: "user", ID: "id" + evil, Reason: evil}},
		Warnings:    []string{evil},
	}
	var out strings.Builder
	if err := r.WriteText(&out); err != nil {
		t.Fatal(err)
	}
	for _, c := range out.String() {
		if c < ' ' && c != '\n' || c == 0x7f {
			t.Fatalf("control character %q in the output:\n%s", c, out.String())
		}
	}
	if strings.Contains(out.String(), "\nUser fake") {
		t.Errorf("a value started a line of its own:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `\x1b[2K`) {
		t.Errorf("the escape was not written out:\n%s", out.String())
	}
}
