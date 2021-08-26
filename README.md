# tail

A tail implementation, or at least part of it.

This app takes a number of final lines to print, a list of paths to files, and
an argument tied to adding a bit of formatting, and for each file prints out the
final lines (as specified by the -n argument) of each file to standard output.
If there is an error such as a bad filename the app will exit with an error
message.

Formally, the arguments are as follows:

* `tail <file>` should print the last 10 lines of the given file to standard out
	* This supports absolute and relative unix file paths
* `tail -n number <file>` prints the last `number` lines of the file
* `tail <file1> <file2> ...` prints the the last 10 lines of all the provided files
* `tail -n number <file1> <file2>` prints the last -n lines of all provided files
* `tail -pretty <file1> <file2>` prints the last 10 lines of all provided
  files with extra formatting.

A hard-core application would use a buffer to hold lines and do something like
iterate in reverse through the contents of a file, printing out line by line
until the target number had been reached or there were no more lines. This could
be done with some sort of rune processing character by character with a count as
newline characters were encountered. I would be able to write such an
application, but I would need to have a good reason to expend the extra effort.

This code has a test and a benchmark. In the base directory you can run the test
by typing:

`go test -v .`

To run the benchmark, in the base directory type:

`go test -run=XXX -bench=. -benchmem`


Ian A. Marsman