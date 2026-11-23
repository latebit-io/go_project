package proc_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/latebit-io/go_project/tools/gate/proc"
)

// invocation is one use of Exec: go started with args, streamed or captured.
type invocation struct {
	stream bool
	env    []string
	args   []string
}

// run returns what the program printed, by either route.
func (i invocation) run(t *testing.T) (string, error) {
	t.Helper()

	var streamed bytes.Buffer

	exec := proc.Exec{Dir: t.TempDir(), Out: &streamed}
	if i.stream {
		err := exec.Stream(t.Context(), i.env, "go", i.args...)

		return streamed.String(), err
	}

	return exec.Output(t.Context(), i.env, "go", i.args...)
}

func TestExec(t *testing.T) {
	t.Parallel()

	plan9 := []string{"GOOS=plan9"}

	tests := []struct {
		name    string
		call    invocation
		want    string
		wantErr string
	}{
		{name: "output is captured", call: invocation{args: []string{"env", "GOOS"}, env: plan9}, want: "plan9"},
		{name: "output failure carries stderr", call: invocation{args: []string{"no-such"}}, wantErr: "unknown command"},
		{
			name: "stream writes to the user",
			call: invocation{stream: true, args: []string{"env", "GOOS"}, env: plan9}, want: "plan9",
		},
		{
			name: "stream failure is reported",
			call: invocation{stream: true, args: []string{"no-such"}}, wantErr: "go no-such",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := tt.call.run(t)

			failed := ""
			if err != nil {
				failed = err.Error()
			}

			if !strings.Contains(failed, tt.wantErr) || (tt.wantErr == "") != (err == nil) {
				t.Fatalf("error = %v, want one mentioning %q", err, tt.wantErr)
			}

			if tt.wantErr == "" && strings.TrimSpace(got) != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
