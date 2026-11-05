package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/owncloud/ocis-groupware/internal/config"
)

const adminUsage = `usage: ocis-groupware admin [--config <file>] [--socket <path>] principals <command>

commands:
  list [--json]                        every user, one per line
  disable (<username> | --sub <sub>)   refuse the user's sign-ins
  enable  (<username> | --sub <sub>)   let the user sign in again
  purge   (<username> | --sub <sub>) [--yes]
                                       delete a disabled user's data; without
                                       --yes it only shows what would go

The command talks to the running service over its admin socket, so the
configuration (--config, GROUPWARE_DATA_DIR, GROUPWARE_ADMIN_SOCKET) must name
the same data directory as the service.
`

// adminCommand is a parsed admin command line.
type adminCommand struct {
	configPath string
	socket     string
	verb       string
	username   string
	sub        string
	json       bool
	yes        bool
}

// runAdmin runs "ocis-groupware admin ..." and returns the exit code: 0 on
// success, 1 on failure and 2 for a usage error.
func runAdmin(args []string, stdout, stderr io.Writer) int {
	cmd, err := parseAdmin(args)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "ocis-groupware:", err)
		_, _ = fmt.Fprint(stderr, adminUsage)
		return 2
	}
	if err := cmd.run(stdout); err != nil {
		if !errors.Is(err, errDryRun) {
			_, _ = fmt.Fprintln(stderr, "ocis-groupware:", err)
		}
		return 1
	}
	return 0
}

// parseAdmin parses the arguments after "admin". Flags before the noun are
// the shared ones; each verb has its own.
func parseAdmin(args []string) (adminCommand, error) {
	var cmd adminCommand
	shared := flag.NewFlagSet("admin", flag.ContinueOnError)
	shared.SetOutput(io.Discard)
	shared.StringVar(&cmd.configPath, "config", "", "")
	shared.StringVar(&cmd.socket, "socket", "", "")
	if err := shared.Parse(args); err != nil {
		return cmd, err
	}
	rest := shared.Args()
	if len(rest) < 2 || rest[0] != "principals" {
		return cmd, errors.New(`expected "principals <command>"`)
	}
	cmd.verb = rest[1]

	verb := flag.NewFlagSet("principals "+cmd.verb, flag.ContinueOnError)
	verb.SetOutput(io.Discard)
	switch cmd.verb {
	case "list":
		verb.BoolVar(&cmd.json, "json", false, "")
	case "disable", "enable":
		verb.StringVar(&cmd.sub, "sub", "", "")
	case "purge":
		verb.StringVar(&cmd.sub, "sub", "", "")
		verb.BoolVar(&cmd.yes, "yes", false, "")
	default:
		return cmd, fmt.Errorf("unknown command %q", cmd.verb)
	}
	positional, err := parseInterleaved(verb, rest[2:])
	if err != nil {
		return cmd, err
	}
	if cmd.verb == "list" {
		if len(positional) > 0 {
			return cmd, errors.New("list takes no arguments")
		}
		return cmd, nil
	}
	subGiven := false
	verb.Visit(func(f *flag.Flag) {
		if f.Name == "sub" {
			subGiven = true
		}
	})
	switch {
	case len(positional) > 1:
		return cmd, fmt.Errorf("%s takes one username", cmd.verb)
	case len(positional) == 1 && subGiven:
		return cmd, errors.New("give a username or --sub, not both")
	case len(positional) == 1:
		cmd.username = positional[0]
	case subGiven && strings.TrimSpace(cmd.sub) == "":
		return cmd, errors.New("--sub must not be empty")
	case !subGiven:
		return cmd, fmt.Errorf("%s needs a username or --sub", cmd.verb)
	}
	return cmd, nil
}

