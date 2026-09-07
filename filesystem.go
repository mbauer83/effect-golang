package effect

import (
	"bytes"
	"context"
	"io/fs"

	runtimecore "github.com/mbauer83/effect-golang/internal/runtime"
)

type fileOperation struct {
	name       IOOperation
	path       string
	targetPath string
}

func filesystemEffect[R, A any](operation fileOperation, execute func(context.Context, FileSystem) (A, error)) Effect[R, IOError, A] {
	return fromRuntime(func(ctx context.Context, state *runtimecore.State, _ R) Exit[IOError, A] {
		value, err := execute(ctx, state.Capabilities().FileSystem)
		if exit, interrupted := interruptedExit[IOError, A](ctx); interrupted {
			return exit
		}
		if err != nil {
			return ExitFailure[IOError, A](IOError{
				Operation:  operation.name,
				Path:       operation.path,
				TargetPath: operation.targetPath,
				Err:        err,
			})
		}
		return ExitSuccess[IOError](value)
	})
}

// ReadFile reads a complete file through the runtime FileSystem.
func ReadFile[R any](path string) Effect[R, IOError, []byte] {
	return filesystemEffect[R](fileOperation{name: IOReadFile, path: path}, func(ctx context.Context, fileSystem FileSystem) ([]byte, error) {
		return fileSystem.ReadFile(ctx, path)
	})
}

// WriteFile writes a complete file through the runtime FileSystem. The input is
// copied when the effect is constructed so later caller mutation cannot alter it.
func WriteFile[R any](path string, data []byte, permissions fs.FileMode) Effect[R, IOError, Unit] {
	ownedData := bytes.Clone(data)
	return filesystemEffect[R](fileOperation{name: IOWriteFile, path: path}, func(ctx context.Context, fileSystem FileSystem) (Unit, error) {
		return Unit{}, fileSystem.WriteFile(ctx, path, ownedData, permissions)
	})
}

// Stat reads file metadata through the runtime FileSystem.
func Stat[R any](path string) Effect[R, IOError, fs.FileInfo] {
	return filesystemEffect[R](fileOperation{name: IOStat, path: path}, func(ctx context.Context, fileSystem FileSystem) (fs.FileInfo, error) {
		return fileSystem.Stat(ctx, path)
	})
}

// ReadDir reads directory entries through the runtime FileSystem.
func ReadDir[R any](path string) Effect[R, IOError, []fs.DirEntry] {
	return filesystemEffect[R](fileOperation{name: IOReadDir, path: path}, func(ctx context.Context, fileSystem FileSystem) ([]fs.DirEntry, error) {
		return fileSystem.ReadDir(ctx, path)
	})
}

// MkdirAll creates a directory tree through the runtime FileSystem.
func MkdirAll[R any](path string, permissions fs.FileMode) Effect[R, IOError, Unit] {
	return filesystemEffect[R](fileOperation{name: IOMkdirAll, path: path}, func(ctx context.Context, fileSystem FileSystem) (Unit, error) {
		return Unit{}, fileSystem.MkdirAll(ctx, path, permissions)
	})
}

// Remove removes one path through the runtime FileSystem.
func Remove[R any](path string) Effect[R, IOError, Unit] {
	return filesystemEffect[R](fileOperation{name: IORemove, path: path}, func(ctx context.Context, fileSystem FileSystem) (Unit, error) {
		return Unit{}, fileSystem.Remove(ctx, path)
	})
}

// Rename renames a path through the runtime FileSystem.
func Rename[R any](oldPath string, newPath string) Effect[R, IOError, Unit] {
	operation := fileOperation{name: IORename, path: oldPath, targetPath: newPath}
	return filesystemEffect[R](operation, func(ctx context.Context, fileSystem FileSystem) (Unit, error) {
		return Unit{}, fileSystem.Rename(ctx, oldPath, newPath)
	})
}
