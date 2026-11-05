package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// service is a running ocis-groupware binary.
type service struct {
	cmd    *exec.Cmd
	URL    string
	stderr *bytes.Buffer
	done   chan struct{}
	// StartToReady is the time from starting the process to the first
	// successful /healthz.
	StartToReady time.Duration

	mu      sync.Mutex
	peakRSS int64 // kilobytes
	stopRSS chan struct{}
}

// buildBinary builds the service from the module that contains the working
// directory into dir and returns the path.
func buildBinary(dir string) (string, error) {
	out := filepath.Join(dir, "ocis-groupware")
	cmd := exec.Command("go", "build", "-o", out, "github.com/owncloud/ocis-groupware/cmd/ocis-groupware")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("go build: %v\n%s", err, b)
	}
	return out, nil
}

// startService starts bin on dataDir and waits until it is healthy. The
// identity provider address points at nothing: the load uses app passwords,
// which are checked without contacting it.
func startService(ctx context.Context, bin, dataDir, sockDir string) (*service, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	addr := l.Addr().String()
	_ = l.Close()

	s := &service{URL: "http://" + addr, stderr: &bytes.Buffer{}, done: make(chan struct{}), stopRSS: make(chan struct{})}
	s.cmd = exec.Command(bin)
	s.cmd.Env = append(os.Environ(),
		"GROUPWARE_HTTP_ADDR="+addr,
		"GROUPWARE_LOG_LEVEL=warn",
		"GROUPWARE_OIDC_ISSUER=http://127.0.0.1:1",
		"GROUPWARE_DATA_DIR="+dataDir,
		"GROUPWARE_ADMIN_SOCKET="+filepath.Join(sockDir, "admin.sock"),
	)
	s.cmd.Stderr = s.stderr
	s.cmd.Stdout = s.stderr
	began := time.Now()
	if err := s.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = s.cmd.Wait(); close(s.done) }()

	client := &http.Client{Timeout: 2 * time.Second}
	for {
		select {
		case <-s.done:
			return nil, fmt.Errorf("the service exited while starting:\n%s", s.stderr.String())
		case <-ctx.Done():
			s.stop()
			return nil, ctx.Err()
		default:
		}
		resp, err := client.Get(s.URL + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		if time.Since(began) > 10*time.Minute {
			s.stop()
			return nil, fmt.Errorf("the service was not healthy after 10 minutes:\n%s", s.stderr.String())
		}
	}
	s.StartToReady = time.Since(began)
	go s.sampleRSS()
	return s, nil
}

// sampleRSS records the peak resident set size of the process once a second.
func (s *service) sampleRSS() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		if kb, err := rssKB(s.cmd.Process.Pid); err == nil {
			s.mu.Lock()
			s.peakRSS = max(s.peakRSS, kb)
			s.mu.Unlock()
		}
		select {
		case <-t.C:
		case <-s.stopRSS:
			return
		case <-s.done:
			return
		}
	}
}

// rssKB returns the resident set size of pid in kilobytes.
func rssKB(pid int) (int64, error) {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
}

// RSS returns the current resident set size in kilobytes, and also counts it
// into the peak.
func (s *service) RSS() int64 {
	kb, err := rssKB(s.cmd.Process.Pid)
	if err != nil {
		return 0
	}
	s.mu.Lock()
	s.peakRSS = max(s.peakRSS, kb)
	s.mu.Unlock()
	return kb
}

// PeakRSS returns the largest resident set size seen, in kilobytes.
func (s *service) PeakRSS() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peakRSS
}

// stop ends the process with SIGTERM and waits for it, killing it after 30
// seconds.
func (s *service) stop() {
	select {
	case <-s.done:
		return
	default:
	}
	_ = s.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-s.done:
	case <-time.After(30 * time.Second):
		_ = s.cmd.Process.Kill()
		<-s.done
	}
}
