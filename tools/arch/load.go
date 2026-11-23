package arch

import (
	"errors"
	"fmt"
	"go/build"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"path"
	"strconv"
	"strings"

	"github.com/latebit-io/go_project/tools/srcwalk"
)

var errNoModule = errors.New("go.mod has no single-line module directive")

// Load reads the module path and every Go file of a source tree, in path
// order. No build tag or platform suffix hides a file from it.
func Load(tree fs.FS) (string, []File, error) {
	module, err := ModulePath(tree)
	if err != nil {
		return "", nil, err
	}

	var files []File

	err = srcwalk.Files(tree, func(name string, src []byte) error {
		file, fileErr := parseFile(name, src)
		if fileErr != nil {
			return fileErr
		}

		// A constraint already settled it; otherwise the file name may still exclude it.
		if file.Excluded == "" {
			file.Excluded, fileErr = unbuilt(tree, name)
		}

		files = append(files, file)

		return fileErr
	})
	if err != nil {
		return "", nil, fmt.Errorf("load files: %w", err)
	}

	return module, files, nil
}

func parseFile(name string, src []byte) (File, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ImportsOnly|parser.ParseComments)
	if err != nil {
		return File{}, fmt.Errorf("parse %s: %w", name, err)
	}

	file := File{
		Dir:     path.Dir(name),
		Name:    path.Base(name),
		Package: strings.TrimSuffix(parsed.Name.Name, "_test"),
	}

	// Any constraint is refused, not only one that fails here: which tags and
	// CPU features a build sets cannot be known from one machine.
	for _, group := range parsed.Comments {
		for _, comment := range group.List {
			if constraint.IsGoBuild(comment.Text) || constraint.IsPlusBuild(comment.Text) {
				file.Excluded = "has a build constraint"
			}
		}
	}

	for _, spec := range parsed.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			return File{}, fmt.Errorf("import path in %s: %w", name, err)
		}

		file.Imports = append(file.Imports, imported)
	}

	return file, nil
}

// unbuilt says which supported platform leaves the file out because of its
// name, such as a _linux.go suffix, or "" when all take it.
func unbuilt(tree fs.FS, name string) (string, error) {
	for _, platform := range Platforms() {
		ctx := build.Default
		ctx.GOOS, ctx.GOARCH, _ = strings.Cut(platform, "/")
		ctx.CgoEnabled, ctx.BuildTags, ctx.JoinPath = true, nil, path.Join
		ctx.OpenFile = func(file string) (io.ReadCloser, error) {
			opened, err := tree.Open(file)
			if err != nil {
				return nil, fmt.Errorf("open %s: %w", file, err)
			}

			return opened, nil
		}

		match, err := ctx.MatchFile(path.Dir(name), path.Base(name))
		if err != nil {
			return "", fmt.Errorf("match %s: %w", name, err)
		}

		if !match {
			return "is left out of the " + platform + " build", nil
		}
	}

	return "", nil
}

// ModulePath reads the module directive of go.mod as text. A form it cannot
// read is an error: a wrong path would make every in-module import look external.
func ModulePath(tree fs.FS) (string, error) {
	data, err := fs.ReadFile(tree, "go.mod")
	if err != nil {
		return "", fmt.Errorf("read go.mod: %w", err)
	}

	for line := range strings.Lines(string(data)) {
		directive, _, _ := strings.Cut(line, "//")
		spaced := strings.Join(strings.Fields(directive), " ")

		name, found := strings.CutPrefix(spaced, "module ")
		if found && !strings.ContainsAny(name, " (") {
			return strings.Trim(name, `"`), nil
		}
	}

	return "", errNoModule
}
