package initmod_test

import (
	"bytes"
	"cmp"
	"errors"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
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
	goMod  = "go.mod"

	// What init reports, a line as each step is done: the middle one only for
	// a copy that still held the template's files, the last one on success.
	pinned   = "ok: module " + next + ", gate pinned in go.mod\n"
	shed     = "ok: README.md started, tools/ removed\n"
	nextStep = "next: go tool gate setup\n"

	templateReadme = "# go_project\n"
	projectReadme  = "# shop\n"

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

// convertCase is one way of calling Convert and what must come of it.
type convertCase struct {
	name    string
	extra   fstest.MapFS
	answers map[string]proctest.Answer
	next    string
	// runs is how often Convert is called; every call must end the same way.
	runs int
	// wantErr is matched with errors.Is; errAny accepts any error.
	wantErr error
	// keepsTools is whether tools/ must still be there afterwards.
	keepsTools bool
	// wantSource is a.go afterwards.
	wantSource string
	wantCalls  []string
	// wantReadme is how README.md starts afterwards; empty means the template's.
	wantReadme string
	// wantOut is everything the last call reported.
	wantOut string
}

// outcome is what the last call of Convert returned, ran and reported.
type outcome struct {
	err   error
	calls []string
	out   string
}

// convert calls Convert as often as the case says and returns what the last
// call did.
func convert(t *testing.T, root string, c *convertCase) outcome {
	t.Helper()

	var last outcome

	for range c.runs {
		var out bytes.Buffer

		fake := &proctest.Fake{Answers: c.answers}
		err := initmod.Project{Root: root, Run: fake, Out: &out}.Convert(t.Context(), c.next)
		last = outcome{err: err, calls: fake.Calls(), out: out.String()}
	}

	return last
}

// check fails the test unless the call and the files it left are as the case says.
func (c *convertCase) check(t *testing.T, root string, got outcome) {
	t.Helper()

	if !matches(got.err, c.wantErr) || got.out != c.wantOut || !slices.Equal(got.calls, c.wantCalls) {
		t.Fatalf("Convert() = %+v, want error %v, output %q, calls %v", got, c.wantErr, c.wantOut, c.wantCalls)
	}

	files := os.DirFS(root)

	source, err := fs.ReadFile(files, "a.go")
	if err != nil || string(source) != c.wantSource {
		t.Fatalf("a.go after Convert() =\n%s\n%v\nwant\n%s", source, err, c.wantSource)
	}

	wantReadme := cmp.Or(c.wantReadme, templateReadme)

	readme, err := fs.ReadFile(files, "README.md")
	if err != nil || !strings.HasPrefix(string(readme), wantReadme) || strings.Contains(string(readme), "{{") {
		t.Fatalf("README.md after Convert() =\n%s\n%v\nwant it to start %q, placeholders filled", readme, err, wantReadme)
	}

	_, statErr := fs.Stat(files, "tools")
	if kept := statErr == nil; kept != c.keepsTools {
		t.Fatalf("tools/ kept = %v, want %v", kept, c.keepsTools)
	}
}

func file(content string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(content)} }

// module writes a small module, with any extra files, to a scratch directory.
func module(t *testing.T, extra fstest.MapFS) string {
	t.Helper()

	root := t.TempDir()
	files := fstest.MapFS{
		goMod:       file("module example.com/old\n"),
		"a.go":      file(source),
		"README.md": file(templateReadme + "\nUse as a template.\n"),
		// The gate's own source, which a project must not keep.
		"tools/gate/main.go":      file("package main\n"),
		"tools/gate/main_test.go": file("package main\n"),
	}

	// An extra file of the same name replaces the default one; nil removes it.
	maps.Copy(files, extra)
	maps.DeleteFunc(files, func(_ string, removed *fstest.MapFile) bool { return removed == nil })

	if err := os.CopyFS(root, files); err != nil {
		t.Fatalf("write module: %v", err)
	}

	return root
}

func TestConvert(t *testing.T) {
	t.Parallel()

	tests := []convertCase{
		{
			name: "renames, pins the gate, starts a README and drops the gate's source", next: next, runs: 1,
			wantSource: renamed, wantCalls: []string{edit, pin, format},
			wantReadme: projectReadme, wantOut: pinned + shed + nextStep,
		},
		{
			// go.mod is the fake's to edit, so it still names the old module, as
			// after a failure part way; a second run must finish cleanly.
			name: "can be run again", next: next, runs: 2,
			wantSource: renamed, wantCalls: []string{edit, pin, format},
			wantReadme: projectReadme, wantOut: pinned + nextStep,
		},
		{
			name: "carries on when the module is already renamed", next: next, runs: 1,
			extra:      fstest.MapFS{goMod: file("module " + next + "\n")},
			wantSource: source, wantCalls: []string{pin, format},
			wantReadme: projectReadme, wantOut: pinned + shed + nextStep,
		},
		{
			// Without the gate's source the copy was converted before: the README
			// and the tools/ directory are the project's own.
			name: "leaves the README and tools of a converted project alone", next: next, runs: 1,
			extra: fstest.MapFS{
				goMod:                file("module " + next + "\n"),
				"README.md":          file("# mine\n"),
				"tools/gate/main.go": nil, "tools/gate/main_test.go": nil,
				"tools/own/main.go": file("package main\n"),
			},
			wantSource: source, wantCalls: []string{pin, format}, keepsTools: true,
			wantReadme: "# mine\n", wantOut: pinned + nextStep,
		},
		{
			name: "rejects a path with spaces", next: "Bad Path", runs: 1,
			wantErr: initmod.ErrModule, wantSource: source, keepsTools: true,
		},
		{
			name: "rejects a path without a host", next: "shop", runs: 1,
			wantErr: initmod.ErrModule, wantSource: source, keepsTools: true,
		},
		{
			name: "rejects the template's own path", next: "github.com/latebit-io/go_project", runs: 1,
			wantErr: initmod.ErrModule, wantSource: source, keepsTools: true,
		},
		{
			name: "stops before go.mod on source that does not parse", next: next, runs: 1,
			extra:   fstest.MapFS{"z/broken.go": file("package")},
			wantErr: errAny, wantSource: renamed, keepsTools: true,
		},
		{
			name: "reports a failing go mod edit", next: next, runs: 1, wantErr: errTool, wantSource: renamed,
			answers:   map[string]proctest.Answer{"go mod edit": {Err: errTool}},
			wantCalls: []string{edit}, keepsTools: true,
		},
		{
			// Without the pin there is no gate to fall back on, so its source stays.
			name: "keeps the gate's source and README when it cannot be pinned", next: next, runs: 1,
			answers: map[string]proctest.Answer{"go get": {Err: errTool}}, wantErr: errTool,
			wantSource: renamed, wantCalls: []string{edit, pin}, keepsTools: true,
		},
		{
			name: "reports a failing format", next: next, runs: 1, wantErr: errTool, wantSource: renamed,
			answers:   map[string]proctest.Answer{format: {Err: errTool}},
			wantCalls: []string{edit, pin, format}, wantReadme: projectReadme, wantOut: pinned + shed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := module(t, tt.extra)

			tt.check(t, root, convert(t, root, &tt))
		})
	}
}
