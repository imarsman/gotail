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
	"sync/atomic"
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

	This app can also follow files as they are added to.

	The native Unix implementation of tail is much smaller and uses less
	resources. This is mostly a test.

	One thing that could be added is to take in data from stdin.

	// Regular tail
	$: time cat /var/log/wifi-08-31-2021__09:26:50.999.log | tail -n +100 >/dev/null
	real    0m0.308s

	// This tail
	$: time cat /var/log/wifi-08-31-2021__09:26:50.999.log | ./tail -H -n +100 >/dev/null
	real    0m0.048
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
	p.currentPath = new(atomic.Value) // initialize atomic value
	p.setPath("")                     // Fails if not initialized

	return p
}

// This could just be an atomic.Value but probably that's too restricted.
type printer struct {
	currentPath *atomic.Value
}

func (p *printer) setPath(path string) {
	p.currentPath.Store(path)
}

func (p *printer) getPath() string {
	return p.currentPath.Load().(string)
}

func colourOutput(colour int, input ...string) string {
	str := fmt.Sprint(strings.Join(input, " "))
	str = strings.Replace(str, "  ", " ", -1)

	if !useColour {
		return str
	}

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

// print print lines from a followed file
func (p *printer) print(path, line string) {
	if p.getPath() == path {
		fmt.Println(line)
	} else {
		p.setPath(path)
		fmt.Println()
		fmt.Println(colourOutput(brightBlue, fmt.Sprintf("==> %s <==", path)))
		fmt.Println(line)
	}
}

// followedFile a file being tailed (followed)
type followedFile struct {
	path string
	tail *tail.Tail
	wg   *sync.WaitGroup
}

// followFile follow a tailed file and call print when new lines come in
func (ff *followedFile) followFile() {
	// Wait for initial output to be done in main.
	ff.wg.Wait()
	// Use inotify or whatever the tail package used decides.
	for line := range ff.tail.Lines {
		// the printer makes sure to set the proper path heading as appropriate.
		linePrinter.print(ff.path, line.Text)
	}
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
	// }

	tf, err := tail.TailFile(path, tail.Config{Follow: true, RateLimiter: lb, ReOpen: true, Location: &si})
	if err != nil {
		return nil, err
	}

	ff := followedFile{}
	ff.tail = tf
	ff.path = path
	ff.wg = new(sync.WaitGroup)
	ff.wg.Add(1)

	// Start the follow process as a go coroutine.
	// Initially the follow waits for initial file to be finished for all files
	// in main.
	go ff.followFile()

	return &ff, nil
}

// getLines get lasn num lines in file and return them as a string slice. Return
// an error if for instance a filename is incorrect.
func getLines(path string, head, startAtOffset bool, num int) ([]string, int, error) {
	totalLines := 0

	// Declare here to ensure that defer works as it should
	var file *os.File

	// Define scanner that will be used either with a file or with stdin
	var scanner *bufio.Scanner

	// Use stdin if it is available
	// path will be ignored.
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

	// A bit inefficient as whole file is read in then out again in reverse
	// order up to num.
	// Since we will have to get the last items we have to read lines lines in
	// then shorten the output. Other algorithms would involve avoiding reading
	// lines the contents in by using a buffer or counting lines or some other
	// technique.
	var lines = make([]string, 0, num*2)

	// Use reader to count lines but discard what is not needed.
	scanner.Split(bufio.ScanLines)

	// Get head lines and return
	// Count all lines but only load what is requested into slice.
	if head {
		if startAtOffset {
			totalLines = 1
			for scanner.Scan() {
				if totalLines >= num {
					lines = append(lines, scanner.Text())
				}
				totalLines++
			}
			if scanner.Err() != nil {
				return []string{}, totalLines, scanner.Err()
			}
			return lines, totalLines, nil
		}
		totalLines = 0
		for scanner.Scan() {
			if totalLines < num {
				lines = append(lines, scanner.Text())
			}
			totalLines++
		}
		if scanner.Err() != nil {
			return []string{}, totalLines, scanner.Err()
		}
		return lines, totalLines, nil
	}

	// Get tail lines and return
	totalLines = 0
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		// If we have more than we need, remove first element
		if totalLines >= num {
			// Get rid of the first element to keep this a "last" slice
			lines = lines[1:]
		}
		totalLines++
	}
	if scanner.Err() != nil {
		return []string{}, totalLines, scanner.Err()
	}

	return lines, totalLines, nil
}

// printHelp print out simple help output
func printHelp(out *os.File) {
	fmt.Fprintln(out, colourOutput(brightGreen, os.Args[0], "- a simple tail program"))
	fmt.Fprintln(out, "Usage")
	fmt.Fprintln(out, "- print tail (or head) n lines of one or more files")
	fmt.Fprintln(out, "Example: tail -n 10 file1.txt file2.txt")
	flag.PrintDefaults()
	os.Exit(0)
}

// Option for following files that seems to be cross platform
// https://github.com/nxadm/tail

