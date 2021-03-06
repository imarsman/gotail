package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/imarsman/gotail/cmd/gotail/input"
	"github.com/imarsman/gotail/cmd/gotail/output"
)

/*
	This app takes a number of lines argument, a "pretty" argument for more
	illustrative output, and a list of paths to files, and for each file gathers
	the number of lines requested from the tail or the head of the file's lines,
	if available, and then prints them out to standard out.

	This app can print the head lines for a file starting at an offset.

	This app can also follow files as they are added to.

	The native Unix implementation of tail is much smaller and uses less
	resources. This is mostly a test but it seems to work well so far.

	// Regular tail
	$: time cat /var/log/wifi.log | tail -n +100 >/dev/null
	tail -n +100 > /dev/null  0.01s user 0.00s system 83% cpu 0.010 total

	// This tail
	$: time cat /var/log/wifi.log | gotail -H -n +100 >/dev/null
	gotail -H -n +100 > /dev/null  0.00s user 0.00s system 70% cpu 0.011 total

	There is likely more to do in terms of directing output properly to stdout
	or stderr.
*/

var useColour = true // use colour - defaults to true
var follow bool      // follow renamed or replaced files
// initialize followed files here - used to keep track of files being followed
// so that they can have things done such as unlocking their channels.
var followedFiles = make([]*output.FollowedFile, 0, 100)

var rlimit uint64

/*
	The soft limit is the value that the kernel enforces for the corresponding
	resource. The hard limit acts as a ceiling for the soft limit: an unprivileged
	process may only set its soft limit to a value in the range from 0 up to the
	hard limit, and (irreversibly) lower its hard limit. A privileged process (under
	Linux: one with the CAP_SYS_RESOURCE capability) may make arbitrary changes to
	either limit value.

	Note:
	When testing the hard limit on MacOS was 9223372036854775807

	Output:
	There are two modes of output in this app. The first mode is a file-by file
	output of lines to standard output. In this condition, each file to have
	lines printed is processed and its output is printed to stdout iteratively.
	In the second condition with the follow option selected, new lines for each
	file are received by the tail package (nxadm/tail) and sent to a common
	printer struct instance (common to the package) with the file path and the
	line as arguments. If the file path is the same as the last one used no file
	path header is added. Otherwise the path of the file is sent to stdout and
	then the new line. Queuing for this is handled by a channel in the printer
	struct.
*/

func callSetRLimit(limit uint64) (err error) {
	return
}

func init() {
	rlimit = 1000

	// Set files limit
	setrlimit(rlimit)
}

// args to use with go-args
type args struct {
	NoColour    bool     `arg:"-C" help:"no colour"`
	Follow      bool     `arg:"-f" help:"follow new file lines."`
	NumLinesStr string   `arg:"-n" default:"10" help:"number of lines - prefix '+' for head to start at line n"`
	PrintExtra  bool     `arg:"-p" help:"print extra formatting to output if more than one file is listed"`
	LineNumbers bool     `arg:"-N" help:"show line numbers"`
	Head        bool     `arg:"-H" help:"print head of file rather than tail"`
	Glob        []string `arg:"-G,separate" help:"quoted filesystem glob patterns - will find new files"`
	Interval    int      `arg:"-i" help:"seconds between new file checks" default:"1"`
	Files       []string `arg:"positional" help:"files to tail"`
}

func (args) Description() string {
	return `This is an implementation of the tail utility. File patterns can be specified
with one or more final arguments or as glob patterns with one or more -G parameters.
If files are followed for new data the glob file list will be checked every interval
seconds.
`
}

// expandGlobs - take a list of glob patterns and get the complete expanded list,
// adding this to the incoming list. The code makes an attempt to normalize paths.
func expandGlobs(globs []string, existing []string) (expanded []string, err error) {
	// make filter map
	var found = map[string]bool{}

	// add in existing items and mark them as present
	expanded = append(expanded, existing...)
	for _, path := range expanded {
		full, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		path = filepath.Clean(full)
		found[path] = true
	}

	for _, g := range globs {
		var files []string
		files, err = filepath.Glob(g)
		if err != nil {
			continue
		}
		for _, path := range files {
			full, err := filepath.Abs(path)
			if err != nil {
				continue
			}
			path = filepath.Clean(full)
			if !found[path] {
				expanded = append(expanded, path)
				found[path] = true
			}
		}
	}

	return
}

