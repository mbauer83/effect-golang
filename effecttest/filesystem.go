package effecttest

import (
	"context"
	"errors"
	"io/fs"
)

// ErrUnimplementedOperation is returned by an unset FileSystemStub operation.
var ErrUnimplementedOperation = errors.New("effecttest: filesystem operation is not implemented")

// FileSystemStub is an explicit function-based filesystem test adapter.
type FileSystemStub struct {
	ReadFileFunc  func(context.Context, string) ([]byte, error)
	WriteFileFunc func(context.Context, string, []byte, fs.FileMode) error
	StatFunc      func(context.Context, string) (fs.FileInfo, error)
	ReadDirFunc   func(context.Context, string) ([]fs.DirEntry, error)
	MkdirAllFunc  func(context.Context, string, fs.FileMode) error
	RemoveFunc    func(context.Context, string) error
	RenameFunc    func(context.Context, string, string) error
}

func (stub FileSystemStub) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if stub.ReadFileFunc == nil {
		return nil, ErrUnimplementedOperation
	}
	return stub.ReadFileFunc(ctx, path)
}

func (stub FileSystemStub) WriteFile(ctx context.Context, path string, data []byte, permissions fs.FileMode) error {
	if stub.WriteFileFunc == nil {
		return ErrUnimplementedOperation
	}
	return stub.WriteFileFunc(ctx, path, data, permissions)
}

func (stub FileSystemStub) Stat(ctx context.Context, path string) (fs.FileInfo, error) {
	if stub.StatFunc == nil {
		return nil, ErrUnimplementedOperation
	}
	return stub.StatFunc(ctx, path)
}

func (stub FileSystemStub) ReadDir(ctx context.Context, path string) ([]fs.DirEntry, error) {
	if stub.ReadDirFunc == nil {
		return nil, ErrUnimplementedOperation
	}
	return stub.ReadDirFunc(ctx, path)
}

func (stub FileSystemStub) MkdirAll(ctx context.Context, path string, permissions fs.FileMode) error {
	if stub.MkdirAllFunc == nil {
		return ErrUnimplementedOperation
	}
	return stub.MkdirAllFunc(ctx, path, permissions)
}

func (stub FileSystemStub) Remove(ctx context.Context, path string) error {
	if stub.RemoveFunc == nil {
		return ErrUnimplementedOperation
	}
	return stub.RemoveFunc(ctx, path)
}

func (stub FileSystemStub) Rename(ctx context.Context, oldPath string, newPath string) error {
	if stub.RenameFunc == nil {
		return ErrUnimplementedOperation
	}
	return stub.RenameFunc(ctx, oldPath, newPath)
}
