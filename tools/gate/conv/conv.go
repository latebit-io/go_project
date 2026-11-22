// Package conv holds the conventional-commit rules for branch names and
// commit subjects.
package conv

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// DefaultBranch is where finished work lands; nothing is developed on it.
const DefaultBranch = "main"

// MaxWords caps the description of a commit subject; the body is free.
const MaxWords = 5

// ErrConvention marks a branch name or commit subject that breaks the rules.
var ErrConvention = errors.New("convention")

// types lists the conventional commit types, shared by branches and subjects.
func types() []string {
	return []string{"feat", "fix", "docs", "style", "refactor", "perf", "test", "build", "ci", "chore", "revert"}
}

// untestedTypes are the branch types that promise unchanged behavior.
func untestedtypes() []string {
	return []string{"refactor", "chore", "docs", "style", "ci", "build", "revert"}
}

// Unmanaged reports branches no naming or history rule applies to: the
// default branch, a detached HEAD and dependabot's own branches.
func Unmanaged(branch string) bool {
	return branch == DefaultBranch || branch == "HEAD" || strings.HasPrefix(branch, "dependabot/")
}

// NeedsTest reports whether Go changes on the branch must come with a test.
func NeedsTest(branch string) bool {
	kind, _, _ := strings.Cut(branch, "/")

	return !Unmanaged(branch) && !slices.Contains(untestedtypes(), kind)
}

// CheckBranch accepts <type>/<slug> with a conventional type and a kebab-case slug.
func CheckBranch(branch string) error {
	pattern := regexp.MustCompile(`^(` + strings.Join(types(), "|") + `)/[a-z0-9]+(-[a-z0-9]+)*$`)
	if Unmanaged(branch) || pattern.MatchString(branch) {
		return nil
	}

	return fmt.Errorf("%w: branch %q is not <type>/<slug>, e.g. feat/order-export; types: %s",
		ErrConvention, branch, strings.Join(types(), " "))
}

// CheckSubject accepts <type>(<scope>): <description> with a description of
// at most five words, lowercase start, no final period. Merge commits pass.
func CheckSubject(subject string) error {
	if strings.HasPrefix(subject, "Merge ") {
		return nil
	}

	pattern := regexp.MustCompile(`^(` + strings.Join(types(), "|") + `)(\([a-z0-9-]+\))?!?: (.+)$`)

	match := pattern.FindStringSubmatch(subject)
	if match == nil {
		return fmt.Errorf("%w: %q is not <type>(<scope>): <description>; types: %s",
			ErrConvention, subject, strings.Join(types(), " "))
	}

	description := match[len(match)-1]
	if words := len(strings.Fields(description)); words > MaxWords {
		return fmt.Errorf("%w: %q has a %d-word description, limit %d; put the rest in the body",
			ErrConvention, subject, words, MaxWords)
	}

	first := description[0]
	lowerStart := (first >= 'a' && first <= 'z') || (first >= '0' && first <= '9')

	if !lowerStart || strings.HasSuffix(description, ".") {
		return fmt.Errorf("%w: %q: the description starts lowercase and has no final period", ErrConvention, subject)
	}

	return nil
}
