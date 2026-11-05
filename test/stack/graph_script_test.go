//go:build stack

package stack

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestPlainGraphScript runs graph_script.py, which lists and creates events
// and contacts with nothing but the Python requests package, as an
// integration written without a Graph SDK would. STACK_PYTHON names an
// interpreter that has requests installed; without it the test is skipped. The
// script cleans up after itself, so it can run beside the other flows as the
// same user.
func TestPlainGraphScript(t *testing.T) {
	python := os.Getenv("STACK_PYTHON")
	if python == "" {
		t.Skip("STACK_PYTHON is not set")
	}
	script, err := filepath.Abs("graph_script.py")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(ctxTimeout(t), python, "-I", "-c", "import requests").CombinedOutput(); err != nil {
		t.Skipf("the interpreter has no requests package: %s", out)
	}

	cmd := exec.CommandContext(t.Context(), python, "-I", script)
	cmd.Env = append(os.Environ(),
		"GRAPH_URL="+baseURL+"/graph/v1.0",
		"GRAPH_TOKEN="+signIn(t, goUser, goPassword),
	)
	if ca := os.Getenv("STACK_CA_FILE"); ca != "" {
		cmd.Env = append(cmd.Env, "REQUESTS_CA_BUNDLE="+ca)
	}
	out, err := cmd.CombinedOutput()
	t.Logf("%s", out)
	if err != nil {
		t.Fatalf("the Graph script failed: %v", err)
	}
}
