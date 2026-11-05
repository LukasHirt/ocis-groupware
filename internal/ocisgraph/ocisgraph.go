// Package ocisgraph is a small client for the oCIS Graph API. It asks one
// question: who is the caller, and which groups is the caller in. It always
// acts with the caller's own bearer token, so the service needs no credential
// of its own for oCIS.
package ocisgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/owncloud/ocis-groupware/internal/auth"
)

const (
	// DefaultTimeout bounds one request to the Graph API.
	DefaultTimeout = 5 * time.Second
	// maxBody is the most the client reads from one response.
	maxBody = 4 << 20
	// maxPages is the most pages of group memberships the client follows.
	maxPages = 20
)

// ErrUnauthorized means the Graph API refused the token (401 or 403).
var ErrUnauthorized = errors.New("ocisgraph: token refused")

// Config configures a Client.
type Config struct {
	// BaseURL is the public URL of oCIS, without the /graph path.
	BaseURL string
	// CAFile is an optional PEM bundle of extra CAs to trust.
	CAFile string
	// Timeout bounds each request. Zero means DefaultTimeout.
	Timeout time.Duration
	// HTTPClient, when set, replaces the client built from CAFile and
	// Timeout. Its transport and timeout are used as they are.
	HTTPClient *http.Client
}

// Group is a group the user is a member of.
type Group struct {
	ID   string
	Name string
}

// User is the caller as the Graph API describes them.
type User struct {
	// ID is the oCIS user id.
	ID          string
	Username    string
	DisplayName string
	Mail        string
	Groups      []Group
}

// Client calls the Graph API. It is safe for concurrent use.
type Client struct {
	base   *url.URL
	client *http.Client
}

// New returns a client for the Graph API below cfg.BaseURL. It does not
// contact the server.
func New(cfg Config) (*Client, error) {
	base, err := url.Parse(strings.TrimSuffix(cfg.BaseURL, "/"))
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" {
		return nil, fmt.Errorf("ocisgraph: %q is not an http or https URL", cfg.BaseURL)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	var hc *http.Client
	if cfg.HTTPClient != nil {
		copied := *cfg.HTTPClient
		hc = &copied
	} else if hc, err = auth.NewHTTPClient(cfg.CAFile, timeout); err != nil {
		return nil, err
	}
	// Never follow a redirect: it could carry the token somewhere else.
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base, client: hc}, nil
}

type graphGroup struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type meResponse struct {
	ID          string       `json:"id"`
	DisplayName string       `json:"displayName"`
	Mail        string       `json:"mail"`
	Username    string       `json:"onPremisesSamAccountName"`
	MemberOf    []graphGroup `json:"memberOf"`
	NextLink    string       `json:"memberOf@odata.nextLink"`
}

type pageResponse struct {
	Value    []graphGroup `json:"value"`
	NextLink string       `json:"@odata.nextLink"`
}

// Me returns the user the token belongs to, with every group of the user.
// If the server pages the group list, Me follows the links, as long as they
// stay on the server it started with. It returns ErrUnauthorized when the
// server refuses the token.
func (c *Client) Me(ctx context.Context, token string) (User, error) {
	u := c.base.JoinPath("/graph/v1.0/me")
	u.RawQuery = "$expand=memberOf"
	var me meResponse
	if err := c.get(ctx, u.String(), token, &me); err != nil {
		return User{}, err
	}
	if me.ID == "" {
		return User{}, errors.New("ocisgraph: answer has no user id")
	}
	user := User{ID: me.ID, Username: me.Username, DisplayName: me.DisplayName, Mail: me.Mail}
	user.Groups = appendGroups(user.Groups, me.MemberOf)

	next := me.NextLink
	for pages := 1; next != ""; pages++ {
		if pages >= maxPages {
			return User{}, fmt.Errorf("ocisgraph: more than %d pages of groups", maxPages)
		}
		nu, err := url.Parse(next)
		if err != nil || nu.Scheme != c.base.Scheme || nu.Host != c.base.Host {
			return User{}, errors.New("ocisgraph: next page of groups is not on the server")
		}
		var page pageResponse
		if err := c.get(ctx, next, token, &page); err != nil {
			return User{}, err
		}
		user.Groups = appendGroups(user.Groups, page.Value)
		next = page.NextLink
	}
	return user, nil
}

func appendGroups(dst []Group, src []graphGroup) []Group {
	for _, g := range src {
		if g.ID != "" {
			dst = append(dst, Group{ID: g.ID, Name: g.DisplayName})
		}
	}
	return dst
}

func (c *Client) get(ctx context.Context, rawURL, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("ocisgraph: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		// The error of net/http names the URL, never the token.
		return fmt.Errorf("ocisgraph: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return fmt.Errorf("%w: status %d", ErrUnauthorized, resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("ocisgraph: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("ocisgraph: reading answer: %w", err)
	}
	if len(body) > maxBody {
		return errors.New("ocisgraph: answer is too large")
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("ocisgraph: decoding answer: %w", err)
	}
	return nil
}
