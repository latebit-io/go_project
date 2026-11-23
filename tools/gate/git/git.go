// Package git asks git about the repository the gate runs in.
package git

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/latebit-io/go_project/tools/gate/conv"
	"github.com/latebit-io/go_project/tools/gate/proc"
)

// Repo is the repository in the Runner's directory. Getenv reads the CI
// variables that name a pull request's branches.
type Repo struct {
	Run    proc.Runner
	Getenv func(string) string
}

// Branch is the pull request's branch in CI, else the checked-out one.
func (r Repo) Branch(ctx context.Context) (string, error) {
	if head := r.Getenv("GITHUB_HEAD_REF"); head != "" {
		return head, nil
	}

	return r.line(ctx, nil, "rev-parse", "--abbrev-ref", "HEAD")
}

// Head is the commit that is checked out.
func (r Repo) Head(ctx context.Context) (string, error) {
	return r.line(ctx, nil, "rev-parse", "HEAD")
}

// ForkPoint is the commit where this branch left its base.
func (r Repo) ForkPoint(ctx context.Context) (string, error) {
	base := cmp.Or(r.Getenv("GITHUB_BASE_REF"), conv.DefaultBranch)
	if _, err := r.line(ctx, nil, "rev-parse", "-q", "--verify", "origin/"+base); err == nil {
		base = "origin/" + base
	}

	return r.line(ctx, nil, "merge-base", base, "HEAD")
}

// Dirty reports uncommitted changes, untracked files included.
func (r Repo) Dirty(ctx context.Context) (bool, error) {
	status, err := r.line(ctx, nil, "status", "--porcelain")

	return status != "", err
}

// Changed lists the files under the pathspecs that differ from a commit,
// untracked files included. filter limits tracked changes like --diff-filter.
func (r Repo) Changed(ctx context.Context, since, filter string, pathspec ...string) ([]string, error) {
	diff := []string{"diff", "--name-only", "-z", "--diff-filter=" + filter, since, "--"}

	tracked, err := r.names(ctx, slices.Concat(diff, pathspec)...)
	if err != nil {
		return nil, err
	}

	others := []string{"ls-files", "-o", "-z", "--exclude-standard", "--"}

	untracked, err := r.names(ctx, slices.Concat(others, pathspec)...)

	return append(tracked, untracked...), err
}

// Subjects lists the commit subjects made since a commit, exactly as stored:
// a subject that starts with a space keeps it.
func (r Repo) Subjects(ctx context.Context, since string) ([]string, error) {
	return r.names(ctx, "log", "-z", "--format=%s", since+"..HEAD")
}

// Dir is the absolute path of the git directory.
func (r Repo) Dir(ctx context.Context) (string, error) {
	return r.line(ctx, nil, "rev-parse", "--absolute-git-dir")
}

// Index describes every staged entry, one per line.
func (r Repo) Index(ctx context.Context) (string, error) {
	return r.line(ctx, nil, "ls-files", "-s")
}

// HooksPath is where git looks for hooks, or "" for its default.
func (r Repo) HooksPath(ctx context.Context) string {
	// git exits non-zero when the setting is absent, which is an answer here.
	dir, err := r.line(ctx, nil, "config", "--get", "core.hooksPath")
	if err != nil {
		return ""
	}

	return dir
}

// SetHooksPath points git at the hooks directory committed with the repository.
func (r Repo) SetHooksPath(ctx context.Context, dir string) error {
	_, err := r.line(ctx, nil, "config", "core.hooksPath", dir)

	return err
}

// Snapshot adds the non-ignored files under the pathspec, tracked or not, to
// a scratch index and returns the id of the tree it then holds.
func (r Repo) Snapshot(ctx context.Context, index, pathspec string) (string, error) {
	env := []string{"GIT_INDEX_FILE=" + index}

	if _, err := r.line(ctx, env, "add", "-A", "--", pathspec); err != nil {
		return "", err
	}

	return r.line(ctx, env, "write-tree")
}

// Tracked lists every path in the real index: committed files and staged
// additions, also those .gitignore matches.
func (r Repo) Tracked(ctx context.Context) ([]string, error) {
	return r.names(ctx, "ls-files", "-z")
}

// Force adds the paths listed in a file, NUL-separated, to a scratch index
// even when .gitignore matches them. Each path must exist on disk.
func (r Repo) Force(ctx context.Context, index, list string) error {
	env := []string{"GIT_INDEX_FILE=" + index}
	// Literal: a file may be named like pathspec magic or a glob, such as ":(x).go".
	_, err := r.line(ctx, env, "--literal-pathspecs", "add", "-f", "--pathspec-from-file="+list, "--pathspec-file-nul")

	return err
}

// HasTree reports whether the id still names a tree; git prunes unreferenced ones.
func (r Repo) HasTree(ctx context.Context, id string) bool {
	if id == "" {
		return false
	}

	_, err := r.line(ctx, nil, "cat-file", "-e", id+"^{tree}")

	return err == nil
}

// ChangedLines counts lines added plus deleted under the pathspec between
// two trees or commits.
func (r Repo) ChangedLines(ctx context.Context, from, to, pathspec string) (int, error) {
	rows, err := r.lines(ctx, "diff", "--numstat", from, to, "--", pathspec)
	if err != nil {
		return 0, err
	}

	total := 0

	for _, row := range rows {
		// Binary files show "-" in both columns and count as no lines.
		for _, column := range strings.Fields(row)[:2] {
			if count, err := strconv.Atoi(column); err == nil {
				total += count
			}
		}
	}

	return total, nil
}

// raw returns git's output untouched, for callers that read binary data.
func (r Repo) raw(ctx context.Context, env []string, args ...string) (string, error) {
	out, err := r.Run.Output(ctx, env, "git", args...)
	if err != nil {
		return "", fmt.Errorf("git: %w", err)
	}

	return out, nil
}

func (r Repo) line(ctx context.Context, env []string, args ...string) (string, error) {
	out, err := r.raw(ctx, env, args...)

	return strings.TrimSpace(out), err
}

// names reads NUL-separated paths, which git prints unquoted with -z; in
// line output it quotes and escapes any name outside ASCII.
func (r Repo) names(ctx context.Context, args ...string) ([]string, error) {
	out, err := r.raw(ctx, nil, args...)
	if err != nil || out == "" {
		return nil, err
	}

	return strings.Split(strings.TrimSuffix(out, "\x00"), "\x00"), nil
}

func (r Repo) lines(ctx context.Context, args ...string) ([]string, error) {
	out, err := r.line(ctx, nil, args...)
	if err != nil || out == "" {
		return nil, err
	}

	return strings.Split(out, "\n"), nil
}
