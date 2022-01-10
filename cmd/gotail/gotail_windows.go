//go:build windows
// +build windows

package gotail

func setrlimit(limit uint64) (err error) {
	return nil
}
