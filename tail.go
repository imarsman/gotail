package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

var linePrinter *printer // A struct to handle printing lines

var useColour = true   // use colour - defaults to true
var usePolling = false // use polling - defaults to inotify
var followTrack bool   // follow renamed or replaced files

func init() {
	linePrinter = newPrinter()
}

// newPrinter get new printer instance properly instantiated
func newPrinter() *printer {
	p := new(printer)
	// setPath needs the rw mutex
	p.mu = new(sync.Mutex)
	// initialize to empty string
	p.setPath("")

	return p
}

// A printer is a central place for printing new lines.
type printer struct {
	currentPath string
	mu          *sync.Mutex
}

func (p *printer) setPath(path string) {
	p.currentPath = path
}

func (p *printer) getPath() string {
	return p.currentPath
}

// print print lines from a followed file
// An atomic value would not stop situations where headers were printed in a
// race condition even if the path was protected.
func (p *printer) print(path, line string) {
	// NOTE: Using a RWMutex will result in having path headers disconnected
	// from the lines they belong to. Need to lock more strictly here.
	p.mu.Lock()
	defer p.mu.Unlock()

	// If the current followed file's path is the same as the previous one used,
	// don't print out a header.
	if p.getPath() == path {
		fmt.Println(line)
		return
	}

	// Print out a header and set new value for the path.
	p.setPath(path)
	fmt.Println()
	fmt.Println(colour(brightBlue, fmt.Sprintf("==> %s <==", path)))
	fmt.Println(line)
}

// followedFile a file being tailed (followed)
// Uses the tail library which has undoubtedly taken many hours to get working
// well.
type followedFile struct {
	path string
	tail *tail.Tail
	ch   chan (int)
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

	// Use leaky bucket algorithm to rate limit output.
	// The setting used has not been tested.
	lb := ratelimiter.NewLeakyBucket(10, 1*time.Millisecond)

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
	ff.ch = make(chan (int))

	// Start the follow process as a go coroutine.
	go ff.followFile()

	return &ff, nil
}

// followFile follow a tailed file and call print when new lines come in. This
// is called from newFollowedFileForPath in a goroutine.
func (ff *followedFile) followFile() {
	// Wait for initial output to be done in main.
	<-ff.ch

	// Range over lines that come in, actually a channel of line structs
	for line := range ff.tail.Lines {
		linePrinter.print(ff.path, line.Text)
	}
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
	// handle a panic
	defer func() {
		if err := recover(); err != nil {
			fmt.Println(err)
		}
	}()

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
		out := os.Stderr
		fmt.Fprintln(out, colour(brightRed, "Can't use -H and -f together. Exiting with usage information."))
		printHelp(out)
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
			if !startAtOffset {
				strategyStr = "head"
			}
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
		// Don't add a newline
		fmt.Print(builder.String())
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

	var followedFiles = make([]*followedFile, 0, len(args))

	// Iterate through file path args
	for i := 0; i < len(args); i++ {
		// fmt.Println("file", args[i], "i", i)
		lines, total, err := getLines(args[i], headFlag, startAtOffset, numLines)
		if err != nil {
			// Handled by defer
			fmt.Fprintln(os.Stderr, err.Error())
			continue
		}
		if !headFlag && followFlag {
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
		ff.ch <- 1
	}

	// Wait to exit if files being followed
	if followFlag && !headFlag {
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)

		<-c
	}
}
