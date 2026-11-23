package arch_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/arch"
)

const goMod = "go.mod"

func source(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content)}
}

// One tree with every way a file can hide from the default build.
func hiddenFiles() fstest.MapFS {
	const domain = "internal/ordering/domain/"

	return fstest.MapFS{
		goMod:                  source("module \"example.com/shop\" // Deprecated: use v2\n\ngo 1.27\n"),
		domain + "order.go":    source("package domain\n\nimport \"errors\"\n"),
		domain + "order_it.go": source("//go:build integration\n\npackage domain\n\nimport \"net/http\"\n"),
		domain + "io_linux.go": source("package domain\n\nimport \"os\"\n"),
		domain + "sock.go":     source("//go:build unix\n\npackage domain\n"),
		// True on every supported platform, and still refused.
		domain + "version.go": source("//go:build !amd64.v4 || go1.20\n\npackage domain\n"),
		domain + "x_test.go":  source("package domain_test\n\nimport \"testing\"\n"),
		domain + "gen.go":     source("//go:build ignore\n\npackage main\n"),
		"pkg/tagged/only.go":  source("//go:build never\n\npackage tagged\n"),
	}
}

func TestLoadReadsEveryFile(t *testing.T) {
	t.Parallel()

	module, files, err := arch.Load(hiddenFiles())
	if err != nil || module != "example.com/shop" {
		t.Fatalf("Load() module = %q, error = %v", module, err)
	}

	got := make([]string, 0, len(files))
	for _, file := range files {
		row := []string{file.Name, file.Package, strings.Join(file.Imports, ","), file.Excluded}
		got = append(got, strings.Join(row, "|"))
	}

	const constrained = "has a build constraint"

	want := []string{
		"gen.go|main||" + constrained,
		"io_linux.go|domain|os|is left out of the darwin/arm64 build",
		"order.go|domain|errors|",
		"order_it.go|domain|net/http|" + constrained,
		"sock.go|domain||" + constrained,
		"version.go|domain||" + constrained,
		"x_test.go|domain|testing|",
		"only.go|tagged||" + constrained,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Load() files =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLoadedFilesAreChecked(t *testing.T) {
	t.Parallel()

	module, files, err := arch.Load(hiddenFiles())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	got := make([]string, 0, len(files))
	for _, v := range arch.Check(module, files) {
		got = append(got, v.String())
	}

	const missed = ", so lint and tests would miss it somewhere"

	want := []string{
		"internal/ordering/domain: build: gen.go has a build constraint" + missed,
		"internal/ordering/domain: build: io_linux.go is left out of the darwin/arm64 build" + missed,
		"internal/ordering/domain: build: order_it.go has a build constraint" + missed,
		"internal/ordering/domain: build: sock.go has a build constraint" + missed,
		"internal/ordering/domain: build: version.go has a build constraint" + missed,
		`internal/ordering/domain: import: domain may not import "net/http"`,
		`internal/ordering/domain: import: domain may not import "os"`,
		"internal/ordering/domain: naming: package main belongs under cmd/<name>",
		"pkg/tagged" + outsideLayout,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Check() =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestLoadFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tree fstest.MapFS
		want string
	}{
		{name: "no go.mod", tree: fstest.MapFS{}, want: "read go.mod"},
		{name: "no module directive", tree: fstest.MapFS{goMod: source("go 1.27\n")}, want: "module directive"},
		{name: "block form", tree: fstest.MapFS{goMod: source("module (\n\tm\n)\n")}, want: "module directive"},
		{
			name: "unparsable file",
			tree: fstest.MapFS{goMod: source("module m\n"), "cmd/x/main.go": source("package")},
			want: "parse cmd/x/main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, err := arch.Load(tt.tree)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}
