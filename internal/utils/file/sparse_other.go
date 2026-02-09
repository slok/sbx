//go:build !linux

package file

import (
	"context"
	"fmt"
	"os"
)

// CopyFileSparse is not supported on non-Linux platforms.
func CopyFileSparse(_ context.Context, _, _ *os.File) error {
	return fmt.Errorf("not available on this platform: %w", ErrSparseUnsupported)
}

// SizeStats returns the virtual size and actual allocated size of a file.
// On non-Linux platforms, allocated size is reported as equal to virtual size.
func SizeStats(path string) (virtualSize int64, allocatedSize int64, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	return fi.Size(), fi.Size(), nil
}
