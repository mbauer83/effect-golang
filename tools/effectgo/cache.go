package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "effectgo")
	return dir, os.MkdirAll(dir, 0o755)
}

// store writes content under a name derived from it, so a file rewritten the
// same way twice is written once and an overlay never names a stale file.
func store(dir, name string, content []byte) (string, error) {
	sum := sha256.Sum256(append([]byte(name+"\x00"), content...))
	target := filepath.Join(dir, hex.EncodeToString(sum[:12])+"-"+filepath.Base(name))
	if _, err := os.Stat(target); err == nil {
		return target, nil
	}
	temporary, err := os.CreateTemp(dir, "writing-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temporary.Name())
	if _, err := temporary.Write(content); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	return target, os.Rename(temporary.Name(), target)
}
