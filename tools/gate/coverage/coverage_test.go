package coverage_test

import (
	"testing"

	"github.com/latebit-io/go_project/tools/gate/coverage"
)

const profile = `mode: atomic
example.com/shop/internal/a/a.go:1.1,2.2 3 1
example.com/shop/internal/a/a.go:3.1,4.2 1 0
example.com/shop/internal/a/a.go:3.1,4.2 1 2
example.com/shop/internal/b/b.go:1.1,2.2 4 0
example.com/shop/cmd/api/main.go:1.1,9.2 20 0
`

func TestPercent(t *testing.T) {
	t.Parallel()

	got, ok := coverage.Percent(profile, "example.com/shop/cmd/")
	if !ok || got != 50 {
		t.Fatalf("Percent() = %v, %v, want 50: 4 of 8 statements outside cmd", got, ok)
	}
}

func TestPercentWithoutStatements(t *testing.T) {
	t.Parallel()

	if _, ok := coverage.Percent("mode: atomic\nnot a block line\n", "x"); ok {
		t.Fatal("Percent() ok = true, want false for a profile without statements")
	}
}
