// Package srcwalk visits the Go files of a source tree as a reader sees it:
// every file, whatever its build tags or target platform.
package srcwalk

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// Files calls visit with the path and content of every .go file in tree, in
// lexical order. Dot, underscore, testdata and vendor directories are skipped.
func Files(tree fs.FS, visit func(name string, src []byte) error) error {
	err := fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("open %s: %w", name, err)
		}

		if entry.IsDir() && Skips(path.Base(name)) {
			return fs.SkipDir
		}

		if entry.IsDir() || path.Ext(name) != ".go" {
			return nil
		}

		src, err := fs.ReadFile(tree, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}

		return visit(name, src)
	})
	if err != nil {
		return fmt.Errorf("walk source tree: %w", err)
	}

	return nil
}

// Skips reports whether the walk leaves a directory of that name out, as the
// go tool leaves it out of ./... patterns.
func Skips(dir string) bool {
	hidden := dir != "." && (strings.HasPrefix(dir, ".") || strings.HasPrefix(dir, "_"))

	return hidden || dir == "testdata" || dir == "vendor"
}
