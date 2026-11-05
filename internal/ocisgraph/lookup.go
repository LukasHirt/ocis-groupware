package ocisgraph

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type userList struct {
	Value    []meResponse `json:"value"`
	NextLink string       `json:"@odata.nextLink"`
}

// UsersByLogin returns the users whose onPremisesSamAccountName is exactly
// login. oCIS answers a $filter on that property with 501, so the users are
// searched for and the answer is filtered here, as it is for groups. The token
// must belong to someone who may list users, such as an administrator. A
// well-formed answer without a match is an empty list.
func (c *Client) UsersByLogin(ctx context.Context, token, login string) ([]User, error) {
	u := c.base.JoinPath("/graph/v1.0/users")
	u.RawQuery = query("$search", `"`+strings.ReplaceAll(login, `"`, `\"`)+`"`)
	var out []User
	for next, pages := u.String(), 0; next != ""; pages++ {
		if pages >= maxPages {
			return nil, fmt.Errorf("ocisgraph: more than %d pages of users", maxPages)
		}
		var page userList
		if err := c.get(ctx, next, token, &page); err != nil {
			return nil, err
		}
		for _, v := range page.Value {
			if v.ID != "" && v.Username == login {
				out = append(out, User{ID: v.ID, Username: v.Username, DisplayName: v.DisplayName, Mail: v.Mail})
			}
		}
		var err error
		if next, err = c.sameServer(page.NextLink); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// GroupsByName returns the groups whose display name is exactly name. The
// server's search matches parts of names, so the answer is filtered here.
func (c *Client) GroupsByName(ctx context.Context, token, name string) ([]Group, error) {
	u := c.base.JoinPath("/graph/v1.0/groups")
	u.RawQuery = query("$search", `"`+strings.ReplaceAll(name, `"`, `\"`)+`"`)
	var out []Group
	for next, pages := u.String(), 0; next != ""; pages++ {
		if pages >= maxPages {
			return nil, fmt.Errorf("ocisgraph: more than %d pages of groups", maxPages)
		}
		var page pageResponse
		if err := c.get(ctx, next, token, &page); err != nil {
			return nil, err
		}
		for _, g := range page.Value {
			if g.ID != "" && g.DisplayName == name {
				out = append(out, Group{ID: g.ID, Name: g.DisplayName})
			}
		}
		var err error
		if next, err = c.sameServer(page.NextLink); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func query(key, value string) string {
	return strings.ReplaceAll(url.Values{key: {value}}.Encode(), "+", "%20")
}

// sameServer accepts an empty link or one on the server the client was
// created for.
func (c *Client) sameServer(link string) (string, error) {
	if link == "" {
		return "", nil
	}
	u, err := url.Parse(link)
	if err != nil || u.Scheme != c.base.Scheme || u.Host != c.base.Host {
		return "", errors.New("ocisgraph: next page is not on the server")
	}
	return link, nil
}
