package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

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

var useColour = true         // use colour - defaults to true
var usePolling = false       // use polling - defaults to inotify
var followTrack bool = false // follow renamed or replaced files

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
func setrlimit(limit uint64) syscall.Rlimit {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Println("Error Getting Rlimit ", err)
	}
	rLimit.Cur = limit
	err = syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Println("Error Setting Rlimit ", err)
	}
	err = syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		fmt.Println("Error Getting Rlimit ", err)
	}

	return rLimit
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
func newFollowedFileForPath(path string) (*followedFile, error) {
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

	config := tail.Config{Follow: true, RateLimiter: lb, ReOpen: false, Poll: false, Location: &si}
	if followTrack {
		config.ReOpen = true
	}
	// For now allow a flag
	if usePolling == true {
		config.Poll = true
	}

	// fsnotify might not be as robust on Windows
	// if runtime.GOOS == "windows" {
	//	config.Poll = true
	// }

	tf, err := tail.TailFile(path, tail.Config{Follow: true, RateLimiter: lb, ReOpen: true, Location: &si})
	if err != nil {
		return nil, err
	}

	ff := followedFile{}
	ff.tail = tf
	ff.path = path

	// make channel to use to wait for initial lines to be tailed
	ff.ch = make(chan struct{})

	// Using anonymous function to avoid having this called separately
	go func() {
		// Wait for initial output to be done in main.
		<-ff.ch

		// Range over lines that come in, actually a channel of line structs
		for line := range ff.tail.Lines {
			linePrinter.print(ff.path, line.Text)
		}
	}()

	return &ff, nil
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
func getLines(path string, head, startAtOffset bool, linesWanted int) ([]string, int, error) {
	totalLines := 0

	// Declare here to ensure that defer works as it should
	var file *os.File

	// Define scanner that will be used either with a file or with stdin
	var scanner *bufio.Scanner

	// Use stdin if it is available. Path will be ignored.
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		scanner = bufio.NewScanner(os.Stdin)
	} else {
		var err error

		file, err = os.Open(path)
		if err != nil {
			// Something wrong like bad file path
			fmt.Fprintln(os.Stderr, err.Error())
			return nil, totalLines, err
		}

		// Deferring in case an error occurs
		defer file.Close()
		scanner = bufio.NewScanner(file)
	}

	// Use a slice the capacity of the number of lines wanted. In the case of
	// offset from head this will be less efficient as re-allocation will be done.
	var lines = make([]string, 0, linesWanted)

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

	return lines, totalLines, nil
}

// printHelp print out simple help output
func printHelp(out *os.File) {
	fmt.Fprintln(out, colour(brightGreen, os.Args[0], "- a simple tail program"))
	fmt.Fprintln(out, "Usage")
	fmt.Fprintln(out, "- print tail (or head) n lines of one or more files")
	fmt.Fprintln(out, "Example: tail -n 10 file1.txt file2.txt")
	// Prints to stdout
	flag.PrintDefaults()
	os.Exit(0)
}

func main() {
	var helpFlag bool
	flag.BoolVar(&helpFlag, "h", false, "print usage")

	var noColourFlag bool
	flag.BoolVar(&noColourFlag, "C", false, "no colour output")

	// Flag for whether to start tail partway into a file
	var startAtOffset bool

	flag.BoolVar(&usePolling, "P", false, "use polling instead of OS file system events (slower).")

	flag.BoolVar(&followTrack, "F", false, "follow new file lines and track file changes.")

	var followFlag bool
	flag.BoolVar(&followFlag, "f", false, "follow new file lines. No change tracking.")

	var numLines int
	var numLinesStr string
	flag.StringVar(&numLinesStr, "n", "10", "number of lines - prefix '+' for head to start at line n")

	var prettyFlag bool
	flag.BoolVar(&prettyFlag, "p", false, "print extra formatting to output if more than one file is listed")

	var printLinesFlag bool
	flag.BoolVar(&printLinesFlag, "N", false, "show line numbers")

	var headFlag bool
	flag.BoolVar(&headFlag, "H", false, "print head of file rather than tail")

	flag.Parse()

	if noColourFlag {
		useColour = false
	}

	// Track file changes. Set follow to true as well. followTrack is used
	// elsewhere in the package where the tail process is set up.
	if followTrack {
		followFlag = true
	}

	if headFlag && followFlag {
		followFlag = false
	}

	if helpFlag == true {
		out := os.Stdout
		printHelp(out)
	}

	justDigits, err := regexp.MatchString(`^[0-9]+$`, numLinesStr)
	if err != nil {
		out := os.Stderr
		fmt.Fprintln(out, colour(brightRed, "Got error", err.Error()))
		printHelp(out)
	}
	if justDigits == false {
		// Test for + prefix. Complain later if something else is wrong
		if !strings.HasPrefix(numLinesStr, "+") {
			out := os.Stderr
			fmt.Fprintln(out, colour(brightRed, "Invalid -n value", numLinesStr, ". Exiting with usage information."))
			printHelp(out)
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
			printHelp(out)
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
			printHelp(out)
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

	// args are interpreted as paths
	args := flag.Args()

	// For printing out file information when > 1 file being processed
	multipleFiles = len(args) > 1 // Are multiple files to be printed

	if len(args) == 0 {
		out := os.Stderr
		fmt.Fprintln(out, colour(brightRed, "No files specified. Exiting with usage information."))
		printHelp(out)
	}

	// var followedFiles = make([]*followedFile, 0, len(args))

	// Guard against handling too many files
	if len(args) > int(rlimit) {
		fmt.Fprintf(os.Stderr, "Too many files specified. Max is %d\n", rlimit)
		os.Exit(1)
	}

	// Iterate through file path args
	for i := 0; i < len(args); i++ {
		// fmt.Println("file", args[i], "i", i)
		lines, total, err := getLines(args[i], headFlag, startAtOffset, numLines)
		if err != nil {
			// there was a problem such as a ban file path
			continue
		}

		if !headFlag && followFlag {
			// followFlag = false
			ff, err := newFollowedFileForPath(args[i])
			followedFiles = append(followedFiles, ff)
			if err != nil {
				panic(err)
			}
		}

		// This is what the tail command does - leave a space before file name
		if i > 0 && len(args) > 1 {
			fmt.Println()
		}
		write(args[i], headFlag, lines, total)
	}

	// Write to channel for each followed file to release them to follow.
	for _, ff := range followedFiles {
		ff.ch <- *new(struct{})
	}

	// Wait to exit if files being followed
	if followFlag && !headFlag {
		// fmt.Printf("active files %+v", activeFiles)
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)

		<-c
	}
}
