package proctest_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

const git = "git"

var errBoom = errors.New("boom")

func script() map[string]proctest.Answer {
	return map[string]proctest.Answer{
		git:          {Out: "generic"},
		"git status": {Out: "specific"},
		"go vet":     {Err: errBoom},
	}
}

func TestFake(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stream   bool
		env      []string
		command  []string
		want     string
		wantErr  error
		wantCall string
	}{
		{
			name: "longest match wins", command: []string{git, "status", "--porcelain"},
			want: "specific", wantCall: "git status --porcelain",
		},
		{name: "shorter match applies", command: []string{git, "log"}, want: "generic", wantCall: "git log"},
		{name: "unscripted command succeeds empty", command: []string{"ls"}, wantCall: "ls"},
		{
			name: "stream returns the scripted error", stream: true,
			env: []string{"GOOS=linux"}, command: []string{"go", "vet", "./..."},
			wantErr: errBoom, wantCall: "GOOS=linux go vet ./...",
		},
		{name: "output returns the scripted error", command: []string{"go", "vet"}, wantErr: errBoom, wantCall: "go vet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fake := &proctest.Fake{Answers: script()}

			var (
				got string
				err error
			)

			if tt.stream {
				err = fake.Stream(t.Context(), tt.env, tt.command[0], tt.command[1:]...)
			} else {
				got, err = fake.Output(t.Context(), tt.env, tt.command[0], tt.command[1:]...)
			}

			if got != tt.want || !errors.Is(err, tt.wantErr) {
				t.Fatalf("got %q, %v, want %q, %v", got, err, tt.want, tt.wantErr)
			}

			if calls := fake.Calls(); !slices.Equal(calls, []string{tt.wantCall}) {
				t.Fatalf("Calls() = %v, want [%s]", calls, tt.wantCall)
			}
		})
	}
}
