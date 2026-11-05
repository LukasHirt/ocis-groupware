//go:build stack

package stack

import (
	"os"
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

	script := groupkitScript(ctx, t, "accept.py")

	a := signInWithAppPassword(t, pyUser, pyPassword)
	runPython(ctx, t, python, script, caFile, baseURL+davPrefix, a)
}
