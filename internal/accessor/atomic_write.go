package accessor

import (
	"fmt"
	"os"
	"path/filepath"
)

// syncFile is a seam so tests can simulate a failure (crash, full disk)
// after the new contents are written but before they replace the original.
var syncFile = func(f *os.File) error { return f.Sync() }

// writeFileAtomic replaces path with data so that readers only ever see the
// old contents or the complete new contents. It writes to a temp file in the
// same directory, fsyncs it, then renames it over the original; a crash or
// full disk mid-write leaves the original file untouched.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err = syncFile(tmp); err != nil {
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err = os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}

	// Persist the rename itself. Directories can't be opened for sync on
	// Windows, so this is best effort.
	if d, openErr := os.Open(dir); openErr == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}
