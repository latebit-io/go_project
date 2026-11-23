package initmod_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/gate/initmod"
	"github.com/latebit-io/go_project/tools/gate/proc/proctest"
)

const (
	next   = "github.com/acme/shop"
	edit   = "go mod edit -module " + next
	pin    = "go get -tool github.com/latebit-io/go_project/tools/gate@latest"
	format = "golangci-lint fmt"

	source = `// Package a mentions "example.com/old" in prose.
package a

import (
	"example.com/old"
	b "example.com/old/internal/b"
	"example.com/older/c"
)

// schemaID is data, not an import, and must survive the rename.
const schemaID = "example.com/old/schema"
`

	renamed = `// Package a mentions "example.com/old" in prose.
package a

import (
	"github.com/acme/shop"
	b "github.com/acme/shop/internal/b"
	"example.com/older/c"
)

// schemaID is data, not an import, and must survive the rename.
const schemaID = "example.com/old/schema"
`
)

var (
	errTool = errors.New("exit status 1")
	// errAny stands for "some error" where its identity does not matter.
	errAny = errors.New("any error")
)

// matches reports whether err is what a case expects.
func matches(err, want error) bool {
	if errors.Is(want, errAny) {
		return err != nil
	}

	return errors.Is(err, want)
}

// renameCase is one way of calling Rename and what must come of it.
type renameCase struct {
	name    string
	extra   fstest.MapFS
	answers map[string]proctest.Answer
	next    string
	// runs is how often Rename is called; every call must end the same way.
	runs int
	// wantErr is matched with errors.Is; errAny accepts any error.
	wantErr error
	// keepsGate is whether tools/ must still be there afterwards.
	keepsGate bool
	// wantSource is a.go afterwards.
	wantSource string
	wantCalls  []string
}

// rename calls Rename as often as the case says and returns the last result
// with the commands of the last call.
func rename(t *testing.T, root string, c *renameCase) ([]string, error) {
	t.Helper()

	var (
		err  error
		fake *proctest.Fake
	)

	for range c.runs {
		fake = &proctest.Fake{Answers: c.answers}
		err = initmod.Rename(t.Context(), root, fake, c.next)
	}

	return fake.Calls(), err
}

// module writes a small module, with any extra files, to a scratch directory.
func module(t *testing.T, extra fstest.MapFS) string {
	t.Helper()

	root := t.TempDir()
	files := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/old\n")},
		"a.go":   {Data: []byte(source)},
		// The gate's own source, which a project must not keep.
		"tools/gate/main.go":      {Data: []byte("package main\n")},
		"tools/gate/main_test.go": {Data: []byte("package main\n")},
	}

	// An extra file of the same name replaces the default one.
	maps.Copy(files, extra)

	if err := os.CopyFS(root, files); err != nil {
		t.Fatalf("write module: %v", err)
	}

	return root
}

func TestRename(t *testing.T) {
	t.Parallel()

	tests := []renameCase{
		{
			name: "renames, pins the gate and drops its source", next: next, runs: 1,
			wantSource: renamed, wantCalls: []string{edit, pin, format},
		},
		{
			// go.mod is the fake's to edit, so it still names the old module, as
			// after a failure part way; a second run must finish cleanly.
			name: "can be run again", next: next, runs: 2,
			wantSource: renamed, wantCalls: []string{edit, pin, format},
		},
		{
			name: "carries on when the module is already renamed", next: next, runs: 1,
			extra:      fstest.MapFS{"go.mod": {Data: []byte("module " + next + "\n")}},
			wantSource: source, wantCalls: []string{pin, format},
		},
		{
			name: "rejects a path with spaces", next: "Bad Path", runs: 1,
			wantErr: initmod.ErrModule, wantSource: source, keepsGate: true,
		},
		{
			name: "rejects a path without a host", next: "shop", runs: 1,
			wantErr: initmod.ErrModule, wantSource: source, keepsGate: true,
		},
		{
			name: "rejects the template's own path", next: "github.com/latebit-io/go_project", runs: 1,
			wantErr: initmod.ErrModule, wantSource: source, keepsGate: true,
		},
		{
			name: "stops before go.mod on source that does not parse", next: next, runs: 1,
			extra:   fstest.MapFS{"z/broken.go": {Data: []byte("package")}},
			wantErr: errAny, wantSource: renamed, keepsGate: true,
		},
		{
			name: "reports a failing go mod edit", next: next, runs: 1, wantErr: errTool, wantSource: renamed,
			answers:   map[string]proctest.Answer{"go mod edit": {Err: errTool}},
			wantCalls: []string{edit}, keepsGate: true,
		},
		{
			// Without the pin there is no gate to fall back on, so its source stays.
			name: "keeps the gate's source when it cannot be pinned", next: next, runs: 1,
			answers: map[string]proctest.Answer{"go get": {Err: errTool}}, wantErr: errTool,
			wantSource: renamed, wantCalls: []string{edit, pin}, keepsGate: true,
		},
		{
			name: "reports a failing format", next: next, runs: 1, wantErr: errTool, wantSource: renamed,
			answers:   map[string]proctest.Answer{format: {Err: errTool}},
			wantCalls: []string{edit, pin, format},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := module(t, tt.extra)

			calls, err := rename(t, root, &tt)
			if !matches(err, tt.wantErr) {
				t.Fatalf("Rename() error = %v, want %v", err, tt.wantErr)
			}

			got, readErr := fs.ReadFile(os.DirFS(root), "a.go")
			if readErr != nil || string(got) != tt.wantSource {
				t.Fatalf("a.go after Rename() =\n%s\n%v\nwant\n%s", got, readErr, tt.wantSource)
			}

			if !slices.Equal(calls, tt.wantCalls) {
				t.Fatalf("Rename() ran %v, want %v", calls, tt.wantCalls)
			}

			_, statErr := fs.Stat(os.DirFS(root), "tools")
			if kept := statErr == nil; kept != tt.keepsGate {
				t.Fatalf("gate source kept = %v, want %v", kept, tt.keepsGate)
			}
		})
	}
}
