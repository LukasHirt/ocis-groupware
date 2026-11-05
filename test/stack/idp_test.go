//go:build stack

package stack

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"
)

// signIn obtains an access token for a demo user from the oCIS identity
// provider the way the web UI does: the logon API creates a session, the
// authorization endpoint answers with a code (PKCE, S256), and the token
// endpoint exchanges it with the "web" client, which needs no secret.
func signIn(t *testing.T, user, password string) string {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
		Jar:       jar,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	const (
		clientID = "web"
		scope    = "openid profile email"
	)
	redirectURI := baseURL + "/oidc-callback.html"

	verifier := randomString(t)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	state := randomString(t)

	// Logon: creates the identity provider session, kept in the cookie jar.
	logon, err := json.Marshal(map[string]any{
		"params": []string{user, password, "1"},
		"hello": map[string]string{
			"scope": scope, "client_id": clientID, "redirect_uri": redirectURI, "flow": "oidc",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL+"/signin/v1/identifier/_/logon", bytes.NewReader(logon))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Kopano-Konnect-XSRF", "1")
	req.Header.Set("Referer", baseURL+"/")
	status, body := send(t, client, req)
	if status != http.StatusOK {
		t.Fatalf("logon as %s: status %d: %s", user, status, body)
	}

	// Authorization: the session turns into a code on the redirect.
	q := url.Values{
		"client_id":             {clientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"response_mode":         {"query"},
		"scope":                 {scope},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"prompt":                {"none"},
	}
	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, baseURL+"/signin/v1/identifier/_/authorize?"+q.Encode(), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	location, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("authorize: Location %q: %v", resp.Header.Get("Location"), err)
	}
	code := location.Query().Get("code")
	if resp.StatusCode != http.StatusFound || code == "" {
		t.Fatalf("authorize: status %d, Location %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got := location.Query().Get("state"); got != state {
		t.Fatalf("authorize: state %q, want %q", got, state)
	}

	// Token: the code and the verifier become an access token.
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	req, err = http.NewRequestWithContext(t.Context(), http.MethodPost, baseURL+"/konnect/v1/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	status, body = send(t, client, req)
	if status != http.StatusOK {
		t.Fatalf("token: status %d: %s", status, body)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
	}
	if err := json.Unmarshal([]byte(body), &token); err != nil {
		t.Fatalf("token: %v: %s", err, body)
	}
	if token.AccessToken == "" || !strings.EqualFold(token.TokenType, "Bearer") {
		t.Fatalf("token: unexpected answer %s", body)
	}
	return token.AccessToken
}

func send(t *testing.T, client *http.Client, req *http.Request) (int, string) {
	t.Helper()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func randomString(t *testing.T) string {
	t.Helper()
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// appPassword creates an app password for the user behind token and removes
// it when the test ends. It returns the secret.
func appPassword(t *testing.T, token, name string) string {
	t.Helper()
	ctx := ctxTimeout(t)
	headers := map[string]string{"Authorization": "Bearer " + token, "Content-Type": "application/json"}
	status, _, body := do(t, ctx, http.MethodPost, "/groupware/v1/app-passwords", headers, strings.NewReader(`{"name":"`+name+`"}`))
	if status != http.StatusCreated {
		t.Fatalf("creating an app password: status %d: %s", status, body)
	}
	var created struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatalf("creating an app password: %v: %s", err, body)
	}
	if created.ID == "" || created.Password == "" {
		t.Fatalf("creating an app password: unexpected answer %s", body)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		status, _, body := do(t, ctx, http.MethodDelete, "/groupware/v1/app-passwords/"+created.ID, headers, nil)
		if status != http.StatusNoContent {
			t.Logf("revoking app password %s: status %d: %s", created.ID, status, body)
		}
	})
	return created.Password
}
