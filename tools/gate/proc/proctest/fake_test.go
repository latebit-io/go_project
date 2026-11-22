package proctest_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

func TestFakeAnswersLongestMatchAndRecords(t *testing.T) {
	t.Parallel()

	errBoom := errors.New("boom")
	fake := &proctest.Fake{Answers: map[string]proctest.Answer{
		"git":        {Out: "generic"},
		"git status": {Out: "specific"},
		"go vet":     {Err: errBoom},
	}}

	if out, err := fake.Output(t.Context(), nil, "git", "status", "--porcelain"); out != "specific" || err != nil {
		t.Fatalf("Output(git status) = %q, %v, want the longest match", out, err)
	}

	if out, err := fake.Output(t.Context(), nil, "ls"); out != "" || err != nil {
		t.Fatalf("Output(ls) = %q, %v, want an empty success", out, err)
	}

	if err := fake.Stream(t.Context(), []string{"GOOS=linux"}, "go", "vet", "./..."); !errors.Is(err, errBoom) {
		t.Fatalf("Stream(go vet) = %v, want the scripted error", err)
	}

	want := []string{"git status --porcelain", "ls", "GOOS=linux go vet ./..."}
	if got := fake.Calls(); !slices.Equal(got, want) {
		t.Fatalf("Calls() = %v, want %v", got, want)
	}
}
