// Package fsutil writes files atomically so that readers (Prometheus, the
// textfile collector, Caddy) never see a half-written file.
//
// Atomic writes replace the file (new inode): containers must therefore mount
// the parent directory, never the single file, or they keep reading the old one.
package fsutil

import (
	"bytes"
	"os"
	"path/filepath"
)

// WriteFile writes data to path through a temporary file and a rename. It
// creates parent directories and reports whether the content changed.
func WriteFile(path string, data []byte, perm os.FileMode) (changed bool, err error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		if st, err := os.Stat(path); err == nil && st.Mode().Perm() != perm {
			return false, os.Chmod(path, perm)
		}
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return false, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(name, path)
}

// Exists reports whether path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
