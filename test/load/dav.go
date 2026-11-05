package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// client talks to the service as one principal at a time.
type client struct {
	base string
	http *http.Client
	rec  *recorder
}

func newClient(base string, rec *recorder) *client {
	tr := &http.Transport{MaxIdleConns: 1024, MaxIdleConnsPerHost: 1024, IdleConnTimeout: 90 * time.Second}
	return &client{base: base, http: &http.Client{Transport: tr, Timeout: 5 * time.Minute}, rec: rec}
}

func basicAuth(u user) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(u.Name+":"+u.Secret))
}

// do sends one request and records its latency under op. The body is read in
// full, as a client would. It returns the status and the body.
func (c *client) do(ctx context.Context, op string, u user, method, path string, body string, hdr ...string) (int, []byte, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", basicAuth(u))
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	began := time.Now()
	resp, err := c.http.Do(req)
	var data []byte
	if err == nil {
		data, err = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
	}
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if err == nil && status >= 400 {
		err = fmt.Errorf("%s %s: status %d", method, path, status)
	}
	// A request cut off by the end of the run says nothing about the service.
	if c.rec != nil && ctx.Err() == nil {
		c.rec.add(op, time.Since(began), err)
	}
	return status, data, err
}

var responseRE = regexp.MustCompile(`<(?:\w+:)?response>`)

const xmlHeader = `<?xml version="1.0" encoding="utf-8"?>`

func syncBody(token string) string {
	return xmlHeader + `<d:sync-collection xmlns:d="DAV:"><d:sync-token>` + token + `</d:sync-token><d:sync-level>1</d:sync-level><d:prop><d:getetag/></d:prop></d:sync-collection>`
}

// rangeBody asks for the events in [from, from+7 days).
func rangeBody(from time.Time) string {
	return fmt.Sprintf(xmlHeader+`<c:calendar-query xmlns:d="DAV:" xmlns:c="urn:ietf:params:xml:ns:caldav"><d:prop><d:getetag/><c:calendar-data/></d:prop><c:filter><c:comp-filter name="VCALENDAR"><c:comp-filter name="VEVENT"><c:time-range start="%s" end="%s"/></c:comp-filter></c:comp-filter></c:filter></c:calendar-query>`,
		from.Format("20060102T150405Z"), from.Add(7*24*time.Hour).Format("20060102T150405Z"))
}

const homePropfind = xmlHeader + `<d:propfind xmlns:d="DAV:"><d:prop><d:displayname/><d:resourcetype/></d:prop></d:propfind>`

// sync runs sync-collection on path from token and returns the new token.
func (c *client) sync(ctx context.Context, op string, u user, path, token string) (string, error) {
	_, body, err := c.do(ctx, op, u, "REPORT", path, syncBody(token), "Content-Type", "application/xml", "Depth", "0")
	if err != nil {
		return token, err
	}
	if t := parseSyncToken(body); t != "" {
		return t, nil
	}
	return token, fmt.Errorf("no sync token in the reply to %s", path)
}

func (c *client) rangeQuery(ctx context.Context, op string, u user, path string, from time.Time) (int, error) {
	_, body, err := c.do(ctx, op, u, "REPORT", path, rangeBody(from), "Content-Type", "application/xml", "Depth", "1")
	return len(responseRE.FindAll(body, -1)), err
}

func (c *client) home(ctx context.Context, op string, u user) error {
	_, _, err := c.do(ctx, op, u, "PROPFIND", "/dav/calendars/"+u.Name+"/", homePropfind, "Content-Type", "application/xml", "Depth", "1")
	return err
}

func (c *client) put(ctx context.Context, op string, u user, path string, data []byte) error {
	_, _, err := c.do(ctx, op, u, "PUT", path, string(data), "Content-Type", "text/calendar", "If-None-Match", "*")
	return err
}
