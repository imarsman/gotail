package util

import (
	"regexp"

	"github.com/imarsman/gotail/cmd/internal/args"
)

func init() {
	if args.Args.Match != "" {
		lineMatchRegexp = regexp.MustCompile(args.Args.Match)
	} else {
		lineMatchRegexp = regexp.MustCompile(`.*`)
	}
}

var lineMatchRegexp *regexp.Regexp

// CheckMatch check if line is a match to regexp
func CheckMatch(input string) bool {
	if args.Args.Match == `` {
		return true
	}
	return lineMatchRegexp.Match([]byte(input))
}

// Pluralize produce sigular or plural output depending on number value
var Pluralize = func(singular, plural string, number int) string {
	if number == 1 {
		return singular
	}
	return plural
}
