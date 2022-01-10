package print

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/nxadm/tail"

	"github.com/nxadm/tail/ratelimiter"
)

var printerOnce sync.Once      // used to ensure printer instantiated only once
var outputPrinter *linePrinter // A struct to handle printing lines

// FollowedFiles a list of followed files - lasts for run of app
var FollowedFiles = make([]*FollowedFile, 0, 100) // initialize followed files here

func init() {
	// We'll always get the same instance from newPrinter.
	outputPrinter = newLinePrinter()
}

// a message to be sent when following a file
type msg struct {
	path string
	line string
}

// linePrinter a printer is a central place for printing new lines.
type linePrinter struct {
	currentPath string
	messages    chan (msg)
}

// NewLinePrinter get new printer instance properly instantiated
// Use package level linePrinter to enforce singleton pattern, as that is the
// needed pattern at this point.
func newLinePrinter() *linePrinter {
	if outputPrinter != nil {
		return outputPrinter
	}
	// Ensure linePrinter is set up only once
	printerOnce.Do(func() {
		outputPrinter = new(linePrinter)
	})

	// initialize to empty string
	outputPrinter.setPath("")
	outputPrinter.messages = make(chan (msg))

	// Print messages in goroutine to avoid exposing messages channel which has
	// its own locking behaviour. Use of a channel avoids worries about race
	// condition with incoming path compared to printer path. Previous code
	// tried atomic values for path and a mutex instead of a channel.
	go func() {
		for m := range outputPrinter.messages {
			if outputPrinter.getPath() == m.path {
				fmt.Println(m.line)
				continue
			}
			// Print out a header and set new value for the path.
			outputPrinter.setPath(m.path)
			fmt.Println()
			fmt.Println(OutputColour(BrightBlue, fmt.Sprintf("==> %s <==", m.path)))
			fmt.Println(m.line)
		}
	}()

	return outputPrinter
}

func (p *linePrinter) setPath(path string) {
	p.currentPath = path
}

func (p *linePrinter) getPath() string {
	return p.currentPath
}

// print print lines from a followed file.
// An anonymous function is started in newPrinter to handle additions to the
// message channel.
func (p *linePrinter) print(path, line string) {
	m := msg{path: path, line: line}
	p.messages <- m
}

// FollowedFile a file being tailed (followed).
// Uses the tail library which has undoubtedly taken many hours to get working
// well.
type FollowedFile struct {
	path string
	tail *tail.Tail
	ch   chan struct{}
}

// Unlock channel for file by writing to channel
func (ff *FollowedFile) Unlock() {
	ff.ch <- *new(struct{})
}

// NewFollowedFileForPath create a new file that will start tailing
func NewFollowedFileForPath(path string) (followed *FollowedFile, err error) {
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

	followed = &FollowedFile{}
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
			outputPrinter.print(followed.path, line.Text)
		}
	}()

	return
}
