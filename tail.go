package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/nxadm/tail"
)

/*
	This app takes a number of lines argument, a "pretty" argument for more
	illustrative output, and a list of paths to files, and for each file gathers
	the number of lines requested, if available, and then prints them out to
	standard out.

	The ideal implementation would use a buffer to read in just enough of each
	file to satisfy the number of lines parameter.
*/

// getLines get lasn num lines in file and return them as a string slice. Return
// an error if for instance a filename is incorrect.
func getLines(num int, startAtOffset, head bool, path string) ([]string, int, error) {
	var total int
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
	var all []string

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	var count = 0
	for scanner.Scan() {
		count++
		if startAtOffset && head && count >= num {
			all = append(all, scanner.Text())
		} else if !startAtOffset {
			all = append(all, scanner.Text())
		}
	}
	if scanner.Err() != nil {
		return []string{}, total, scanner.Err()
	}

	total = count

	// Exit now as we have what we need if we need head lines starting at offset
	if head && startAtOffset {
		return all, total, nil
	}

	// Make output slice at capacity we need
	var lines = make([]string, 0, num)

	// If we want first num lines
	if head {
		// Get the first lines instead of the last lines
		if total >= num {
			for i := 0; i < num; i++ {
				lines = append(lines, all[i])
				if len(lines) == num {
					break
				}
			}
		} else {
			for i := 0; i < total; i++ {
				lines = append(lines, all[i])
				if len(lines) == num {
					break
				}
			}
		}
		// If we want tail lines
	} else {
		// Get last num lines by iterating backwards
		// Slightly more efficient to pre-allocate capacity to known value.
		for i := len(all) - 1; i > -1; i-- {
			lines = append(lines, all[i])
			if len(lines) == num {
				break
			}
		}

		// Another way to do it, which is easier to follow for me. Sample I found
		// returned the slice but you don't need to do that with a slice when it is
		// not being changed in size. As a rule, though, if the slice might be
		// changed you can pass a pointer to it, though that makes it a bit more
		// cumbersome syntactially. I dealt with it in terms of pointers to
		// experiment with the contorted dereferencing.
		var reverse = func(s *[]string) {
			for i, j := 0, len(*s)-1; i < j; i, j = i+1, j-1 {
				(*s)[i], (*s)[j] = (*s)[j], (*s)[i]
			}
		}

		// Call the function just defined
		reverse(&lines)
	}

	return lines, total, nil
}

func printHelp() {
	fmt.Println("Print tail (or head) n lines of one or more files")
	fmt.Println("Note: an -f option is not currently supported")
	fmt.Println("Example: tail -n 10 file1.txt file2.txt")
	flag.PrintDefaults()
	os.Exit(0)
}

var linePrinter *printer

func init() {
	linePrinter = new(printer)
	linePrinter.mu = new(sync.Mutex)
}

// FollowedFile a file being tailed (followed)
type FollowedFile struct {
	Path string
	Tail *tail.Tail
}

type printer struct {
	CurrentPath string
	mu          *sync.Mutex
}

func (p *printer) print(path, line string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.CurrentPath == path {
		fmt.Println(line)
	} else {
		fmt.Printf("==> File %s <==\n", path)
		fmt.Println(line)
	}
}

func (ff *FollowedFile) followFile() {
	for line := range ff.Tail.Lines {
		linePrinter.print(ff.Path, line.Text)
	}
}

// NewTailedFileForPath create a new file that will start tailing
func NewTailedFileForPath(path string) (*FollowedFile, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	// get the size
	size := fi.Size()
	si := tail.SeekInfo{Offset: size}
	tf, err := tail.TailFile("", tail.Config{Follow: true, Location: &si})
	if err != nil {
		return nil, err
	}

	ff := FollowedFile{}
	ff.Tail = tf
	ff.Path = path

	go ff.followFile()

	return &ff, nil
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

	if h == true {
		printHelp()
	}

	justDigits, err := regexp.MatchString(`^[0-9]+$`, nStr)
	if err != nil {
		fmt.Println("got error", err)
		printHelp()
	}
	if justDigits == false {
		// Test for + prefix. Complain later if something else is wrong
		if !strings.HasPrefix(nStr, "+") {
			fmt.Println("invalid -n value", nStr)
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
			fmt.Println("invalid -n value", nStrOrig)
			printHelp()
		}
	} else {
		var err error
		// Extremely unlikely to have error as we've checked for all digits
		n, err = strconv.Atoi(nStr)
		if err != nil {
			fmt.Println("invalid -n value", nStr)
			printHelp()
		}
	}

	var multiple bool
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
		if p == true && multiple {
			builder.WriteString(fmt.Sprintf("%s\n", strings.Repeat("-", 70)))
		}
		// head is also true
		if startAtOffset {
			if len(lines) == 0 {
				extent := total
				builder.WriteString(fmt.Sprintf("==> File %s - starting at %d of %d lines <==\n", fname, n, extent))
			} else {
				// The tail utility prints out filenames if there is more than one
				// file. Do so here as well.
				if multiple {
					extent := len(lines) + n - 1
					builder.WriteString(fmt.Sprintf("==> File %s - starting at %d of %d lines <==\n", fname, n, extent))
				}
			}

		} else {
			// The tail utility prints out filenames if there is more than one
			// file. Do so here as well.
			if multiple {
				builder.WriteString(fmt.Sprintf("==> File %s - %s %d of %d lines <==\n", fname, strategyStr, len(lines), total))
			}
		}
		if p == true && multiple {
			builder.WriteString(fmt.Sprintf("%s\n", strings.Repeat("-", 70)))
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

	multiple = len(args) > 1

	if len(args) == 0 {
		fmt.Println("No files specified. Exiting with usage information")
		fmt.Println()
		printHelp()
	}

	for i := 0; i < len(args); i++ {
		lines, total, err := getLines(n, startAtOffset, head, args[i])
		if err != nil {
			// panic if something like a bad filename is used
			panic(err)
		}
		write(args[i], head, lines, total)
	}
}
