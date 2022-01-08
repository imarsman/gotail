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
	"sync"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/jwalton/gchalk"
	"github.com/nxadm/tail"
	"github.com/nxadm/tail/ratelimiter"
)

const (
	brightGreen = iota
	brightYellow
	brightBlue
	brightRed
	noColour // Can use to default to no colour output
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
	$: time cat /var/log/wifi-08-31-2021__09:26:50.999.log | tail -n +100 >/dev/null
	real	0m0.011s

	// This tail
	$: time cat /var/log/wifi-08-31-2021__09:26:50.999.log | gotail -H -n +100 >/dev/null
	real	0m0.006s

	There is likely more to do in terms of directing output properly to stdout
	or stderr.
*/

var printerOnce sync.Once                         // used to ensure printer instantiated only once
var linePrinter *printer                          // A struct to handle printing lines
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

// type rl struct {
// 	Cur uint64
// 	Max uint64
// }

func callSetRLimit(limit uint64) (err error) {
	return
}

func init() {
	// We'll always get the same instance from newPrinter.
	linePrinter = newPrinter()

	rlimit = 1000

	// Set files limit
	setrlimit(rlimit)
}

// a message to be sent when following a file
type msg struct {
	path string
	line string
}

// A printer is a central place for printing new lines.
type printer struct {
	currentPath string
	messages    chan (msg)
}

// newPrinter get new printer instance properly instantiated
// Use package level linePrinter to enforce singleton pattern, as that is the
// needed pattern at this point.
func newPrinter() *printer {
	if linePrinter != nil {
		return linePrinter
	}
	// Ensure linePrinter is set up only once
	printerOnce.Do(func() {
		linePrinter = new(printer)
	})

	// initialize to empty string
	linePrinter.setPath("")
	linePrinter.messages = make(chan (msg))

	// Print messages in goroutine to avoid exposing messages channel which has
	// its own locking behaviour. Use of a channel avoids worries about race
	// condition with incoming path compared to printer path. Previous code
	// tried atomic values for path and a mutex instead of a channel.
	go func() {
		for m := range linePrinter.messages {
			if linePrinter.getPath() == m.path {
				fmt.Println(m.line)
				continue
			}
			// Print out a header and set new value for the path.
			linePrinter.setPath(m.path)
			fmt.Println()
			fmt.Println(colour(brightBlue, fmt.Sprintf("==> %s <==", m.path)))
			fmt.Println(m.line)
		}
	}()

	return linePrinter
}

func (p *printer) setPath(path string) {
	p.currentPath = path
}

func (p *printer) getPath() string {
	return p.currentPath
}

// print print lines from a followed file.
// An anonymous function is started in newPrinter to handle additions to the
// message channel.
func (p *printer) print(path, line string) {
	m := msg{path: path, line: line}
	p.messages <- m
}

// followedFile a file being tailed (followed).
// Uses the tail library which has undoubtedly taken many hours to get working
// well.
type followedFile struct {
	path string
	tail *tail.Tail
	ch   chan struct{}
}

// newFollowedFileForPath create a new file that will start tailing
func newFollowedFileForPath(path string) (followed *followedFile, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// get the length of the file im bytes for SeekInfo.
	size := fi.Size()
	// Set seek location in bytes, with reference to start of file.
	si := tail.SeekInfo{Offset: size, Whence: 0}

	// Use leaky bucket algorithm to rate limit output. Implemented by tail
	// package. The size is the bucket capacity before rate limiting begins.
	// After that, the leak interval kicks in. If the size is too small a spurt
	// of new lines will cause the tail package to cease tailing for a period of
	// time. Initially the size was set to 10 and that was insufficient.
	lb := ratelimiter.NewLeakyBucket(1000, 1*time.Millisecond)

	tf, err := tail.TailFile(path, tail.Config{Follow: true, RateLimiter: lb, ReOpen: true, Location: &si})
	if err != nil {
		return
	}

	followed = &followedFile{}
	followed.tail = tf
	followed.path = path

	// make channel to use to wait for initial lines to be tailed
	followed.ch = make(chan struct{})

	// Using anonymous function to avoid having this called separately
	go func() {
		// Wait for initial output to be done in main.
		<-followed.ch

		// Range over lines that come in, actually a channel of line structs
		for line := range followed.tail.Lines {
			linePrinter.print(followed.path, line.Text)
		}
	}()

	return
}

func colour(colour int, input ...string) string {
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

	// Set follow flag to false if this is a file head call
	// This is relied upon later
	if headFlag && followFlag {
		followFlag = false
	}

	justDigits, err := regexp.MatchString(`^[0-9]+$`, numLinesStr)
	if err != nil {
		out := os.Stderr
		fmt.Fprintln(out, colour(brightRed, "Got error", err.Error()))
		os.Exit(1)
	}
	if justDigits == false {
		// Test for + prefix. Complain later if something else is wrong
		if !strings.HasPrefix(numLinesStr, "+") {
			out := os.Stderr
			fmt.Fprintln(out, colour(brightRed, "Invalid -n value", numLinesStr, ". Exiting with usage information."))
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
			fmt.Fprintln(out, colour(brightRed, "Invalid -n value", nStrOrig, ". Exiting with usage information."))
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
			fmt.Fprintln(out, colour(brightRed, "invalid -n value", numLinesStr, ". Exiting with usage information."))
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
	// all at once.
	var write = func(path string, head bool, lines []string, linesAvailable int) {
		builder := new(strings.Builder)

		strategyStr := "tail"
		if head {
			strategyStr = "head"
		}

		// Skips for single file and stdin
		if prettyFlag == true && multipleFiles {
			builder.WriteString(colour(brightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
		}

		// head is also true
		if startAtOffset {
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(colour(brightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", linesAvailable), linesAvailable)))
			} else {
				// The tail utility prints out filenames if there is more than one
				// file. Do so here as well.
				if multipleFiles {
					extent := len(lines) + numLines - 1
					builder.WriteString(colour(brightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", linesAvailable), extent)))
				}
			}
		} else {
			// The tail utility prints out filenames if there is more than one
			// file. Do so here as well.

			// No lines in file
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(colour(brightBlue, fmt.Sprintf("==> %s - %s of %d %s <==\n", path, strategyStr, len(lines), pluralize("line", "lines", len(lines)))))
			} else {
				// With multiple files print out filename, etc. otherwise leave empty.
				if multipleFiles {
					if startAtOffset {
						builder.WriteString(colour(brightBlue, fmt.Sprintf("==> %s - starting at %d of %d %s <==\n", path, numLines, linesAvailable, pluralize("line", "lines", linesAvailable))))
					} else {
						if head {
							count := numLines
							if numLines > linesAvailable {
								count = linesAvailable
							}
							builder.WriteString(colour(brightBlue, fmt.Sprintf("==> %s - head %d of %d %s <==\n", path, count, linesAvailable, pluralize("line", "lines", linesAvailable))))
						} else {
							count := numLines
							if numLines > linesAvailable {
								count = linesAvailable
							}
							builder.WriteString(colour(brightBlue, fmt.Sprintf("==> %s - tail %d of %d %s <==\n", path, count, linesAvailable, pluralize("line", "lines", linesAvailable))))
						}
					}
				}
			}
		}
		if prettyFlag == true && multipleFiles {
			builder.WriteString(colour(brightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
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
		fmt.Fprintln(out, colour(brightRed, "No files specified. Exiting with usage information."))
		os.Exit(1)
	}

	// Guard against handling too many files
	if len(files) > int(rlimit) {
		fmt.Fprintf(os.Stderr, "Too many files specified. Max is %d\n", rlimit)
		os.Exit(1)
	}

	// Iterate through file path args
	for i := 0; i < len(files); i++ {
		// fmt.Println("file", args[i], "i", i)
		lines, total, err := getLines(files[i], headFlag, startAtOffset, numLines)
		if err != nil {
			// there was a problem such as a ban file path
			continue
		}

		if followFlag {
			ff, err := newFollowedFileForPath(files[i])
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
		ff.ch <- *new(struct{})
	}

	// Wait to exit if files being followed
	if followFlag {
		// fmt.Printf("active files %+v", activeFiles)
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)

		<-c
	}
}
