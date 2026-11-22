// Package initmod renames the module after "Use this template".
package initmod

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/latebit-io/go_project/tools/arch"
	"github.com/latebit-io/go_project/tools/gate/proc"
	"github.com/latebit-io/go_project/tools/srcwalk"
)

// ErrModule marks a module path that cannot be used.
var ErrModule = errors.New("module path")

const fileMode = 0o644

// Rename rewrites go.mod and every Go import in the module rooted at root
// from the current module path to next, then reformats.
func Rename(ctx context.Context, root string, run proc.Runner, next string) error {
	if !regexp.MustCompile(`^[a-z0-9.-]+(/[A-Za-z0-9._~-]+)+$`).MatchString(next) {
		return fmt.Errorf("%w: %q does not look like github.com/acme/shop", ErrModule, next)
	}

	files, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("open module: %w", err)
	}

	renameErr := rewrite(ctx, files, run, next)

	return errors.Join(renameErr, files.Close())
}

func rewrite(ctx context.Context, files *os.Root, run proc.Runner, next string) error {
	current, err := arch.ModulePath(files.FS())
	if err != nil {
		return fmt.Errorf("read module: %w", err)
	}

	if next == current {
		return fmt.Errorf("%w: the module is already %s", ErrModule, current)
	}

	_, err = run.Output(ctx, nil, "go", "mod", "edit", "-module", next)
	if err != nil {
		return fmt.Errorf("edit go.mod: %w", err)
	}

	err = srcwalk.Files(files.FS(), func(name string, src []byte) error {
		rewritten := rewriteImports(src, current, next)
		if bytes.Equal(rewritten, src) {
			return nil
		}

		return files.WriteFile(name, rewritten, fileMode)
	})
	if err != nil {
		return fmt.Errorf("rewrite imports: %w", err)
	}

	// The new path can sort differently among the imports.
	err = run.Stream(ctx, nil, "golangci-lint", "fmt")
	if err != nil {
		return fmt.Errorf("format: %w", err)
	}

	return nil
}

// rewriteImports replaces the module path where it opens a quoted import
// path, so prose that mentions the old path is left alone.
func rewriteImports(src []byte, current, next string) []byte {
	src = bytes.ReplaceAll(src, []byte(`"`+current+`/`), []byte(`"`+next+`/`))

	return bytes.ReplaceAll(src, []byte(`"`+current+`"`), []byte(`"`+next+`"`))
}
