package args

import (
	"fmt"
	"strings"

	"github.com/alexflint/go-arg"
)

// GitCommit for use when compiling
var GitCommit string

// GitLastTag for use when compiling
var GitLastTag string

// GitExactTag for use when compiling
var GitExactTag string

// Date for use when compiling
var Date string

// args to use with go-args
type args struct {
	NoColour    bool     `arg:"-C" help:"no colour"`
	Follow      bool     `arg:"-f" help:"follow new file lines."`
	NumLines    string   `arg:"-n" default:"10" help:"number of lines - prefix '+' for head to start at line n"`
	PrintExtra  bool     `arg:"-p" help:"print extra formatting to output if more than one file is listed"`
	LineNumbers bool     `arg:"-N" help:"show line numbers"`
	JSON        bool     `arg:"-j" help:"pretty print JSON"`
	JSONOnly    bool     `arg:"-J,--json-only" help:"ignore non-JSON and process JSON"`
	Match       string   `arg:"-m,--match" help:"match lines by regex"`
	Head        bool     `arg:"-H" help:"print head of file rather than tail"`
	Interval    uint     `arg:"-i" help:"seconds between new file checks" default:"1"`
	Files       []string `arg:"-f,--files" help:"files to tail"`
}

func (args) Description() string {
	return `This is an implementation of the tail utility. File patterns can be specified
with one or more final arguments or as glob patterns with one or more -G parameters.
If files are followed for new data the glob file list will be checked every 
interval seconds. Initiate completion by running COMP_INSTALL=1 gotail
`
}

func leftjust(s string, n int, fill string) string {
	return s + strings.Repeat(fill, n)
}

func (args) Version() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("commit: %8s\n", GitCommit))
	sb.WriteString(fmt.Sprintf("tag: %10s\n", GitExactTag))
	sb.WriteString(fmt.Sprintf("date: %23s\n", Date))

	return sb.String()
}

// Args incoming arguments
var Args args

func init() {
	// Start off by gathering arguments
	arg.MustParse(&Args)
	if Args.JSONOnly {
		Args.JSON = true
	}
}
