package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
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
func getLines(num int, path string) ([]string, int, error) {
	var total int
	file, err := os.Open(path)
	if err != nil {
		return nil, total, err
	}

	// A bit inefficient as whole file is read in then out again in reverse
	// order up to num.
	var all []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	if scanner.Err() != nil {
		return []string{}, total, scanner.Err()
	}

	total = len(all)
	// Slightly more efficient to avoid defer and it's ok to do now
	file.Close()

	// Get last num lines
	var lines []string
	for i := len(all) - 1; i > -1; i-- {
		lines = append(lines, all[i])
		if len(lines) == num {
			break
		}
	}

	// Another way to do it, which is easier to follow for me. Sample I found
	// returned the slice but you don't need to do that with a slice.
	var reverse = func(s []string) {
		for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
			s[i], s[j] = s[j], s[i]
		}
	}
	reverse(lines)

	return lines, total, nil
}

func main() {
	var h bool
	flag.BoolVar(&h, "h", false, "print usage")
	var p bool
	flag.BoolVar(&p, "p", false, "add formatting to output")
	flag.BoolVar(&p, "pretty", false, "add formatting to output")
	var n int
	flag.IntVar(&n, "n", 10, "number of lines")
	var printLines bool
	flag.BoolVar(&printLines, "N", false, "show line numbers")
	flag.Parse()
	if h == true {
		fmt.Println("Print last n lines of one or more files")
		fmt.Println("Example: tail -n 10 file1.txt file2.txt")
		flag.PrintDefaults()
		os.Exit(0)
	}

	// If a large amount of processing is required handling output for a file at
	// a time shoud help the garbage collector and memory usage.
	// Added total for when pretty printing used.
	var write = func(fname string, lines []string, total int) {
		builder := new(strings.Builder)
		if p == true {
			builder.WriteString(fmt.Sprintf("%s\n", strings.Repeat("-", 50)))
		}
		builder.WriteString(fmt.Sprintf("File %s showing %d of %d\n", fname, len(lines), total))
		if p == true {
			builder.WriteString(fmt.Sprintf("%s\n", strings.Repeat("-", 50)))
		}
		for i := 0; i < len(lines); i++ {
			if printLines == true {
				builder.WriteString(fmt.Sprintf("%-3d %s\n", i+1, lines[i]))
				continue
			}
			builder.WriteString(fmt.Sprintf("%s\n", lines[i]))
		}
		fmt.Println(strings.TrimSpace(builder.String()))
	}

	// Iterate through list of files (the bits that are not flags), using a
	// strings builder to prepare output. Strings builder avoids allocation.
	// This could be more efficient if the lines were printed immediately.
	args := flag.Args()
	for i := 0; i < len(args); i++ {
		lines, total, err := getLines(n, args[i])
		if err != nil {
			// panic if something like a bad filename is used
			panic(err)
		}
		write(args[i], lines, total)
	}
}
