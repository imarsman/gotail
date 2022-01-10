package print

import (
	"fmt"
	"strings"

	"github.com/jwalton/gchalk"
)

const (
	brightGreen = iota
	brightYellow
	brightBlue
	brightRed
	noColour // Can use to default to no colour output
)

const (
	BrightGreen = iota
	BrightYellow
	BrightBlue
	BrightRed
	NoColour // Can use to default to no colour output
)

var useColour bool

// SetColour set whether or not to use colour output
func SetColour(use bool) {
	useColour = use
}

// OutputColour print in outputColour
func OutputColour(colour int, input ...string) string {
	str := fmt.Sprint(strings.Join(input, " "))
	str = strings.Replace(str, "  ", " ", -1)

	if !useColour {
		return str
	}

	// Choose colour for output or none
	switch colour {
	case brightGreen:
		return gchalk.BrightGreen(str)
	case brightYellow:
		return gchalk.BrightYellow(str)
	case brightBlue:
		return gchalk.BrightBlue(str)
	case brightRed:
		return gchalk.BrightRed(str)
	default:
		return str
	}
}
