// Package fileio holds the writing machinery poo's file tools share.
//
// A tool reserves a path before it writes and releases it after, so two
// calls in one process cannot hold the same file. The write itself goes
// through a temporary file and a rename, so a crash or a full disk
// leaves the old file in place rather than a partial one.
package fileio

import (
	"os"
	"path/filepath"
	"sync"
)

// Defaults for the tools that write files.
const (
	// FileMode is the mode a created file gets.
	FileMode = 0o644
	// DirMode is the mode a created directory gets.
	DirMode = 0o755
)

// reservations holds each path a call is writing, from its reserve to
// its release. It covers this process's file tools, not writes by other
// programs.
var reservations sync.Map

// Reserve marks a path as held, and reports whether the call may go on:
// a false return means another call holds the path.
func Reserve(path string) bool {
	_, busy := reservations.LoadOrStore(path, struct{}{})
	return !busy
}

// Release frees a path a Reserve held.
func Release(path string) {
	reservations.Delete(path)
}

// Resolve returns the path a call works on. A relative path is taken
// from the working directory of the run.
func Resolve(path, workingDir string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(workingDir, path)
}

// Write creates a new file with content, making the directories the
// path needs.
func Write(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return err
	}
	return WriteAtomic(path, content, FileMode)
}

// WriteAtomic writes a file through a temporary file and a rename in the
// same directory, so a crash or a full disk leaves the old file in place
// rather than a partial one. The mode is the file's own.
func WriteAtomic(path, content string, mode os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".poo-write-*")
	if err != nil {
		return err
	}
	temp := f.Name()
	cleanup := func() {
		f.Close()
		os.Remove(temp)
	}
	if _, err := f.WriteString(content); err != nil {
		cleanup()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(temp)
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}
	return nil
}