func main() {
	var helpFlag bool
	// Help flag
	flag.BoolVar(&helpFlag, "h", false, "print usage")

	var noColourFlag bool
	flag.BoolVar(&noColourFlag, "C", false, "no colour output")

	// Flag for whetehr to start tail partway into a file
	var startAtOffset bool

	flag.BoolVar(&usePolling, "P", false, "use polling instead of OS file system events (slower).")

	// Flag for following tailed files
	flag.BoolVar(&followTrack, "F", false, "follow new file lines and track file changes.")

	// Flag for following tailed files
	var followFlag bool
	flag.BoolVar(&followFlag, "f", false, "follow new file lines. No change tracking.")

	// For later - number to use for head or tail or start at
	var numLines int
	// String for number to use for head or tail or to with offset
	var numLinesStr string
	// Number of lines to print argument
	flag.StringVar(&numLinesStr, "n", "10", "number of lines - prefix '+' for head to start at line n")

	var prettyFlag bool
	// Pretty printing flag
	flag.BoolVar(&prettyFlag, "p", false, "print extra formatting to output if more than one file is listed")

	var printLinesFlag bool
	// Pring line numbers flag
	flag.BoolVar(&printLinesFlag, "N", false, "show line numbers")

	var headFlag bool
	// Print head lines flag
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
		fmt.Fprintln(out, colourOutput(brightRed, "Can't use -H and -f together. Exiting with usage information."))
		printHelp(out)
	}

	if helpFlag == true {
		out := os.Stdout
		printHelp(out)
	}

	justDigits, err := regexp.MatchString(`^[0-9]+$`, numLinesStr)
	if err != nil {
		out := os.Stderr
		fmt.Fprintln(out, colourOutput(brightRed, "Got error", err.Error()))
		printHelp(out)
	}
	if justDigits == false {
		// Test for + prefix. Complain later if something else is wrong
		if !strings.HasPrefix(numLinesStr, "+") {
			out := os.Stderr
			fmt.Fprintln(out, colourOutput(brightRed, "Invalid -n value", numLinesStr, ". Exiting with usage information."))
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
			fmt.Fprintln(out, colourOutput(brightRed, "Invalid -n value", nStrOrig, ". Exiting with usage information."))
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
			fmt.Fprintln(out, colourOutput(brightRed, "invalid -n value", numLinesStr, ". Exiting with usage information."))
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

	// If a large amount of processing is required handling output for a file at
	// a time shoud help the garbage collector and memory usage.
	// Added total for more informative output.
	var write = func(path string, head bool, lines []string, total int) {
		builder := new(strings.Builder)

		strategyStr := "tail"
		if head {
			if !startAtOffset {
				strategyStr = "head"
			}
		}

		// Skips for single file and stdin
		if prettyFlag == true && multipleFiles {
			builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
		}

		// head is also true
		if startAtOffset {
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", total), total)))
			} else {
				// The tail utility prints out filenames if there is more than one
				// file. Do so here as well.
				if multipleFiles {
					extent := len(lines) + numLines - 1
					builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("==> %s - starting at %d of %s %d <==\n", path, numLines, pluralize("line", "lines", total), extent)))
				}
			}
		} else {
			// The tail utility prints out filenames if there is more than one
			// file. Do so here as well.
			if len(lines) == 0 && multipleFiles {
				builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("==> %s - %s of %d %s <==\n", path, strategyStr, len(lines), pluralize("line", "lines", len(lines)))))
			} else {
				// The tail utility prints out filenames if there is more than one
				// file. Do so here as well.
				if multipleFiles {
					if startAtOffset {
						builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("==> %s - starting at %d of %d %s <==\n", path, numLines, total, pluralize("line", "lines", total))))
					} else {
						if head {
							count := numLines
							if numLines > total {
								count = total
							}
							builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("==> %s - head %d of %d %s <==\n", path, count, total, pluralize("line", "lines", total))))
						} else {
							count := numLines
							if numLines > total {
								count = total
							}
							builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("==> %s - tail %d of %d %s <==\n", path, count, total, pluralize("line", "lines", total))))
						}
					}
				}
			}
		}
		if prettyFlag == true && multipleFiles {
			builder.WriteString(colourOutput(brightBlue, fmt.Sprintf("%s\n", strings.Repeat("-", 80))))
		}

		index := 0
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
			// panic if something went wrong
			panic(err)
		}

		// write to stdout
		write("", headFlag, lines, total)
		os.Exit(0)
	}

	// Get args not tied to defined parameters. They will be interpreted as file
	// paths.
	args := flag.Args()

	// For printing out file information when > 1 file being processed
	multipleFiles = len(args) > 1 // Are multiple files to be printed

	if len(args) == 0 {
		out := os.Stderr
		fmt.Fprintln(out, colourOutput(brightRed, "No files specified. Exiting with usage information."))
		printHelp(out)
	}

	var followedFiles = make([]*followedFile, 0, len(args))

	// Iterate through file path args
	for i := 0; i < len(args); i++ {
		lines, total, err := getLines(args[i], headFlag, startAtOffset, numLines)
		if err != nil {
			// panic if something like a bad filename is used
			panic(err)
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

	// Release waitgroup for each file being followed. This allows waiting until
	// initial tail lines printed.
	// No items will result in nothing done
	for _, ff := range followedFiles {
		ff.wg.Done()
	}

	// Wait to exit if files being followed
	if followFlag && !headFlag {
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)

		<-c
	}
}
