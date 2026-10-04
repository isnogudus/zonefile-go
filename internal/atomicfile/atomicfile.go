// Package atomicfile writes files so that readers see either the old or
// the new content, never a partial file.
package atomicfile

import (
	"os"
	"path/filepath"
)

// WriteFile writes data to a temporary file next to path and renames it
// over path.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Chmod(perm)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
