package initmod_test

import (
	"errors"
	"io/fs"
	"os"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/gate/initmod"
	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

const source = `// Package a mentions example.com/old in prose.
package a

import (
	"example.com/old"
	"example.com/old/internal/b"
	"example.com/older/c"
)
`

// module writes a small module to a scratch directory and returns its path.
func module(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	files := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/old\n")},
		"a.go":   {Data: []byte(source)},
	}

	if err := os.CopyFS(root, files); err != nil {
		t.Fatalf("write module: %v", err)
	}

	return root
}

func TestRenameRewritesImportsOnly(t *testing.T) {
	t.Parallel()

	root := module(t)
	fake := &proctest.Fake{}

	if err := initmod.Rename(t.Context(), root, fake, "github.com/acme/shop"); err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	got, err := fs.ReadFile(os.DirFS(root), "a.go")
	if err != nil {
		t.Fatalf("read a.go: %v", err)
	}

	want := `// Package a mentions example.com/old in prose.
package a

import (
	"github.com/acme/shop"
	"github.com/acme/shop/internal/b"
	"example.com/older/c"
)
`
	if string(got) != want {
		t.Fatalf("a.go after Rename() =\n%s\nwant\n%s", got, want)
	}

	calls := []string{"go mod edit -module github.com/acme/shop", "golangci-lint fmt"}
	if !slices.Equal(fake.Calls(), calls) {
		t.Fatalf("Rename() ran %v, want %v", fake.Calls(), calls)
	}
}

func TestRenameRejectsBadPaths(t *testing.T) {
	t.Parallel()

	root := module(t)

	for _, next := range []string{"Bad Path", "shop", "example.com/old"} {
		if err := initmod.Rename(t.Context(), root, &proctest.Fake{}, next); !errors.Is(err, initmod.ErrModule) {
			t.Errorf("Rename(%q) = %v, want a module path error", next, err)
		}
	}
}

func TestRenameReportsToolFailure(t *testing.T) {
	t.Parallel()

	root := module(t)
	errTool := errors.New("exit status 1")

	for _, failing := range []string{"go mod edit", "golangci-lint fmt"} {
		fake := &proctest.Fake{Answers: map[string]proctest.Answer{failing: {Err: errTool}}}
		if err := initmod.Rename(t.Context(), root, fake, "github.com/acme/"+failing[:2]); !errors.Is(err, errTool) {
			t.Errorf("Rename() with failing %q = %v, want the tool failure", failing, err)
		}
	}
}
