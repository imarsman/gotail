package main

import (
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

const (
	bechmarkBytesPerOp int64 = 10
)

func init() {
}

// Get some lines
func TestGetLines(t *testing.T) {

	lines, err := getLines(10, "sample/1.txt")
	if err != nil {
		t.Fail()
	}
	t.Log("lines", lines)
}

// go test -run=XXX -bench=. -benchmem
// BenchmarkGetLines-12    659.9 ns/op    15.15 MB/s    363 B/op    3 allocs/op
func BenchmarkGetLines(b *testing.B) {

	var lines []string
	var err error

	b.SetBytes(bechmarkBytesPerOp)
	b.ReportAllocs()
	b.SetParallelism(30)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lines, err = getLines(10, "sample/1.txt")
		}
	})

	if len(lines) == 0 {
		b.Fail()
	}
	if err != nil {
		b.Fail()
	}
}
