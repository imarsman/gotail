//go:build windows
// +build windows

package main

func setrlimit(limit uint64) (err error) {
	return nil
}
