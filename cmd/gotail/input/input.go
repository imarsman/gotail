package input

import (
	"bufio"
	"fmt"
	"os"
)

// GetLines get linesWanted lines or start gathering lines at linesWanted if
// head is true and startAtOffset is true. Return lines as a string slice.
// Return an error if for instance a filename is incorrect.
func GetLines(path string, head, startAtOffset bool, linesWanted int) (lines []string, totalLines int, err error) {
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
