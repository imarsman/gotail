# tail (gotail)

This is an implementation of part of the tail command, which was used in PWB
UNIX, out of Bell Labs, in 1977. The tail command does lots of things, most
prominently showing the last lines of a file. The tail command also allows you
to print lines of a file stating at an offset and to show new lines in a file as
they are written to the file. This implementation does all of this but does not
currently add new files that appear in a directory.

The specification for the tail command can be found
[here](https://pubs.opengroup.org/onlinepubs/007904875/utilities/tail.html). No
claim is made that this app is compliant with that standard. This implementation
does not have the ability to use bytes as the offset, it only uses lines (`-n`).
Unlike the standard `tail`, this implementation has a `-H` (head) flag and
produces coloured output for file paths. Colour output can be turned off using
the `-C` flag. This implementation also allows for a small amount of extra
formatting to be added using the `-p` (pretty) flag and for the output to
include line numbers for non-followed output using the `-N` flag.

## Arguments

The arguments are as follows:

```
% gotail -h
Usage: gotail [--nocolour] [--polling] [--followflag] [--numlinesstr NUMLINESSTR] [--printextra] 
   [--linenumbers] [--head] [FILES [FILES ...]]

Positional arguments:
  FILES                  files to tail

Options:
  --nocolour, -C         no colour
  --polling, -P          polling - use file polling instead of inotify
  --followflag, -f       follow new file lines.
  --numlinesstr NUMLINESSTR, -n NUMLINESSTR
                         number of lines - prefix '+' for head to start at line n [default: 10]
  --printextra, -p       print extra formatting to output if more than one file is listed
  --linenumbers, -N      show line numbers
  --head, -H             print head of file rather than tail
  --help, -h             display this help and exit
```

One possible extension would be to periodically look for new files and add them
to a followed list.

## Building and Running

This build requires a build flag to be available to either use or not use
sycall.RLimit. Windows does not support syscall.RLimit, so there re two files,
gotail_windows.go and gotail_nonwindws.go, only one of which should be used when
compiling. The windows one has an empty function to satisfy build checks but
does not bring in the call to RLimit. You can use a sample script as a pointer
for how to do the build. For example, the makewindows.sh script has:

```shell
#!/bin/bash

GOOS=windows GOARCH=amd64 go build -o gotail.exe
```
makedarwin.sh has

```shell
#!/bin/bash

GOOS=windows GOARCH=amd64 go build -o gotail.exe
```

For a different architectu specify a different GOOS and GOARCH.

The Windows "tail" command is:

`Get-Content <filename> -Wait -Tail 30`

I have not tested the follow part on Windows. This app uses a follow library and
keeping track of files that get appended to is done idiosynchratically on
Windows. If there is an issue the tail package allows for a different strategy
to be used for tracking file changes. I  have added a `-P` option to use polling
rather than the native follow. From what I can tell, though, the tail library
being used should work. I have yet to test on Windows.

If you don't provide the file to compile the built app will be named whatever
the directory from the repository is named. In this case the app would be
compiled to be named `tail`. It might be best to call the compiled binary
something like `gotail`. 

The app can be run without building by typing

`go run gotail.go`

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

## Notes

The argument parsing library used here does not deal with arguments such as -1,
-2, -, etc. It may be that an argument will need to have a different identifier to
work around this.

-- Ian A. Marsman