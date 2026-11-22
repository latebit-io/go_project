package proc_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/latebit-io/go_project/tools/gate/proc"
)

func TestOutputCapturesStdoutAndEnv(t *testing.T) {
	t.Parallel()

	run := proc.Exec{Dir: t.TempDir()}

	got, err := run.Output(t.Context(), []string{"GOOS=plan9"}, "go", "env", "GOOS")
	if err != nil || strings.TrimSpace(got) != "plan9" {
		t.Fatalf("Output() = %q, %v, want plan9", got, err)
	}
}

func TestOutputFailureCarriesStderr(t *testing.T) {
	t.Parallel()

	_, err := proc.Exec{Dir: t.TempDir()}.Output(t.Context(), nil, "go", "no-such-command")
	if err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("Output() error = %v, want go's own message", err)
	}
}

func TestStreamWritesToTheUser(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	run := proc.Exec{Dir: t.TempDir(), Out: &out}
	if err := run.Stream(t.Context(), nil, "go", "env", "GOARCH"); err != nil || out.Len() == 0 {
		t.Fatalf("Stream() = %v with output %q, want the architecture printed", err, out.String())
	}

	if err := run.Stream(t.Context(), nil, "go", "no-such-command"); err == nil {
		t.Fatal("Stream() error = nil, want the failure")
	}
}
