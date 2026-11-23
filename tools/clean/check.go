package clean

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
)

// Finding is one flagged line or comment block.
type Finding struct {
	File string
	Line int
	Rule string
	Hint string
	Text string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s: %s: %q", f.File, f.Line, f.Rule, f.Hint, f.Text)
}

// Checker holds the compiled rules.
type Checker struct {
	rules []rule
}

// NewChecker compiles the rules once for many files.
func NewChecker() *Checker {
	return &Checker{rules: newRules()}
}

// Check returns the findings in one Go source file. Generated files are skipped.
func (c *Checker) Check(name string, src []byte) ([]Finding, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", name, err)
	}

	if ast.IsGenerated(file) {
		return nil, nil
	}

	found := emojiFindings(name, src)
	for _, group := range file.Comments {
		found = append(found, c.groupFindings(fset, name, group)...)
	}

	return found, nil
}

// groupFindings checks one comment block: every line against the patterns,
// and the block as a whole against the length cap.
func (c *Checker) groupFindings(fset *token.FileSet, name string, group *ast.CommentGroup) []Finding {
	var found []Finding

	total := 0

	for _, comment := range group.List {
		if isDirective(comment.Text) {
			continue
		}

		lines := commentLines(comment.Text)
		total += len(lines)
		first := fset.Position(comment.Pos()).Line

		for offset, line := range lines {
			if hit, flagged := c.match(line); flagged {
				found = append(found, Finding{File: name, Line: first + offset, Rule: hit.name, Hint: hit.hint, Text: line})
			}
		}
	}

	if total > maxCommentLines {
		found = append(found, Finding{
			File: name,
			Line: fset.Position(group.Pos()).Line,
			Rule: lengthRule,
			Hint: lengthHint,
			Text: fmt.Sprintf("%d comment lines, limit %d", total, maxCommentLines),
		})
	}

	return found
}

func (c *Checker) match(line string) (rule, bool) {
	for _, candidate := range c.rules {
		if candidate.pattern.MatchString(line) {
			return candidate, true
		}
	}

	return rule{}, false
}

// commentLines returns the prose of one comment, an element per source line.
func commentLines(text string) []string {
	if body, isLine := strings.CutPrefix(text, "//"); isLine {
		return []string{strings.TrimSpace(body)}
	}

	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(text, "/*"), "*/"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "*"))
	}

	return lines
}

func emojiFindings(name string, src []byte) []Finding {
	var found []Finding

	for i, line := range bytes.Split(src, []byte("\n")) {
		if bytes.ContainsFunc(line, isEmoji) {
			text := string(bytes.TrimSpace(line))
			found = append(found, Finding{File: name, Line: i + 1, Rule: emojiRule, Hint: emojiHint, Text: text})
		}
	}

	return found
}
