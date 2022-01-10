package input

import (
	"testing"
)

var sampleDir = "../../../sample"

const (
	bechmarkBytesPerOp int64 = 10
)

// Get some lines
func TestGetLines(t *testing.T) {
	lines, total, err := GetLines(sampleDir+"/1.txt", false, false, 10)
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
	// Change rlimit prior to trying to run with this level of parallelism
	b.SetParallelism(30)
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			lines, total, err = GetLines(sampleDir+"/1.txt", false, false, 10)
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
