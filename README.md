# tail

A tail implementation, or at least part of it.

This app takes a number of final lines to print, a list of paths to files, and
an argument tied to adding a bit of formatting, and for each file prints out the
final lines (as specified by the -n argument) of each file to standard output.
If there is an error such as a bad filename the app will exit with an error
message.

Formally, the arguments are as follows:

* `tail <file>` prints the last 10 lines of the given file to standard out
	* This supports absolute and relative unix file paths
* `tail -H <file>` prints the first `number` lines of the file
* `tail -n number <file>` prints the last `number` lines of the file
* `tail <file1> <file2> ...` prints the the last 10 lines of all the provided files
* `tail -n number <file1> <file2>` prints the last -n lines of all provided files
* `tail -pretty <file1> <file2>` prints the last 10 lines of all provided
  files with extra formatting.
  * Also accepts -p for pretty
* `tail -N <file>` prints the last 10 lines of the given file to standard out
  with leading line numbering.

The app can be build by typing (with a go 1.16 compiler. If you have an older
version of Go installed you can change the version number in go.mod. This should
be compatible with earlier versions of go like 1.14 and 1.15 though I have not
checked.

`go build tail.go`

It can be run without building by typing

`go run tail.go`

Somewhat surprisingly, file globbing works. Thus this works:

`./tail -N -n 15 sample/*.txt`

A hard-core application would use a buffer to hold lines and do something like
iterate in reverse through the contents of a file, printing out line by line
until the target number had been reached or there were no more lines. This could
be done with some sort of rune processing character by character with a count as
newline characters were encountered. I would be able to write such an
application, but I would need to have a good reason to expend the extra effort.

I did modify the code to print out a file at a time rather than building a
buffer of all of the lines for all of the files then printing.

This code has a test and a benchmark. In the base directory you can run the test
by typing:

`go test -v ./...`

To run the benchmark, in the base directory type:

`go test -run=XXX -bench=. -benchmem ./...`

To see what the Go compiler does with the code type:

`go build -gcflags '-m -m' ./*.go 2>&1 |less`

Ian A. Marsman