func main() {
	// Start off by gathering arguments
	var args args
	arg.MustParse(&args)

	// Set re-check interval and ensure it is not zero
	interval := args.Interval
	if interval == 0 {
		interval = 1
	}

	var noColourFlag = args.NoColour

	if args.NumLinesStr == "" {
		args.NumLinesStr = "10"
	}

	// Flag for whether to start tail partway into a file
	var startAtOffset bool

	follow = args.Follow

	var numLinesStr = args.NumLinesStr
	var numLines int
	var pretty = args.PrintExtra
	var printLines = args.LineNumbers
	var head = args.Head

	if noColourFlag {
		useColour = false
	}
	output.SetColour(useColour) // Set colour output for the run of this app

	// Set follow flag to false if this is a file head call
	// This is relied upon later
	if head && follow {
		follow = false
	}

	justDigits, err := regexp.MatchString(`^[0-9]+$`, numLinesStr)
	if err != nil {
		out := os.Stderr
		fmt.Fprintln(out, output.Colour(output.BrightRed, "Got error", err.Error()))
		os.Exit(1)
	}
	if justDigits == false {
		// Test for + prefix. Complain later if something else is wrong
		if !strings.HasPrefix(numLinesStr, "+") {
			out := os.Stderr
			fmt.Fprintln(out, output.Colour(output.BrightRed, "Invalid -n value", numLinesStr, ". Exiting with usage information."))
			os.Exit(1)
		}
	}

	// Deal selectively with offset
	if !justDigits {
		nStrOrig := numLinesStr
		numLinesStr = numLinesStr[1:]
		// Ignore prefix if not a head request
		var err error
		// Invalid  somehow - for example +20a is not caught above but would be invalid
		numLines, err = strconv.Atoi(numLinesStr)
		if err != nil {
			out := os.Stderr
			fmt.Fprintln(out, output.Colour(output.BrightRed, "Invalid -n value", nStrOrig, ". Exiting with usage information."))
			os.Exit(1)
		}
		// Assume head if we got an offset
		head = true
		startAtOffset = true
	} else {
		var err error
		// Extremely unlikely to have error as we've checked for all digits
		numLines, err = strconv.Atoi(numLinesStr)
		if err != nil {
			out := os.Stderr
			fmt.Fprintln(out, output.Colour(output.BrightRed, "invalid -n value", numLinesStr, ". Exiting with usage information."))
			os.Exit(1)
		}
	}

	var multipleFiles bool

	var pluralize = func(singular, plural string, number int) string {
		if number == 1 {
			return singular
		}
		return plural
	}

	// Write lines for a single file to avoid growing large output then dumping
	// all at once. The lines to print are passed in.
	var write = func(path string, head bool, lines []string, linesAvailable int) {
		builder := new(strings.Builder)

		strategyStr := "tail"
		if head {
			strategyStr = "head"
		}

		// Skips for single file and stdin
		if pretty == true && multipleFiles {
			builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
		}

		// head is also true
		if startAtOffset {
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", linesAvailable), linesAvailable)))
			} else {
				// The tail utility prints out filenames if there is more than one
				// file. Do so here as well.
				if multipleFiles {
					extent := len(lines) + numLines - 1
					builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", linesAvailable), extent)))
				}
			}
		} else {
			// The tail utility prints out filenames if there is more than one
			// file. Do so here as well.

			// No lines in file
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("==> %s - %s of %d %s <==\n", path, strategyStr, len(lines), pluralize("line", "lines", len(lines)))))
			} else {
				// With multiple files print out filename, etc. otherwise leave empty.
				if multipleFiles {
					if startAtOffset {
						builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("==> %s - starting at %d of %d %s <==\n", path, numLines, linesAvailable, pluralize("line", "lines", linesAvailable))))
					} else {
						if head {
							count := numLines
							if numLines > linesAvailable {
								count = linesAvailable
							}
							builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("==> %s - head %d of %d %s <==\n", path, count, linesAvailable, pluralize("line", "lines", linesAvailable))))
						} else {
							count := numLines
							if numLines > linesAvailable {
								count = linesAvailable
							}
							builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("==> %s - tail %d of %d %s <==\n", path, count, linesAvailable, pluralize("line", "lines", linesAvailable))))
						}
					}
				}
			}
		}
		if pretty == true && multipleFiles {
			builder.WriteString(output.Colour(output.BrightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
		}

		index := 0
		// Print out all lines for file using string builder.
		for i := 0; i < len(lines); i++ {
			if printLines == true {
				if startAtOffset {
					index = i + numLines
				} else {
					index = i + 1
				}
				builder.WriteString(fmt.Sprintf("%-3d %s\n", index, lines[i]))
			} else {
				if lines[i] == "" {
					// Add newline for empty string
					builder.WriteString("\n")
				} else {
					builder.WriteString(fmt.Sprintf("%s\n", lines[i]))
				}
			}
		}

		// Write out what was recieved with no added newline
		io.WriteString(os.Stdout, builder.String())
	}

	// Use stdin if available
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		lines, total, err := input.GetLines("", head, startAtOffset, numLines)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return
		}

		// write to stdout
		write("", head, lines, total)
		os.Exit(0)
	}

	files, err := expandGlobs(args.Glob, args.Files)
	if err != nil {
		panic(err)
	}

	// For printing out file information when > 1 file being processed
	multipleFiles = len(files) > 1 // Are multiple files to be printed

	if len(files) == 0 {
		out := os.Stderr
		fmt.Fprintln(out, output.Colour(output.BrightRed, "No files specified. Exiting with usage information."))
		os.Exit(1)
	}

	// Guard against handling too many files
	if len(files) > int(rlimit) {
		fmt.Fprintf(os.Stderr, "Too many files specified. Max is %d\n", rlimit)
		os.Exit(1)
	}

	var filesFollowed = map[string]bool{}

	// runFiles run through file list and for any new files and when follow is true, add
	// the files to the set of followed files.
	var runFiles = func(files []string) {
		// make empty set of followed files
		var newFollowedFiles = make([]*output.FollowedFile, 0, 100)

		foundNew := false
		// Iterate through file path args and for each get then print out lines
		for i := 0; i < len(files); i++ {
			path, err := filepath.Abs(files[i])
			if err != nil {
				continue
			}

			if filesFollowed[path] {
				continue
			}

			foundNew = true
			filesFollowed[path] = true

			lines, total, err := input.GetLines(files[i], head, startAtOffset, numLines)
			if err != nil {
				// there was a problem such as a bad file path
				continue
			}

			if follow {
				ff, err := output.NewFollowedFileForPath(files[i]) // define followed file
				// unlikely given that non-existent filess would be caught above
				if err != nil {
					continue
				}
				// Add to comprehensive list of followed files
				followedFiles = append(followedFiles, ff)
				// Add to list of new files found to follow
				newFollowedFiles = append(newFollowedFiles, ff)
			}

			// This is what the tail command does - leave a space before file name
			if i > 0 && len(files) > 1 {
				fmt.Println()
			}
			write(files[i], head, lines, total)
		}

		if foundNew {
			// Write to channel for each followed file to release them to
			// follow. Only do so if the file is being encountered for the first
			// time.
			for _, ff := range newFollowedFiles {
				ff.Unlock()
			}
		}
	}

	// Just run the files specified if following isn't being requested
	if !follow {
		runFiles(files)
	} else {
		// Follow periodically if follow specified
		// Code will exit below if follow is set
		go func() {
			// If there were glob arguments check for new ever few seconds
			if len(args.Glob) > 0 {
				for {
					files, err = expandGlobs(args.Glob, args.Files)
					if err != nil {
						panic(err)
					}
					runFiles(files)
					time.Sleep(time.Duration(interval) * time.Second)
				}
			} else {
				// If no glob patterns don't bother checking ever interval seconds
				runFiles(files)
				return
			}
		}()
	}

	// Wait to exit if files being followed
	if follow {
		// fmt.Printf("active files %+v", activeFiles)
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)

		<-c
	}
}
