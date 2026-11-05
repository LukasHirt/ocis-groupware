package graphapi

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LukasHirt/groupkit/vcard"
)

// maxCardLineOctets is the longest content line encodeCard writes, line break
// excluded.
const maxCardLineOctets = 75

// encodeCard writes the card as vCard text with CRLF line endings, folded at
// 75 octets without splitting a UTF-8 character. Parameters are written in
// name order. A parameter value with a comma, semicolon or colon is quoted,
// and a double quote inside one is dropped, since the syntax cannot hold it.
func encodeCard(c *vcard.Card) []byte {
	b := foldCardLine(nil, "BEGIN:VCARD")
	b = foldCardLine(b, "VERSION:"+c.Version)
	for _, p := range c.Props {
		b = foldCardLine(b, cardLine(p))
	}
	return foldCardLine(b, "END:VCARD")
}

func cardLine(p vcard.Property) string {
	var sb strings.Builder
	if p.Group != "" {
		sb.WriteString(p.Group)
		sb.WriteByte('.')
	}
	sb.WriteString(p.Name)
	names := make([]string, 0, len(p.Params))
	for n := range p.Params {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		sb.WriteByte(';')
		sb.WriteString(n)
		sb.WriteByte('=')
		for i, v := range p.Params[n] {
			if i > 0 {
				sb.WriteByte(',')
			}
			v = strings.ReplaceAll(v, `"`, "")
			if strings.ContainsAny(v, ",;:") {
				v = `"` + v + `"`
			}
			sb.WriteString(v)
		}
	}
	sb.WriteByte(':')
	sb.WriteString(p.Raw)
	return sb.String()
}

func foldCardLine(b []byte, line string) []byte {
	limit := maxCardLineOctets
	for len(line) > limit {
		n := limit
		for n > 0 && !utf8.RuneStart(line[n]) {
			n--
		}
		if n == 0 {
			n = limit
		}
		b = append(b, line[:n]...)
		b = append(b, "\r\n "...)
		line = line[n:]
		limit = maxCardLineOctets - 1
	}
	b = append(b, line...)
	return append(b, "\r\n"...)
}
