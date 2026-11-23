package srcwalk_test

import (
	"errors"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/srcwalk"
)

var (
	errBroken = errors.New("disk on fire")
	errStop   = errors.New("stop")
)

// errFS fails every open, like a tree that cannot be read.
type errFS struct{}

func (errFS) Open(string) (fs.File, error) { return nil, errBroken }

func file(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content)}
}

func TestFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		tree     fs.FS
		visitErr error
		want     []string
		wantErr  error
	}{
		{
			name: "go files in lexical order, skipped directories left out",
			tree: fstest.MapFS{
				"b/b.go":          file("b"),
				"a/a_linux.go":    file("a"),
				"a/a_test.go":     file("t"),
				"README.md":       file("no"),
				"a/testdata/x.go": file("no"),
				"vendor/v/v.go":   file("no"),
				".cache/c.go":     file("no"),
				"_old/o.go":       file("no"),
			},
			want: []string{"a/a_linux.go=a", "a/a_test.go=t", "b/b.go=b"},
		},
		{
			name:     "a visit error stops the walk",
			tree:     fstest.MapFS{"a.go": file("a"), "b.go": file("b")},
			visitErr: errStop,
			want:     []string{"a.go=a"},
			wantErr:  errStop,
		},
		{name: "an unreadable tree is reported", tree: errFS{}, wantErr: errBroken},
		{name: "an empty tree visits nothing", tree: fstest.MapFS{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got []string

			err := srcwalk.Files(tt.tree, func(name string, src []byte) error {
				got = append(got, name+"="+string(src))

				return tt.visitErr
			})
			if !errors.Is(err, tt.wantErr) || !slices.Equal(got, tt.want) {
				t.Fatalf("Files() visited %v, error %v, want %v, error %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestSkips(t *testing.T) {
	t.Parallel()

	tests := map[string]bool{
		".": false, "internal": false, "cmd": false,
		"testdata": true, "vendor": true, ".git": true, "_old": true,
	}

	for dir, want := range tests {
		t.Run(dir, func(t *testing.T) {
			t.Parallel()

			if got := srcwalk.Skips(dir); got != want {
				t.Fatalf("Skips(%q) = %v, want %v", dir, got, want)
			}
		})
	}
}
