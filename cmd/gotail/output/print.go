package output

import (
	"fmt"
	"strings"

	"github.com/jwalton/gchalk"
)

const (
	// BrightGreen bright green output colour
	BrightGreen = iota
	// BrightYellow bright yellow output colour
	BrightYellow
	// BrightBlue bright blue output colour
	BrightBlue
	// BrightRed bright red output colour
	BrightRed
	// NoColour no output colour
	NoColour // Can use to default to no colour output
)

var useColour bool

// SetColour set whether or not to use colour output
func SetColour(use bool) {
	useColour = use
}

// Colour print in outputColour
func Colour(colour int, input ...string) string {
	str := fmt.Sprint(strings.Join(input, " "))
	str = strings.Replace(str, "  ", " ", -1)

	if !useColour {
		return str
	}

	// Choose colour for output or none
	switch colour {
	case BrightGreen:
		return gchalk.BrightGreen(str)
	case BrightYellow:
		return gchalk.BrightYellow(str)
	case BrightBlue:
		return gchalk.BrightBlue(str)
	case BrightRed:
		return gchalk.BrightRed(str)
	default:
		return str
	}
}