// parseInterleaved parses args with flags, letting flags follow positional
// arguments ("purge alice --yes"), which the flag package alone does not.
// Everything after "--" is positional.
func parseInterleaved(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		rest := flags.Args()
		if len(rest) == 0 {
			return positional, nil
		}
		if consumed := args[:len(args)-len(rest)]; len(consumed) > 0 && consumed[len(consumed)-1] == "--" {
			return append(positional, rest...), nil
		}
		positional = append(positional, rest[0])
		args = rest[1:]
	}
}

// run executes the command against the service's admin socket. Of the
// configuration it validates only what it uses: the socket path, or the
// data directory the path derives from.
func (cmd adminCommand) run(stdout io.Writer) error {
	cfg, err := config.Load(cmd.configPath)
	if err != nil {
		return err
	}
	socket := cmd.socket
	if socket == "" {
		socket = cfg.AdminSocket
	}
	if socket == "" {
		return errors.New("config: data_dir must be set (GROUPWARE_DATA_DIR) or --socket given")
	}
	c := newAdminClient(socket, cfg.DataDir)

	switch cmd.verb {
	case "list":
		return cmd.list(c, stdout)
	case "disable":
		return cmd.setDisabled(c, stdout, true)
	case "enable":
		return cmd.setDisabled(c, stdout, false)
	case "purge":
		return cmd.purge(c, stdout)
	default:
		return fmt.Errorf("unknown command %q", cmd.verb)
	}
}

// errDryRun is a purge that stopped at showing what would go. It exits with
// 1 and no message: the output already says what to do.
var errDryRun = errors.New("dry run")

