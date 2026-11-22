// Package coverage reads a Go cover profile.
package coverage

import (
	"strconv"
	"strings"
)

const (
	percent = 100
	// fieldsPerBlock is the shape of a profile line: block, statements, count.
	fieldsPerBlock = 3
)

// Percent returns the share of statements a profile covers, leaving out
// files whose import path starts with exclude. ok is false with no statements.
func Percent(profile, exclude string) (float64, bool) {
	// A block listed twice, by two test binaries, counts once: covered if any run hit it.
	covered := map[string]bool{}
	sizes := map[string]int{}

	for line := range strings.Lines(profile) {
		fields := strings.Fields(line)
		if len(fields) != fieldsPerBlock || strings.HasPrefix(line, exclude) {
			continue
		}

		statements, sizeErr := strconv.Atoi(fields[1])
		count, countErr := strconv.Atoi(fields[2])

		if sizeErr != nil || countErr != nil {
			continue
		}

		sizes[fields[0]] = statements
		covered[fields[0]] = covered[fields[0]] || count > 0
	}

	total, hit := 0, 0

	for block, statements := range sizes {
		total += statements

		if covered[block] {
			hit += statements
		}
	}

	if total == 0 {
		return 0, false
	}

	return percent * float64(hit) / float64(total), true
}
