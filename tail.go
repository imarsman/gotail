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

/*
	This app takes a number of lines argument, a "pretty" argument for more
	illustrative output, and a list of paths to files, and for each file gathers
	the number of lines requested, if available, and then prints them out to
	standard out.

	The ideal implementation would use a buffer to read in just enough of each
	file to satisfy the number of lines parameter.
*/

var linePrinter *printer
var currentFile *atomic.Value

func init() {
	// Instantiate our current file atomic value
	currentFile = new(atomic.Value)
	// We're storing a string so start that off
	currentFile.Store("")

	// Initialize our line printer
	linePrinter = new(printer)
}

// FollowedFile a file being tailed (followed)
type FollowedFile struct {
	Path string
	Tail *tail.Tail
	wg   *sync.WaitGroup
}

// followFile follow a tailed file and call print when new lines come in
func (ff *FollowedFile) followFile() {
	for line := range ff.Tail.Lines {
		linePrinter.print(ff.Path, line.Text)
	}
}

// NewFollowedFileForPath create a new file that will start tailing
func NewFollowedFileForPath(path string) (*FollowedFile, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	// get the size
	size := fi.Size()
	si := tail.SeekInfo{Offset: size, Whence: 0}
	lb := ratelimiter.NewLeakyBucket(100, 1*time.Millisecond)
	tf, err := tail.TailFile(path, tail.Config{Follow: true, RateLimiter: lb, Location: &si})
	if err != nil {
		return nil, err
	}

	ff := FollowedFile{}
	ff.Tail = tf
	ff.Path = path
	ff.wg = new(sync.WaitGroup)

	go ff.followFile()

	return &ff, nil
}

// This could just be an atomic.Value but probably that's too restricted.
type printer struct {
}

// print print lines from a followed file
func (p *printer) print(path, line string) {
	if currentFile.Load().(string) == path {
		fmt.Println(line)
	} else {
		currentFile.Store(path)
		fmt.Printf("==> File %s <==\n", path)
		fmt.Println(line)
	}
}

// getLines get lasn num lines in file and return them as a string slice. Return
// an error if for instance a filename is incorrect.
func getLines(num int, startAtOffset, head bool, path string) ([]string, int, error) {
	total := 0

	file, err := os.Open(path)
	if err != nil {
		return nil, total, err
	}

	// Deferring in case an error occurs
	defer file.Close()

	// A bit inefficient as whole file is read in then out again in reverse
	// order up to num.
	// Since we will have to get the last items we have to read all lines in
	// then shorten the output. Other algorithms would involve avoiding reading
	// all the contents in by using a buffer or counting lines or some other
	// technique.
	var all = make([]string, 0, num*2)

	// Use reader to count lines but discard what is not needed.
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	// Get head lines
	if head {
		if startAtOffset {
			total = 1
			for scanner.Scan() {
				if total >= num {
					all = append(all, scanner.Text())
				}
				total++
			}
			if scanner.Err() != nil {
				return []string{}, total, scanner.Err()
			}
			return all, total, nil
		}
		total = 0
		for scanner.Scan() {
			if total <= num {
				all = append(all, scanner.Text())
			}
			total++
		}
		if scanner.Err() != nil {
			return []string{}, total, scanner.Err()
		}
		return all, total, nil
	}

	// Get tail lines
	total = 0
	for scanner.Scan() {
		all = append(all, scanner.Text())
		// If we have more than we need, remove first element
		if total >= num {
			all = all[1:]
		}
		total++
	}
	if scanner.Err() != nil {
		return []string{}, total, scanner.Err()
	}
	return all, total, nil
}

func printHelp() {
	fmt.Println(gchalk.BrightGreen(os.Args[0], "- a simple tail program"))
	fmt.Println("Usage")
	fmt.Println("- print tail (or head) n lines of one or more files")
	fmt.Println("Example: tail -n 10 file1.txt file2.txt")
	flag.PrintDefaults()
	os.Exit(0)
}

// Option for following files that seems to be cross platform
// https://github.com/nxadm/tail

