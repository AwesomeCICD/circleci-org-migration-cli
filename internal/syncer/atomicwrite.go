package syncer

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeSecretFile atomically replaces path with data at mode 0600: the content
// goes to a temp file in the same directory (created 0600), is fsynced, and is
// renamed over path, so a crash or full disk leaves either the old file or the
// new one, never a truncated mix. Used for files holding secrets or state that
// must not be lost (webhook secrets, the cutover state).
func writeSecretFile(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if err = f.Chmod(0o600); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if _, err = f.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err = f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	if err = os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	// Best effort: persist the rename itself.
	if d, derr := os.Open(dir); derr == nil { // #nosec G304 -- directory of the operator-chosen path
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// checkDirWritable reports whether a new file can be created next to path (the
// precondition for writeSecretFile) without leaving anything behind.
func checkDirWritable(path string) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".probe-*")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", path, err)
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}
