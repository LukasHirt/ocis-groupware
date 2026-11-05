//go:build stack

package stack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPythonClient runs the Python caldav client check that ships with
// groupkit against the stack. STACK_PYTHON names an interpreter that has the
// client installed (groupkit's test/pyclient/requirements.txt), and
// STACK_CA_FILE is needed because the client verifies the certificate. The
// check expects a user with nothing but the default calendar, so it signs in
// as marie, whom the Go flows leave alone.
func TestPythonClient(t *testing.T) {
	needsReport(t)
	python := os.Getenv("STACK_PYTHON")
	if python == "" {
		t.Skip("STACK_PYTHON is not set")
	}
	caFile := os.Getenv("STACK_CA_FILE")
	if caFile == "" {
		t.Skip("STACK_CA_FILE is not set; the Python client needs the certificate")
	}
	ctx := ctxTimeout(t)

	dir, err := exec.CommandContext(ctx, "go", "list", "-m", "-f", "{{.Dir}}", "github.com/LukasHirt/groupkit").Output()
	if err != nil {
		t.Fatalf("locating the groupkit module: %v", err)
	}
	script := filepath.Join(strings.TrimSpace(string(dir)), "test", "pyclient", "accept.py")
	if _, err := os.Stat(script); err != nil {
		t.Fatal(err)
	}

	a := signInWithAppPassword(t, pyUser, pyPassword)
	cmd := exec.CommandContext(ctx, python, "-I", script)
	cmd.Env = append(os.Environ(),
		"GROUPKIT_URL="+baseURL+davPrefix,
		"GROUPKIT_USER="+a.user,
		"GROUPKIT_PASSWORD="+a.appPassword,
		"REQUESTS_CA_BUNDLE="+caFile,
	)
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("the Python client check failed: %v", err)
	}
}