func main() {
	var h bool
	// Help flag
	flag.BoolVar(&h, "h", false, "print usage")

	// Flag for whetehr to start tail partway into a file
	var startAtOffset bool

	var follow bool
	flag.BoolVar(&follow, "f", false, "follow new file lines")

	// For later - number to use for head or tail or start at
	var n int
	// String for number to use for head or tail or to with offset
	var nStr string
	// Number of lines to print argument
	flag.StringVar(&nStr, "n", "10", "number of lines - prefix '+' for head to start at line n")

	var p bool
	// Pretty printing flag
	flag.BoolVar(&p, "p", false, "print extra formatting to output if more than one file is listed")

	var printLines bool
	// Pring line numbers flag
	flag.BoolVar(&printLines, "N", false, "show line numbers")

	var head bool
	// Print head lines flag
	flag.BoolVar(&head, "H", false, "print head of file rather than tail")

	flag.Parse()

	if head && follow {
		fmt.Println(gchalk.BrightRed("Can't use -H and -f together. Exiting with usage information."))
		printHelp()
	}

	if h == true {
		printHelp()
	}

	justDigits, err := regexp.MatchString(`^[0-9]+$`, nStr)
	if err != nil {
		fmt.Println(gchalk.BrightRed("Got error", err.Error()))
		printHelp()
	}
	if justDigits == false {
		// Test for + prefix. Complain later if something else is wrong
		if !strings.HasPrefix(nStr, "+") {
			fmt.Println(gchalk.BrightRed("Invalid -n value", nStr, ". Exiting with usage information."))
			printHelp()
		}
	}
	// Deal selectively with offset
	if !justDigits {
		nStrOrig := nStr
		nStr = nStr[1:]
		// If we are in a head situation we will set the startAt flag
		if head {
			startAtOffset = true
		}
		// Ignore prefix if not a head request
		var err error
		// Invalid  somehow - for example +20a is not caught above but would be invalid
		n, err = strconv.Atoi(nStr)
		if err != nil {
			fmt.Println(gchalk.BrightRed("Invalid -n value", nStrOrig, ". Exiting with usage information."))
			printHelp()
		}
	} else {
		var err error
		// Extremely unlikely to have error as we've checked for all digits
		n, err = strconv.Atoi(nStr)
		if err != nil {
			fmt.Println(gchalk.BrightRed("invalid -n value", nStr, ". Exiting with usage information."))
			printHelp()
		}
	}

	var multipleFiles bool
	// If a large amount of processing is required handling output for a file at
	// a time shoud help the garbage collector and memory usage.
	// Added total for more informative output.
	var write = func(fname string, head bool, lines []string, total int) {
		builder := new(strings.Builder)
		strategyStr := "last"
		if head {
			if !startAtOffset {
				strategyStr = "first"
			}
		}
		if p == true && multipleFiles {
			builder.WriteString(fmt.Sprintf("%s\n", strings.Repeat("-", 80)))
		}
		// head is also true
		if startAtOffset {
			if len(lines) == 0 {
				extent := total
				builder.WriteString(fmt.Sprintf("==> File %s - starting at %d of %d lines <==\n", fname, n, extent))
			} else {
				// The tail utility prints out filenames if there is more than one
				// file. Do so here as well.
				if multipleFiles {
					extent := len(lines) + n - 1
					builder.WriteString(fmt.Sprintf("==> File %s - starting at %d of %d lines <==\n", fname, n, extent))
				}
			}

		} else {
			// The tail utility prints out filenames if there is more than one
			// file. Do so here as well.
			if multipleFiles {
				builder.WriteString(fmt.Sprintf("==> File %s - %s %d of %d lines <==\n", fname, strategyStr, len(lines), total))
			}
		}
		if p == true && multipleFiles {
			builder.WriteString(fmt.Sprintf("%s\n", strings.Repeat("-", 80)))
		}
		index := 0
		for i := 0; i < len(lines); i++ {
			if printLines == true {
				if startAtOffset {
					index = i + n
				} else {
					index = i + 1
				}
				builder.WriteString(fmt.Sprintf("%-3d %s\n", index, lines[i]))
			} else {
				builder.WriteString(fmt.Sprintf("%s\n", lines[i]))
			}
		}
		fmt.Println(strings.TrimSpace(builder.String()))
	}

	// Iterate through list of files (the bits that are not flags), using a
	// strings builder to prepare output. Strings builder avoids allocation.
	args := flag.Args()

	// For printing out file information when > 1 file being processed
	multipleFiles = len(args) > 1

	if len(args) == 0 {
		fmt.Println(gchalk.BrightRed("No files specified. Exiting with usage information."))
		printHelp()
	}

	for i := 0; i < len(args); i++ {
		lines, total, err := getLines(n, startAtOffset, head, args[i])
		if err != nil {
			// panic if something like a bad filename is used
			panic(err)
		}
		if !head && follow {
			_, err = NewFollowedFileForPath(args[i])
			if err != nil {
				panic(err)
			}
		}

		write(args[i], head, lines, total)
	}

	if follow && !head {
		c := make(chan os.Signal)
		signal.Notify(c, os.Interrupt)

		<-c
	}
}
