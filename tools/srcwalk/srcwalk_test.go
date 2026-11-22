package srcwalk_test

import (
	"errors"
	"io/fs"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/latebit-io/go_project/tools/srcwalk"
)

var errBroken = errors.New("disk on fire")

// errFS fails every open, like a tree that cannot be read.
type errFS struct{}

func (errFS) Open(string) (fs.File, error) { return nil, errBroken }

func file(content string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(content)}
}

func TestFilesVisitsGoFilesInOrder(t *testing.T) {
	t.Parallel()

	tree := fstest.MapFS{
		"b/b.go":          file("b"),
		"a/a_linux.go":    file("a"),
		"a/a_test.go":     file("t"),
		"README.md":       file("no"),
		"a/testdata/x.go": file("no"),
		"vendor/v/v.go":   file("no"),
		".cache/c.go":     file("no"),
		"_old/o.go":       file("no"),
	}

	var got []string

	err := srcwalk.Files(tree, func(name string, src []byte) error {
		got = append(got, name+"="+string(src))

		return nil
	})
	if err != nil {
		t.Fatalf("Files() error = %v", err)
	}

	want := []string{"a/a_linux.go=a", "a/a_test.go=t", "b/b.go=b"}
	if !slices.Equal(got, want) {
		t.Fatalf("Files() visited %v, want %v", got, want)
	}
}

func TestFilesStopsOnVisitError(t *testing.T) {
	t.Parallel()

	errStop := errors.New("stop")
	tree := fstest.MapFS{"a.go": file("a"), "b.go": file("b")}
	visits := 0

	err := srcwalk.Files(tree, func(string, []byte) error {
		visits++

		return errStop
	})
	if !errors.Is(err, errStop) || visits != 1 {
		t.Fatalf("Files() = %v after %d visits, want the visit error after 1", err, visits)
	}
}

func TestFilesReportsMissingRoot(t *testing.T) {
	t.Parallel()

	err := srcwalk.Files(errFS{}, func(string, []byte) error { return nil })
	if !errors.Is(err, errBroken) {
		t.Fatalf("Files() error = %v, want the open failure", err)
	}
}
