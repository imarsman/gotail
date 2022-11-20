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

/*
	Interestingly, this implementation is faster than the official one. It tends
	to use more CPU (0.1 vs 0.0) and is much larger though.

	$: time tail -30 sample/*.txt >/dev/null
	real	0m0.031s
	user	0m0.010s
	sys	    0m0.014s

	$: time ./tail -n 30 sample/*.txt >/dev/null
	real	0m0.006s
	user	0m0.002s
	sys	    0m0.003s

	Native tail does slightly better than this tail with stdin

	$: time cat sample/1.txt|tail -n 10 >/dev/null
	real    0m0.003s

	$: time cat sample/1.txt|gotail -n 10 >/dev/null
	real    0m0.006s
*/

const (
	bechmarkBytesPerOp int64 = 10
)

func init() {
}

func TestRLimit(t *testing.T) {
	t.Logf("Limit %+v", setrlimit(1000))
}

// func TestJSONLine(t *testing.T) {
// 	line := `Nov 19 21:19:19 c1 nomad-firehose: {"Name":"997b2ae0-4640-40c4-b776-a878c969135c.5ea0d3cb-7f0d-49c2-bd7e-e1321d8557aa[2]","NodeID":"84cb91a8-aec0-03d0-bd2b-c35422f32066","AllocationID":"072c47d4-557e-1ab5-3f8b-f46dcaec2d09","DesiredStatus":"run","DesiredDescription":"","ClientStatus":"running","ClientDescription":"Tasks are running","JobID":"997b2ae0-4640-40c4-b776-a878c969135c","GroupName":"5ea0d3cb-7f0d-49c2-bd7e-e1321d8557aa","TaskName":"virtual_machine","EvalID":"0b888438-1621-631e-bf29-24cbdf4a8983","TaskState":"running","TaskFailed":false,"TaskStartedAt":"2022-11-19T21:19:19.60062168Z","TaskFinishedAt":"0001-01-01T00:00:00Z","TaskEvent":{"Type":"Started","Time":1668892759600613277,"DisplayMessage":"Task started by client","Details":{},"FailsTask":false,"RestartReason":"","SetupError":"","DriverError":"","DriverMessage":"","ExitCode":0,"Signal":0,"Message":"","KillReason":"","KillTimeout":0,"KillError":"","StartDelay":0,"DownloadError":"","ValidationError":"","DiskLimit":0,"DiskSize":0,"FailedSibling":"","VaultError":"","TaskSignalReason":"","TaskSignal":"","GenericSource":""}}`

// 	ok, jl := GetContent(line)
// 	t.Log("ok", ok)
// 	t.Logf("PREFIX %s JSON %s", jl.prefix, jl.json)

// 	line = `Nov 19 21:19:20 c1 nomad[18222]:     2022-11-19T21:19:20.354Z [DEBUG] http: request complete: method=GET path=/v1/allocations?`

// 	ok, jl = GetContent(line)
// 	t.Log("ok", ok)
// 	t.Logf("PREFIX %s JSON %s", jl.prefix, jl.json)
// }
