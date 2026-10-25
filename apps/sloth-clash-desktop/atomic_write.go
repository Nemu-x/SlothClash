package main

import (
	"os"

	"SlothClashDesktop/internal/fsutil"
)

// atomicWriteFile is fsutil.AtomicWriteFile: a crash-safe temp-file + rename
// write (see there for why every persisted file goes through it).
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	return fsutil.AtomicWriteFile(path, data, perm)
}
