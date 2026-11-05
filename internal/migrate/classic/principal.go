package classic

import (
	"fmt"
	"strings"
)

// PrincipalKind is the kind of a principal URI.
type PrincipalKind string

const (
	PrincipalUser   PrincipalKind = "user"
	PrincipalGroup  PrincipalKind = "group"
	PrincipalSystem PrincipalKind = "system"
)

// Principal is a classic principal such as principals/users/alice.
type Principal struct {
	Kind PrincipalKind
	Name string
}

func (p Principal) String() string { return string(p.Kind) + ":" + p.Name }

var principalSegments = map[string]PrincipalKind{
	"users":  PrincipalUser,
	"groups": PrincipalGroup,
	"system": PrincipalSystem,
}

// ParsePrincipal parses a principal URI of the form principals/<users|groups|system>/<name>.
func ParsePrincipal(uri string) (Principal, error) {
	rest, ok := strings.CutPrefix(strings.Trim(uri, "/"), "principals/")
	if !ok {
		return Principal{}, fmt.Errorf("classic: %q is not a principal URI", uri)
	}
	seg, name, ok := strings.Cut(rest, "/")
	kind, known := principalSegments[seg]
	if !ok || !known || name == "" {
		return Principal{}, fmt.Errorf("classic: %q is not a principal URI", uri)
	}
	return Principal{Kind: kind, Name: name}, nil
}
