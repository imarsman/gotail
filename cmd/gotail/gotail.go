package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"

	"github.com/alexflint/go-arg"
	"github.com/imarsman/gotail/cmd/gotail/print"
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
	real	0m0.011s

	// This tail
	$: time cat /var/log/wifi.log | gotail -H -n +100 >/dev/null
	real	0m0.006s

	There is likely more to do in terms of directing output properly to stdout
	or stderr.
*/

var followedFiles = make([]*followedFile, 0, 100) // initialize followed files here

var useColour = true   // use colour - defaults to true
var usePolling = false // use polling - defaults to inotify
var followFlag bool    // follow renamed or replaced files

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

	Before the use of a channel for the printer a mutex was used. The channel
	does what a mutex would do and has the benefit of allowing a simple message
	with path and line to be sent in the channel.
*/

func callSetRLimit(limit uint64) (err error) {
	return
}

func init() {
	rlimit = 1000

	// Set files limit
	setrlimit(rlimit)
}

// getLines get linesWanted lines or start gathering lines at linesWanted if
// head is true and startAtOffset is true. Return lines as a string slice.
// Return an error if for instance a filename is incorrect.
func getLines(path string, head, startAtOffset bool, linesWanted int) (lines []string, totalLines int, err error) {
	// Declare here to ensure that defer works as it should
	var file *os.File

	// Define scanner that will be used either with a file or with stdin
	var scanner *bufio.Scanner

	// Use stdin if it is available. Path will be ignored.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		scanner = bufio.NewScanner(os.Stdin)
	} else {
		file, err = os.Open(path)
		if err != nil {
			// Something wrong like bad file path
			fmt.Fprintln(os.Stderr, err.Error())
			return
		}

		// Deferring in case an error occurs
		defer file.Close()
		scanner = bufio.NewScanner(file)
	}

	// Use a slice the capacity of the number of lines wanted. In the case of
	// offset from head this will be less efficient as re-allocation will be done.
	lines = make([]string, 0, linesWanted)

	// Tell scanner to scan by lines.
	scanner.Split(bufio.ScanLines)

	// Get head lines and return. Easiest option as we don't need to use slice
	// tricks to get last lines.
	if head {
		// Handle starting at offset, get lines, then return
		if startAtOffset {
			totalLines = 1
			for scanner.Scan() {
				// Add to lines slice when in range
				if totalLines >= linesWanted {
					lines = append(lines, scanner.Text())
				}
				totalLines++
			}
			// scanner keeps track of non-EOF error
			if scanner.Err() != nil {
				return []string{}, totalLines, scanner.Err()
			}

			return lines, totalLines, nil
		}
		// not starting at offset so get head lines
		totalLines = 0
		for scanner.Scan() {
			// Add to lines slice when in range
			if totalLines < linesWanted {
				lines = append(lines, scanner.Text())
			}
			totalLines++
		}
		// scanner keeps track of non-EOF error
		if scanner.Err() != nil {
			return []string{}, totalLines, scanner.Err()
		}

		return lines, totalLines, nil
	}

	// Get tail lines and return
	totalLines = 0
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		totalLines++
		// Add to lines slice when in range
		if totalLines > linesWanted {
			// Get rid of the first element to keep this a "last" slice
			lines = lines[1:]
		}
	}
	// scanner keeps track of non-EOF error
	if scanner.Err() != nil {
		return []string{}, totalLines, scanner.Err()
	}

	return
}

// args to use with go-args
var args struct {
	NoColour    bool     `arg:"-C" help:"no colour"`
	Polling     bool     `arg:"-P" help:"polling - use file polling instead of inotify"`
	FollowFlag  bool     `arg:"-f" help:"follow new file lines."`
	NumLinesStr string   `arg:"-n" default:"10" help:"number of lines - prefix '+' for head to start at line n"`
	PrintExtra  bool     `arg:"-p" help:"print extra formatting to output if more than one file is listed"`
	LineNumbers bool     `arg:"-N" help:"show line numbers"`
	Head        bool     `arg:"-H" help:"print head of file rather than tail"`
	Files       []string `arg:"positional" help:"files to tail"`
}

