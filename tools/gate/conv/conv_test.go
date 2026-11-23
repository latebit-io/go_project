package conv_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/latebit-io/go_project/tools/gate/conv"
)

func TestCheckBranch(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"feat/order-export":       true,
		"fix/nil-task":            true,
		"main":                    true,
		"HEAD":                    true,
		"dependabot/go_modules/x": true,
		"Feature/x":               false,
		"feat/Order_Export":       false,
		"feat/":                   false,
		"wip/x":                   false,
		"order-export":            false,
	}

	for branch, ok := range tests {
		t.Run(branch, func(t *testing.T) {
			t.Parallel()

			err := conv.CheckBranch(branch)
			if (err == nil) != ok || (err != nil && !errors.Is(err, conv.ErrConvention)) {
				t.Fatalf("CheckBranch(%q) = %v, want ok=%v", branch, err, ok)
			}
		})
	}
}

func TestCheckSubject(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"feat(orders): add csv export":               "",
		"fix: handle nil task":                       "",
		"feat!: drop v1 api":                         "",
		"chore: 2 fast 2 furious now":                "",
		"Merge branch 'main' into feat/x":            "",
		" fix: handle nil task":                      "starts or ends with whitespace",
		"added stuff":                                "is not <type>",
		"wip: try things":                            "is not <type>",
		"feat: add a very long description here now": "7-word description",
		"feat: Add export":                           "starts lowercase",
		"feat: add export.":                          "no final period",
	}

	for subject, want := range tests {
		t.Run(subject, func(t *testing.T) {
			t.Parallel()

			err := conv.CheckSubject(subject)
			if want == "" && err != nil {
				t.Fatalf("CheckSubject(%q) = %v, want nil", subject, err)
			}

			if want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
				t.Fatalf("CheckSubject(%q) = %v, want an error mentioning %q", subject, err, want)
			}
		})
	}
}

func TestNeedsTest(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		"feat/x": true, "fix/x": true, "perf/x": true, "test/x": true,
		"refactor/x": false, "chore/x": false, "docs/x": false, "main": false, "HEAD": false, "dependabot/x": false,
	}

	for branch, want := range tests {
		if got := conv.NeedsTest(branch); got != want {
			t.Errorf("NeedsTest(%q) = %v, want %v", branch, got, want)
		}
	}
}
