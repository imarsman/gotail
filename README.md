# tail

This is an implementation of part of the tail command, which apparently first
appeared in PWB UNIX, out of Bell Labs, in 1977. The tail command does lots of
things, most prominently showing the last lines of a file. The tail command also
allows you to print lines of a file stating at an offset and to show new lines
in a file as they are written to the file. This app implements a subset of the
official tail command's options. It prints out a default of the last 10 lines of
a file or a number that can be specified with then -n flag. This implementation
also has flags to allow adding formatting to output, printing out the line
number for each line, and the option of showing the head (first) lines instead
of the tail (last) lines of a file (what the `head` command does). Adding in
support for starting at an offset would be fun but is not implemented.

## Arguments

The arguments are as follows:

 * `-H`	print head of file rather than tail
   * fails with `-f` option
 * `-N`	show line numbers
 * `-f`	follow new file lines (currently handles reopened or renamed files)
   * this will fail if the `-H` option is specified
 * `-h`	print usage
 * `-n` string
   * number of lines - prefix `+` for head to start at line `-n` (default "10")
   * when `-H` is used can prefix with `+` to start at `-n` lines into file
 * `-p`	print extra formatting to output if more than one file is listed


# Building and Running

The app can be built by typing the command below (with a Go 1.16 compiler). If
you have an older version of Go installed you can change the version number in
go.mod if there is a complaint on trying to compile. This should be compatible
with earlier versions of Go like 1.14 and 1.15 though I have not checked. This
app does not use embedding, which appeared in Go 1.16.

`go build tail.go`

If you don't provide the file to compile the built app will be named whatever
the directory from the repository is named. In this case the app would be
compiled to be named `Ian-Challenge`. 

The app can be run without building by typing

`go run tail.go`

Somewhat surprisingly, file globbing works for path patterns that contain the
`*` character. I have not read the source code of the flag package but the logic
to intepret globbing patterns as paths must be in there. Thus this works:

`./tail -N -n 15 sample/*.txt`

An efficiency option for very large files would use a buffer to hold lines and
do something like iterate in reverse through the contents of a file, printing
out line by line until the target number had been reached or there were no more
lines. This would bring in the complexity of dealing with different line ending
standards in Unix and Windows. As it stands, the string reading core package
used, bufio, deals with reading in lines very well. I would be able to write
such an application, but I would need to have a good reason to spend the extra
effort. One great reason would be the ability to handl extremely large files
with a limited increase in memory expended. Such an implementation would read a
portion of the file into memory, such as 1024 bytes or a span or runes, which
would avoid damaging multi-byte characters, and read this buffer into a scanner.
This is something I have not done yet but that is the general plan. Extra issues
would have to be dealt with such as having lines break over buffer reads,
avoiding having an error on hitting the end of a file, etc.

I did modify the code to print out a file at a time rather than building a
buffer of all of the lines for all of the files then printing. I also used an
in-place method to reverse the ordering of a string slice to avoid allocation.
A strings.Builder is used when writing out file data. It has been available
since Go 1.10 and is non-allocating. It also deals with bytes, runes, and
strings.

## Running Tests and Benchmarks

This code has a test and a benchmark. In the base directory you can run the test
by typing:

  `go test -v ./...`

To run the benchmark, in the base directory type:

  `go test -run=XXX -bench=. -benchmem ./...`

To see what the Go compiler does with the code type:

  `go build -gcflags '-m -m' ./*.go 2>&1 |less`

Thank-you for this fun task and for taking the time to review my work on it.

--
Ian A. Marsman