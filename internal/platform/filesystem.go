package platform

import (
	"context"
	"io/fs"
	"os"
)

// LiveFileSystem delegates filesystem operations to os after a cooperative
// cancellation checkpoint.
type LiveFileSystem struct{}

func (LiveFileSystem) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (LiveFileSystem) WriteFile(ctx context.Context, path string, data []byte, permissions fs.FileMode) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return os.WriteFile(path, data, permissions)
}

func (LiveFileSystem) Stat(ctx context.Context, path string) (fs.FileInfo, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return os.Stat(path)
}

func (LiveFileSystem) ReadDir(ctx context.Context, path string) ([]fs.DirEntry, error) {
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	return os.ReadDir(path)
}

func (LiveFileSystem) MkdirAll(ctx context.Context, path string, permissions fs.FileMode) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return os.MkdirAll(path, permissions)
}

func (LiveFileSystem) Remove(ctx context.Context, path string) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return os.Remove(path)
}

func (LiveFileSystem) Rename(ctx context.Context, oldPath string, newPath string) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return os.Rename(oldPath, newPath)
}
