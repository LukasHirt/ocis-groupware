// Package termsafe makes text that other people chose safe to print on an
// administrator's terminal.
package termsafe

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Clean returns s with every character that a terminal could act on written
// out as an escape such as \x1b. That covers the C0 and C1 control characters
// (which include escape, newline and tab), the line and paragraph separators,
// the bidirectional overrides and bytes that are not valid UTF-8. Everything
// else is kept as it is.
func Clean(s string) string {
	if !strings.ContainsFunc(s, unsafe) && utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case !unsafe(r):
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
		i += size
	}
	return b.String()
}

func unsafe(r rune) bool {
	return unicode.IsControl(r) || r == ' ' || r == ' ' ||
		r >= '‪' && r <= '‮' || r >= '⁦' && r <= '⁩'
}
