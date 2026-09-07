package effect

import (
	"io/fs"
)

// IOOperations is the standard capability surface for programs whose typed
// failures are IOError. Embedded Operations supplies clock and logging effects.
type IOOperations[R any] struct {
	Operations[R, IOError]
}

// IO constructs standard operations for a program with no application
// environment and IOError as its expected-error channel.
func IO() IOOperations[Unit] {
	return IOFor[Unit]()
}

// IOFor constructs standard operations for an application environment R and
// IOError as its expected-error channel.
func IOFor[R any]() IOOperations[R] {
	return IOOperations[R]{Operations: For[R, IOError]()}
}

func (IOOperations[R]) ReadFile(path string) Effect[R, IOError, []byte] {
	return ReadFile[R](path)
}

func (IOOperations[R]) WriteFile(path string, data []byte, permissions fs.FileMode) Effect[R, IOError, Unit] {
	return WriteFile[R](path, data, permissions)
}

func (IOOperations[R]) Stat(path string) Effect[R, IOError, fs.FileInfo] {
	return Stat[R](path)
}

func (IOOperations[R]) ReadDir(path string) Effect[R, IOError, []fs.DirEntry] {
	return ReadDir[R](path)
}

func (IOOperations[R]) MkdirAll(path string, permissions fs.FileMode) Effect[R, IOError, Unit] {
	return MkdirAll[R](path, permissions)
}

func (IOOperations[R]) Remove(path string) Effect[R, IOError, Unit] {
	return Remove[R](path)
}

func (IOOperations[R]) Rename(oldPath string, newPath string) Effect[R, IOError, Unit] {
	return Rename[R](oldPath, newPath)
}
