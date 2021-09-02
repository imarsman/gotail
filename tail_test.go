package main

import (
	"fmt"
	"math/rand"
	"os"
	"testing"
)

//                Tests and benchmarks
// -----------------------------------------------------
// benchmark
//   go test -run=XXX -bench=. -benchmem
// Get allocation information and pipe to less
//   go build -gcflags '-m -m' ./*.go 2>&1 |less
// Run all tests
//   go test -v
// Run one test and do allocation profiling
//   go test -run=XXX -bench=IterativeISOTimestampLong -gcflags '-m' 2>&1 |less
// Run a specific test by function name pattern
//  go test -run=TestParsISOTimestamp
//
//  go test -run=XXX -bench=.
//  go test -bench=. -benchmem -memprofile memprofile.out -cpuprofile cpuprofile.out
//  go tool pprof -http=:8080 memprofile.out
//  go tool pprof -http=:8080 cpuprofile.out

/*
	Interestingly, the naive implementation is faster than the official one. This
	implemenation does not do useful things like follow a log that is having lines
	added to it. That is a difficult thing to do cross-platform.

	$: time tail -30 sample/*.txt >/dev/null
	real	0m0.021s
	user	0m0.009s
	sys	    0m0.011s

	$: time ./tail -p -N -n 30 sample/*.txt >/dev/null
	real	0m0.005s
	user	0m0.002s
	sys	    0m0.002s
*/

const (
	bechmarkBytesPerOp int64 = 10
)

func init() {
}

// Get some lines
func TestGetLines(t *testing.T) {

	lines, total, err := getLines("sample/1.txt", false, false, 10)
	if err != nil {
		t.Fail()
	}
	t.Log("lines", lines, "total", total)
}

// go test -run=XXX -bench=. -benchmem
// BenchmarkGetLines-12    659.9 ns/op    15.15 MB/s    363 B/op    3 allocs/op
func BenchmarkGetLines(b *testing.B) {

	var lines []string
	var total int
	var err error

	b.SetBytes(bechmarkBytesPerOp)
	b.ReportAllocs()
	b.SetParallelism(30)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lines, total, err = getLines("sample/1.txt", false, false, 10)
		}
	})

	if len(lines) == 0 {
		b.Fail()
	}
	if total == 0 {
		b.Fail()
	}
	if err != nil {
		b.Fail()
	}
}

// BenchmarkPrintLines benchmark line printing with a path change every call
func BenchmarkPrintLines(b *testing.B) {
	printer := newPrinter()
	origOut := os.Stdout
	// Disable stdout for benchmark
	os.Stdout = nil

	b.SetBytes(bechmarkBytesPerOp)
	b.ReportAllocs()
	b.SetParallelism(30)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Change path on each run
			r := rand.Intn(100)
			s := fmt.Sprint(r)
			printer.print(s, "hello")
		}
	})

	// Re-enable stdout
	os.Stdout = origOut
}

// BenchmarkPrintLinesWithNoPathChange benchmark line printing with no path changes
func BenchmarkPrintLinesWithNoPathChange(b *testing.B) {
	printer := newPrinter()
	origOut := os.Stdout
	// Disable stdout for benchmark
	os.Stdout = nil

	b.SetBytes(bechmarkBytesPerOp)
	b.ReportAllocs()
	b.SetParallelism(30)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			printer.print("", "hello")
		}
	})

	// Re-enable stdout
	os.Stdout = origOut
}
