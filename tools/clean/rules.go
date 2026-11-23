// Package clean keeps comments to what a reader of the code needs: the why,
// in plain words, within three lines, and no emoji anywhere in the source.
package clean

import (
	"regexp"
	"strings"
)

// maxCommentLines caps one comment block; longer reasoning belongs in docs/.
const maxCommentLines = 3

const (
	lengthRule = "length"
	lengthHint = "move the reasoning to docs/"
	emojiRule  = "emoji"
	emojiHint  = "no emoji in source"
)

type rule struct {
	name    string
	hint    string
	pattern *regexp.Regexp
}

// newRules lists the comment patterns, one rule per kind. Each pattern is
// narrow on purpose: a rule that flags honest comments gets switched off.
func newRules() []rule {
	kind := func(name, hint string, exprs ...string) rule {
		joined := `(?i)(?:` + strings.Join(exprs, `)|(?:`) + `)`

		return rule{name: name, hint: hint, pattern: regexp.MustCompile(joined)}
	}

	return []rule{
		kind("narration", "say why, not what",
			`^(loop|iterate) (through|over)\b`,
			`^check (if|whether)\b`,
			`^(initialize|create a new|return the result|helper function)\b`,
			`^(this (function|method)|(function|method) to)\b`),
		kind("changelog", "history belongs in the commit message",
			`^(added|updated|changed|fixed|removed|refactored) (to|for|the|a|an|this|in|from)\b`,
			`\b(as requested|as per (your|the) (request|instructions?))\b`,
			`\bper (the )?(review|feedback|request)\b`),
		kind("placeholder", "finish it or delete it",
			`\bin a real (implementation|application|app|system|world)\b`,
			`\bin production,? (you|we|this) (would|should|will)\b`,
			`\bfor (demonstration|demo purposes|simplicity|now)\b`,
			`\b(simplified (version|implementation)|not (yet )?implemented)\b`,
			`\bplaceholder (for|implementation|value|logic)\b`,
			`\b(implementation (goes )?here|your (code|logic) here|existing code)\b`,
			`\brest of (the )?(code|implementation|file|function)\b`),
		kind("chat", "written for a chat, not for a reader of the code",
			`\b(here's|here is|here we|now we|let's|feel free|happy to|hope this)\b`,
			`\b(first|next|finally),? we\b`,
			`\b(i've|i'll|i have|as an ai|worth noting)\b`,
			`^(note|important|warning|caution|remember)\s*[:!]`),
		kind("constraint", "enforce it with a test, a type or a lint rule",
			`\b(do not|don't|never) (remove|delete|change|modify|touch|edit|reorder) (this|these|the (line|order))\b`,
			`\b(this|these|the (line|order)).* must (not|never) be (removed|deleted|changed|modified|reordered)\b`,
			`\bkeep (this |these |it )?in sync\b`),
		kind("filler", "drop the filler", `\b(simply|basically|essentially|obviously)\b`),
		kind("typography", "use ASCII punctuation",
			`[\x{2013}\x{2014}\x{2018}\x{2019}\x{201C}\x{201D}\x{2026}\x{2190}\x{2192}\x{21D2}]`),
	}
}

// isDirective reports machine-read comments such as //go:build and //nolint:
// a line comment with no space after the slashes. They are exempt from every rule.
func isDirective(text string) bool {
	body, isLine := strings.CutPrefix(text, "//")

	return isLine && body != "" && body[0] != ' ' && body[0] != '\t'
}

// isEmoji covers the pictograph blocks, dingbats, the clock and media symbols
// that render as emoji, and the selector that turns a plain symbol into one.
func isEmoji(r rune) bool {
	const variationSelector = 0xFE0F

	spans := [...][2]rune{
		{0x1F000, 0x1FAFF},
		{0x2600, 0x27BF},
		{0x2B00, 0x2BFF},
		{0x231A, 0x231B},
		{0x23E9, 0x23FA},
		{0x25FD, 0x25FE},
	}
	for _, span := range spans {
		if r >= span[0] && r <= span[1] {
			return true
		}
	}

	return r == variationSelector
}
