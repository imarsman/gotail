package args

import (
	"github.com/alexflint/go-arg"
)

// args to use with go-args
type args struct {
	NoColour    bool   `arg:"-C" help:"no colour"`
	Follow      bool   `arg:"-f" help:"follow new file lines."`
	NumLines    string `arg:"-n" default:"10" help:"number of lines - prefix '+' for head to start at line n"`
	PrintExtra  bool   `arg:"-p" help:"print extra formatting to output if more than one file is listed"`
	LineNumbers bool   `arg:"-N" help:"show line numbers"`
	JSON        bool   `arg:"-j" help:"pretty print JSON"`
	// AllLines    bool     `arg:"-a" help:"show all lines"`
	Head     bool     `arg:"-H" help:"print head of file rather than tail"`
	Glob     []string `arg:"-G,separate" help:"quoted filesystem glob patterns - will find new files"`
	Interval uint     `arg:"-i" help:"seconds between new file checks" default:"1"`
	Files    []string `arg:"positional" help:"files to tail"`
}

func (args) Description() string {
	return `This is an implementation of the tail utility. File patterns can be specified
with one or more final arguments or as glob patterns with one or more -G parameters.
If files are followed for new data the glob file list will be checked every interval
seconds.
`
}

// Args incoming arguments
var Args args

func init() {
	// Start off by gathering arguments
	arg.MustParse(&Args)
}
