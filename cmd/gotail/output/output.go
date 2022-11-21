package output

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/TylerBrock/colorjson"
	"github.com/fatih/color"
	"github.com/imarsman/gotail/cmd/gotail/util"
	"github.com/imarsman/gotail/cmd/internal/args"
	"github.com/jwalton/gchalk"
	"github.com/nxadm/tail"

	"github.com/nxadm/tail/ratelimiter"
)

var printerOnce sync.Once      // used to ensure printer instantiated only once
var outputPrinter *linePrinter // A struct to handle printing lines

func init() {
	// We'll always get the same instance from newPrinter.
	outputPrinter = newLinePrinter()
}

var reJSON = `(?P<PREFIX>[^\{]+)(?P<JSON>[\{].*$)`
var compRegEx = regexp.MustCompile(reJSON)

type jsonLine struct {
	prefix string
	json   string
}

// colourize print output with colour highlighting if the -c/--colour flag is used
// Currently messes up piping
func colourize(output string) (colourOutput string) {
	var obj interface{}
	json.Unmarshal([]byte(output), &obj)
	// obj = expandInterfaceToMatch(obj)

	f := colorjson.NewFormatter()
	f.Indent = 2
	f.KeyColor = color.New(color.FgHiBlue)

	s, err := f.Marshal(obj)
	if err != nil {
		fmt.Println(err)
		return
	}

	return string(s)
}

func getParamMap(re *regexp.Regexp, input string) (ok bool, paramsMap map[string]string) {
	matches := re.FindStringSubmatch(input)

	paramsMap = make(map[string]string)
	for i, name := range re.SubexpNames() {
		if i > 0 && i <= len(matches) {
			paramsMap[name] = matches[i]
		}
	}
	ok = true
	return
}

func getContent(input string) (ok bool, jl jsonLine) {
	gotParams, matches := getParamMap(compRegEx, input)
	if !gotParams {
		return
	}

	if len(matches) == 0 {
		return
	}
	isJSON := json.Valid([]byte(matches[`JSON`]))
	if !isJSON {
		return
	}
	ok = true
	jl.prefix = strings.TrimSpace(matches[`PREFIX`])
	jl.json = matches[`JSON`]

	return
}

// expandInterfaceToMatch take interface and expand to match JSON or YAML interface structures
func expandInterfaceToMatch(i interface{}) interface{} {
	switch x := i.(type) {
	case map[interface{}]interface{}:
		m2 := map[string]interface{}{}
		for k, v := range x {
			m2[k.(string)] = expandInterfaceToMatch(v)
		}
		return m2
	case []interface{}:
		for i, v := range x {
			x[i] = expandInterfaceToMatch(v)
		}
	}
	return i
}

// IndentJSON read json in then write it out indented
func IndentJSON(input string) (result string, err error) {
	var obj interface{}
	err = json.Unmarshal([]byte(input), &obj)
	if err != nil {
		fmt.Println(gchalk.Red(err.Error()))
		os.Exit(1)
	}
	obj = expandInterfaceToMatch(obj)

	bytes, err := json.MarshalIndent(&obj, "", "  ")
	if err != nil {
		return
	}
	result = strings.TrimSpace(string(bytes))

	return
}

// GetOutput get output from a log line consisting of the timestamp prefix and potentially JSON payload
func GetOutput(input string) (output string, err error) {
	ok, jl := getContent(input)
	if ok {
		var json string
		var err error
		if args.Args.JSON && !args.Args.NoColour {
			json, err = IndentJSON(jl.json)
			if err != nil {

			}
		} else {
			json = jl.json
		}

		if args.Args.NoColour {
			if args.Args.JSON {
				json, err = IndentJSON(json)
				if err != nil {

				}
				output = fmt.Sprintf("%s, %s", jl.prefix, json)
			} else {
				output = fmt.Sprintf("%s, %s", jl.prefix, json)
			}
		} else {
			if args.Args.JSON {
				output = fmt.Sprintf("%s %s", jl.prefix, colourize(fmt.Sprintf("%s", json)))
			} else {
				output = fmt.Sprintf("%s, %s", jl.prefix, json)
			}
		}
	} else {
		if args.Args.JSONOnly {
			err = errors.New("line is not JSON and JSON only flag used")
			return
		}
		output = fmt.Sprintf("%s", input)
	}

	return
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
			fmt.Println(Colour(BrightBlue, fmt.Sprintf("==> %s <==", m.path)))
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
	Path string
	Tail *tail.Tail
	ch   chan struct{}
}

// Unlock channel for file by writing to channel
func (ff *FollowedFile) Unlock() {
	ff.ch <- *new(struct{})
}

// NewFollowedFileForPath create a new file that will start tailing
func NewFollowedFileForPath(path string) (ff *FollowedFile, err error) {
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

	// Set up a new tailfile with no logging
	tf, err := tail.TailFile(path, tail.Config{
		Follow: true, RateLimiter: lb, ReOpen: true, Location: &si, Logger: tail.DiscardingLogger},
	)
	if err != nil {
		return
	}

	ff = &FollowedFile{}
	ff.Tail = tf
	ff.Path = path

	// make channel to use to wait for initial lines to be tailed
	ff.ch = make(chan struct{})

	// Using anonymous function to avoid having this called separately
	go func() {
		// Wait for initial output to be done in main.
		<-ff.ch
		// defer ff.Tail.Cleanup()

		// Range over lines that come in, actually a channel of line structs
		for line := range ff.Tail.Lines {
			if !util.CheckMatch(line.Text) {
				continue
			}
			output, err := GetOutput(line.Text)
			if err != nil {
				continue
			}
			outputPrinter.print(ff.Path, output)
		}
	}()

	return
}