func main() {
	// Start off by gathering various parameters

	arg.MustParse(&args)

	if args.NumLinesStr == "" {
		args.NumLinesStr = "10"
	}

	var noColourFlag = args.NoColour

	// Flag for whether to start tail partway into a file
	var startAtOffset bool

	usePolling = args.Polling // consider removing and only supporting OS follow
	followFlag = args.FollowFlag

	var numLinesStr = args.NumLinesStr
	var numLines int
	var prettyFlag = args.PrintExtra
	var printLinesFlag = args.LineNumbers
	var headFlag = args.Head

	if noColourFlag {
		useColour = false
	}
	print.SetColour(useColour)

	// Set follow flag to false if this is a file head call
	// This is relied upon later
	if headFlag && followFlag {
		followFlag = false
	}

	justDigits, err := regexp.MatchString(`^[0-9]+$`, numLinesStr)
	if err != nil {
		out := os.Stderr
		fmt.Fprintln(out, print.OutputColour(print.BrightRed, "Got error", err.Error()))
		os.Exit(1)
	}
	if justDigits == false {
		// Test for + prefix. Complain later if something else is wrong
		if !strings.HasPrefix(numLinesStr, "+") {
			out := os.Stderr
			fmt.Fprintln(out, print.OutputColour(print.BrightRed, "Invalid -n value", numLinesStr, ". Exiting with usage information."))
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
			fmt.Fprintln(out, print.OutputColour(print.BrightRed, "Invalid -n value", nStrOrig, ". Exiting with usage information."))
			os.Exit(1)
		}
		// Assume head if we got an offset
		headFlag = true
		startAtOffset = true
	} else {
		var err error
		// Extremely unlikely to have error as we've checked for all digits
		numLines, err = strconv.Atoi(numLinesStr)
		if err != nil {
			out := os.Stderr
			fmt.Fprintln(out, print.OutputColour(print.BrightRed, "invalid -n value", numLinesStr, ". Exiting with usage information."))
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
		if prettyFlag == true && multipleFiles {
			builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
		}

		// head is also true
		if startAtOffset {
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", linesAvailable), linesAvailable)))
			} else {
				// The tail utility prints out filenames if there is more than one
				// file. Do so here as well.
				if multipleFiles {
					extent := len(lines) + numLines - 1
					builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", linesAvailable), extent)))
				}
			}
		} else {
			// The tail utility prints out filenames if there is more than one
			// file. Do so here as well.

			// No lines in file
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("==> %s - %s of %d %s <==\n", path, strategyStr, len(lines), pluralize("line", "lines", len(lines)))))
			} else {
				// With multiple files print out filename, etc. otherwise leave empty.
				if multipleFiles {
					if startAtOffset {
						builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("==> %s - starting at %d of %d %s <==\n", path, numLines, linesAvailable, pluralize("line", "lines", linesAvailable))))
					} else {
						if head {
							count := numLines
							if numLines > linesAvailable {
								count = linesAvailable
							}
							builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("==> %s - head %d of %d %s <==\n", path, count, linesAvailable, pluralize("line", "lines", linesAvailable))))
						} else {
							count := numLines
							if numLines > linesAvailable {
								count = linesAvailable
							}
							builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("==> %s - tail %d of %d %s <==\n", path, count, linesAvailable, pluralize("line", "lines", linesAvailable))))
						}
					}
				}
			}
		}
		if prettyFlag == true && multipleFiles {
			builder.WriteString(print.OutputColour(print.BrightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
		}

		index := 0
		// Print out all lines for file using string builder.
		for i := 0; i < len(lines); i++ {
			if printLinesFlag == true {
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
		lines, total, err := getLines("", headFlag, startAtOffset, numLines)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			return
		}

		// write to stdout
		write("", headFlag, lines, total)
		os.Exit(0)
	}

	var files = args.Files

	// For printing out file information when > 1 file being processed
	multipleFiles = len(files) > 1 // Are multiple files to be printed

	if len(files) == 0 {
		out := os.Stderr
		fmt.Fprintln(out, print.OutputColour(print.BrightRed, "No files specified. Exiting with usage information."))
		os.Exit(1)
	}

	// Guard against handling too many files
	if len(files) > int(rlimit) {
		fmt.Fprintf(os.Stderr, "Too many files specified. Max is %d\n", rlimit)
		os.Exit(1)
	}

	// Iterate through file path args and for each get then print out lines
	for i := 0; i < len(files); i++ {
		// fmt.Println("file", args[i], "i", i)
		lines, total, err := getLines(files[i], headFlag, startAtOffset, numLines)
		if err != nil {
			// there was a problem such as a ban file path
			continue
		}

		if followFlag {
			ff, err := newFollowedFileForPath(files[i]) // define followed file
			followedFiles = append(followedFiles, ff)
			if err != nil {
				panic(err)
			}
		}

		// This is what the tail command does - leave a space before file name
		if i > 0 && len(files) > 1 {
			fmt.Println()
		}
		write(files[i], headFlag, lines, total)
	}

	// Write to channel for each followed file to release them to follow.
	for _, ff := range followedFiles {
		ff.unlock()
	}

	// Wait to exit if files being followed
	if followFlag {
		// fmt.Printf("active files %+v", activeFiles)
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)

		<-c
	}
}
