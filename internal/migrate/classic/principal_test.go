package classic

import "testing"

func TestParsePrincipal(t *testing.T) {
	tests := []struct {
		in   string
		want Principal
		ok   bool
	}{
		{"principals/users/alice", Principal{PrincipalUser, "alice"}, true},
		{"/principals/users/alice/", Principal{PrincipalUser, "alice"}, true},
		{"principals/groups/team", Principal{PrincipalGroup, "team"}, true},
		{"principals/system/system", Principal{PrincipalSystem, "system"}, true},
		{"principals/users/a b@example.org", Principal{PrincipalUser, "a b@example.org"}, true},
		{"principals/alice", Principal{}, false},
		{"principals/users/", Principal{}, false},
		{"principals/things/x", Principal{}, false},
		{"", Principal{}, false},
	}
	for _, tt := range tests {
		got, err := ParsePrincipal(tt.in)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("ParsePrincipal(%q) = %v, %v; want %v ok=%v", tt.in, got, err, tt.want, tt.ok)
		}
	}
}

func TestOpenRejectsUnknownPrefix(t *testing.T) {
	if _, err := NewReader(nil, MySQL, WithTablePrefix("oc_; DROP")); err == nil {
		t.Fatal("a table prefix with SQL in it was accepted")
	}
}

func TestAccessFromClassic(t *testing.T) {
	for in, want := range map[int]Access{2: ReadWrite, 3: ReadOnly, 4: ReadOnly} {
		if got := accessFromClassic(in); got != want {
			t.Errorf("accessFromClassic(%d) = %v, want %v", in, got, want)
		}
	}
}