func (cmd adminCommand) purge(c *adminClient, stdout io.Writer) error {
	p, err := cmd.resolve(c)
	switch {
	case err == nil && !p.Disabled:
		return cmd.notDisabled(p)
	case err != nil && cmd.sub != "":
		// A directory that lost its principal.json is not listed, but the
		// service can still purge it by sub.
		p = principal{Sub: cmd.sub}
	case err != nil:
		return err
	}

	raw, err := c.do(http.MethodPost, "/v1/principals/purge", map[string]any{"sub": p.Sub, "dry_run": !cmd.yes})
	var status *statusError
	switch {
	case err == nil:
	case errors.As(err, &status) && status.status == http.StatusNotFound:
		return cmd.notFound()
	case errors.As(err, &status) && status.status == http.StatusConflict:
		return cmd.notDisabled(p)
	default:
		return err
	}
	if cmd.yes {
		_, err := fmt.Fprintf(stdout, "purged %s\n", label(p))
		return err
	}
	var report struct {
		Calendars    int `json:"calendars"`
		AddressBooks int `json:"addressbooks"`
		AppPasswords int `json:"app_passwords"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return fmt.Errorf("reading the service's answer: %w", err)
	}
	_, err = fmt.Fprintf(stdout, "would purge %s: %s, %s, %s\nadd --yes to delete this data; it cannot be undone\n",
		label(p),
		plural(report.Calendars, "calendar", "calendars"),
		plural(report.AddressBooks, "address book", "address books"),
		plural(report.AppPasswords, "app password", "app passwords"))
	if err != nil {
		return err
	}
	return errDryRun
}

// notDisabled tells the admin to disable the user first, with the same
// selector the command was given.
func (cmd adminCommand) notDisabled(p principal) error {
	selector := cmd.username
	if selector == "" {
		selector = "--sub " + cmd.sub
	}
	return fmt.Errorf("%s is not disabled; run \"admin principals disable %s\" first", nameOf(p), selector)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// resolve finds the user the command names, by username or by sub, in the
// service's list. The admin API works on subs only.
func (cmd adminCommand) resolve(c *adminClient) (principal, error) {
	list, _, err := c.list()
	if err != nil {
		return principal{}, err
	}
	for _, p := range list {
		if cmd.username != "" && p.Username == cmd.username || cmd.username == "" && p.Sub == cmd.sub {
			return p, nil
		}
	}
	return principal{}, cmd.notFound()
}

func (cmd adminCommand) notFound() error {
	if cmd.username != "" {
		return fmt.Errorf("no user named %q", cmd.username)
	}
	return fmt.Errorf("no user with sub %q", cmd.sub)
}

// label names a user in output: the username with the sub in brackets, or
// the sub alone for a user without a username.
func label(p principal) string {
	if p.Username == "" {
		return p.Sub
	}
	return fmt.Sprintf("%s (sub %s)", p.Username, p.Sub)
}

func (cmd adminCommand) setDisabled(c *adminClient, stdout io.Writer, disabled bool) error {
	p, err := cmd.resolve(c)
	if err != nil {
		return err
	}
	state := "enabled"
	if disabled {
		state = "disabled"
	}
	if p.Disabled == disabled {
		_, err := fmt.Fprintf(stdout, "%s is already %s\n", nameOf(p), state)
		return err
	}
	if _, err := c.do(http.MethodPost, "/v1/principals/"+cmd.verb, map[string]string{"sub": p.Sub}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "%s %s\n", state, label(p))
	return err
}

// nameOf is the username, or the sub for a user without one.
func nameOf(p principal) string {
	if p.Username == "" {
		return p.Sub
	}
	return p.Username
}

func (cmd adminCommand) list(c *adminClient, stdout io.Writer) error {
	list, raw, err := c.list()
	if err != nil {
		return err
	}
	if cmd.json {
		_, err := stdout.Write(raw)
		return err
	}
	tw := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(tw, "USERNAME\tDISPLAY NAME\tSTATE\tCREATED\tSUB")
	for _, p := range list {
		name := p.Username
		if name == "" {
			name = "-"
		}
		state := "enabled"
		if p.Disabled {
			state = "disabled"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, p.DisplayName, state,
			p.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), p.Sub)
	}
	return tw.Flush()
}

// principal is a principal as the admin API lists it.
type principal struct {
	Sub         string    `json:"sub"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
	Disabled    bool      `json:"disabled"`
}

// adminClient talks to the admin API over its unix socket.
type adminClient struct {
	socket  string
	dataDir string
	http    *http.Client
}

func newAdminClient(socket, dataDir string) *adminClient {
	return &adminClient{
		socket:  socket,
		dataDir: dataDir,
		http: &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}

// list returns every principal, and the server's answer as it came.
func (c *adminClient) list() ([]principal, []byte, error) {
	raw, err := c.do(http.MethodGet, "/v1/principals", nil)
	if err != nil {
		return nil, nil, err
	}
	var answer struct {
		Principals []principal `json:"principals"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, nil, fmt.Errorf("reading the service's answer: %w", err)
	}
	return answer.Principals, raw, nil
}

// do sends one request and returns the body of a 200 answer. Any other
// status is returned as an error carrying the status and the server's
// message.
func (c *adminClient) do(method, path string, body any) ([]byte, error) {
	var payload io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		payload = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://admin"+path, payload)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if c.notRunning(err) {
			if c.dataDir != "" {
				return nil, fmt.Errorf("the service is not running on %s (no admin socket at %s)", c.dataDir, c.socket)
			}
			return nil, fmt.Errorf("the service is not running (no admin socket at %s)", c.socket)
		}
		return nil, fmt.Errorf("admin socket %s: %w", c.socket, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the service's answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{status: resp.StatusCode, message: errorMessage(raw, resp.Status)}
	}
	return raw, nil
}

// notRunning reports whether err means nobody serves the socket: the file
// is missing, or it is there but nothing accepts connections.
func (c *adminClient) notRunning(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
}

// statusError is an answer other than 200.
type statusError struct {
	status  int
	message string
}

func (e *statusError) Error() string { return e.message }

// errorMessage returns the "error" field of a JSON body, or fallback.
func errorMessage(raw []byte, fallback string) string {
	var answer struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &answer); err == nil && answer.Error != "" {
		return answer.Error
	}
	return fallback
}
