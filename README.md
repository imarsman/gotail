# tail (gotail)

This is an implementation of the tail command, which was used in PWB UNIX, out
of Bell Labs, in 1977. The tail command does lots of things, most prominently
showing the last lines of a file. The tail command also allows you to print
lines of a file stating at an offset and to show new lines in a file as they are
written to the file. This implementation does all of this.

The specification for the tail command can be found
[here](https://pubs.opengroup.org/onlinepubs/007904875/utilities/tail.html). No
claim is made that this app is compliant with that standard. This implementation
does not have the ability to use bytes as the offset, it only uses lines (`-n`).
Unlike the standard `tail`, this implementation has a `-H` (head) flag and
produces coloured output for file paths. Colour output can be turned off using
the `-C` flag. This implementation also allows for a small amount of extra
formatting to be added using the `-p` (pretty) flag and for the output to
include line numbers for non-followed output using the `-N` flag.

This implementation of the tail command allows glob patterns to be specified in
addition to a list of files. Here is an example

```sh
gotail -f -G "/var/log/*log" -G "/tmp/test.txt" ~/dir/file.txt ~/dir2/*txt
```

This would take the expanded file list from the final argument (which are not
re-checked since by the time the code sees the list it will have been expanded
by the shell) and periodically the globbed patterns will be evaluated to produce
a list of files that will change as files are added and removed.

There is a lot for the code to keep track of, including use of resources if a
file disappears. The tail library being used will begin timing out and
re-checking for a file that disappears.

Along with the switch to using the go-arg commandline argument handling package
a general review was carried out that allowed interim logic to be removed from
code.

To make things less intertwined input and output have been split into separate
packages.

## JSON output

gotail can use a `-json` flag to have every log line containing JSON to be
placed in output formatted and colourized. Currently only a single line is
examined. Log output with JSON that spans more than one line will not be
detected.

```
$ echo 'prefix {"timestamp":"2016-11-13 23:06:17.727","level":"INFO","thread":"qtp745835029-19"}'|gotail -json
prefix {
  "level": "INFO",
  "thread": "qtp745835029-19",
  "timestamp": "2016-11-13 23:06:17.727"
}
```

## Completion

`gotail` uses completion using the
[posener](https://github.com/posener/complete/tree/master) library. To activate
it, once `gotail` is in your path, type `COMP_INSTALL=1 gotail`. You will
be asked to confirm that you wish to have completion support added to your shell
config. After running this you will need to refresh your terminal session or
start a new one. If you use `zsh` your `.zshrc` fill will contain `complete -o
nospace -C /path/to/gotail gotail`.

## Arguments

The arguments are as follows:

```
$ gotail -h
This is an implementation of the tail utility. File patterns can be specified
with one or more final arguments or as glob patterns with one or more -G parameters.
If files are followed for new data the glob file list will be checked every interval
seconds.

gotail
------
commit:  407f215
tag:     v0.1.5
date:    2022-11-21T01:07:27Z

Usage: gotail [--nocolour] [--follow] [--numlines NUMLINES] [--printextra] 
      [--linenumbers] [--json] [--json-only] [--match MATCH] [--head] [--glob GLOB] 
      [--interval INTERVAL] [FILES [FILES ...]]

Positional arguments:
  FILES                  files to tail

Options:
  --nocolour, -C         no colour
  --follow, -f           follow new file lines.
  --numlines NUMLINES, -n NUMLINES
                         number of lines - prefix '+' for head to start at line n [default: 10]
  --printextra, -p       print extra formatting to output if more than one file is listed
  --linenumbers, -N      show line numbers
  --json, -j             pretty print JSON
  --json-only, -J        ignore non-JSON
  --match MATCH, -m MATCH
                         match lines by regex
  --head, -H             print head of file rather than tail
  --glob GLOB, -G GLOB   quoted filesystem glob patterns - will find new files
  --interval INTERVAL, -i INTERVAL
                         seconds between new file checks [default: 1]
  --help, -h             display this help and exit
  --version              display version and exit
  ```

One possible extension would be to periodically look for new files and add them
to a followed list.

## Building and Running

This build requires a build flag to be available to either use or not use
sycall.RLimit. Windows does not support syscall.RLimit, so there re two files,
gotail_windows.go and gotail_nonwindws.go, only one of which should be used when
compiling. The windows one has an empty function to satisfy build checks but
does not bring in the call to RLimit.

The build is handled by the Taskfile.yml taskfile. If you don't want to use the
Taskfile you can look at its contents and guess the call. Basically a build for
any OS involves the use of the GOOS and GOARCH environment variables which are
then used by the go build tool to use whichever build flag is appropriate. e.g.
`// +build !windows`.

Here is a sample build invocation.

`GOOS=darwin GOARCH=arm64 go build -o gotail`

Because of the OS specific build flag the `GOOS` environment variable must be
set if using `go run`. For example

`GOOS=darwin go run . ./gotail.go`. This will give the Go compiler enough
information to selectively use the build flag module such as
`gotail_nonwindows.go`. I have not tested out many alternate run parameters.

The Windows "tail" command is:

`Get-Content <filename> -Wait -Tail 30`

I have not tested the follow part on Windows. This app uses a follow library and
keeping track of files that get appended to is done idiosynchratically on
Windows. If there is an issue the tail package allows for a different strategy
to be used for tracking file changes. I have not implemented support for
optional polling.

Somewhat surprisingly, file globbing works for path patterns that contain the
`*` character. I have not read the source code of the flag package but the logic
to intepret globbing patterns as paths must be in there. Thus this works:

`gotail -N -n 15 sample/*.txt`

The code is stuctured to limit memory usage. The buffer used to read in lines
only allocates to the lines slice when it is within range (for tail or head) and
otherwise uses the line fetching only to count lines. The largest memory usage
is likely to be caused by a head request starting at an offset. Each file is
written after its lines are fetched, so hopefully this will help avoid building
of memory use.

## Running Tests and Benchmarks

This code has a test and a benchmark. In the base directory you can run the test
by typing:

  `go test -v ./...`

To run the benchmark, in the base directory type:

  `go test -run=XXX -bench=. -benchmem ./...`

To see what the Go compiler does with the code type:

  `go build -gcflags '-m -m' ./*.go 2>&1 |less`

-- Ian A. Marsman