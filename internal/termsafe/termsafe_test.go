package termsafe

import "testing"

func TestClean(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"plain text, ümlauts and 日本語", "plain text, ümlauts and 日本語"},
		{"\x1b[2K\x1b]0;title\x07", `\x1b[2K\x1b]0;title\x07`},
		{"line\none\rtwo\tthree", `line\none\rtwo\tthree`},
		{"del\x7f c1\u009b", `del\x7f c1\x9b`},
		{"bidi \u202e reversed", `bidi \u202e reversed`},
		{"line separator \u2028 here", `line separator \u2028 here`},
		{"bad \xff byte", `bad \xff byte`},
		{"", ""},
	} {
		if got := Clean(c.in); got != c.want {
			t.Errorf("Clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
