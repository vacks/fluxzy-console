package pathutil

import (
	"os"
	"path/filepath"
	"strings"
)

// SafeUnder reports whether path is located inside root after cleaning both
// paths. It is used before deleting or opening data derived from user input.
func SafeUnder(path, root string) bool {
	cleanPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}
	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// RemoveIndexFiles removes a SQLite database and its sidecars. Deleting only
// the main file can leave WAL pages containing data that was meant to be
// purged.
func RemoveIndexFiles(path string) {
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		_ = os.Remove(path + suffix)
	}
}
