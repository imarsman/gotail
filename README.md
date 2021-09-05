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

 * `-h` print usage
 * `-n` string
   * number of lines to print from tail or head of file
   * when the `+` prefix is used `-H` is assumed (e.g. `-n +10`) and causes
     printout to start at `-n` lines into file
 * `-f`	follow new file lines but don't recover from reopened or renamed files
   * this will fail if the `-H` option is specified
 * `-F`	follow new file lines and handle reopened or renamed files
   * this will fail if the `-H` option is specified
 * `-P` use polling instead of OS file system events (slower but may be required
   on Windows).
 * `-p`	print extra formatting to output if more than one file is listed
 * `-C`	no colour output
 * `-N`	show line numbers
 * `-H`	print head of file rather than tail - assumed with `+` in `-n` value
   * fails with `-f` option

One possible extension would be to periodically look for new files and add them
to a followed list.

## Building and Running

The app can be built by typing the command below (with a Go 1.16 compiler). If
you have an older version of Go installed you can change the version number in
go.mod if there is a complaint on trying to compile. This should be compatible
with earlier versions of Go like 1.14 and 1.15 though I have not checked. This
app does not use embedding, which appeared in Go 1.16.

`go build tail.go -o gotail`

To build for Windows, for which there is an existing equivalent whose syntax I
always forget. 

`GOOS=windows GOOARCH=amd64 go build -o tail-windows .`

FYI the Windows command is:

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

`go run tail.go`

Somewhat surprisingly, file globbing works for path patterns that contain the
`*` character. I have not read the source code of the flag package but the logic
to intepret globbing patterns as paths must be in there. Thus this works:

`./tail -N -n 15 sample/*.txt`

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