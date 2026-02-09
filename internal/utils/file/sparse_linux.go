package file

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

// CopyFileSparse copies a file preserving sparse holes using SEEK_DATA/SEEK_HOLE.
// Only data extents are copied; holes remain as holes in the destination, preserving
// the sparse layout and keeping disk usage low.
//
// Returns an error wrapping ErrSparseUnsupported if SEEK_DATA/SEEK_HOLE is not
// supported, allowing the caller to fall back to a regular copy.
func CopyFileSparse(ctx context.Context, src, dst *os.File) error {
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	size := info.Size()
	if size == 0 {
		return nil
	}

	srcFD := int(src.Fd())

	// Probe whether SEEK_DATA is supported.
	if _, err := unix.Seek(srcFD, 0, unix.SEEK_DATA); err != nil {
		if isSeekDataUnsupported(err) {
			return fmt.Errorf("SEEK_DATA not supported: %w", ErrSparseUnsupported)
		}
		if errors.Is(err, syscall.ENXIO) {
			// Entire file is a hole — just set the size.
			return dst.Truncate(size)
		}
		return err
	}
	// Reset after probe.
	if _, err := unix.Seek(srcFD, 0, io.SeekStart); err != nil {
		return err
	}

	offset := int64(0)
	buf := make([]byte, 1024*1024)

	for offset < size {
		if err := ctx.Err(); err != nil {
			return err
		}

		data, err := unix.Seek(srcFD, offset, unix.SEEK_DATA)
		if err != nil {
			if errors.Is(err, syscall.ENXIO) {
				break // No more data extents.
			}
			return err
		}

		hole, err := unix.Seek(srcFD, data, unix.SEEK_HOLE)
		if err != nil {
			return err
		}
		if hole > size {
			hole = size
		}

		if _, err := src.Seek(data, io.SeekStart); err != nil {
			return fmt.Errorf("seeking source data extent: %w", err)
		}
		if _, err := dst.Seek(data, io.SeekStart); err != nil {
			return fmt.Errorf("seeking destination data extent: %w", err)
		}

		remaining := hole - data
		for remaining > 0 {
			if err := ctx.Err(); err != nil {
				return err
			}

			chunk := int64(len(buf))
			if remaining < chunk {
				chunk = remaining
			}
			rn, err := io.ReadFull(src, buf[:int(chunk)])
			if rn > 0 {
				if _, werr := dst.Write(buf[:rn]); werr != nil {
					return werr
				}
				remaining -= int64(rn)
			}
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					break
				}
				return err
			}
		}

		offset = hole
	}

	// Preserve the virtual size (important for sparse files).
	if err := dst.Truncate(size); err != nil {
		return fmt.Errorf("preserving sparse file virtual size: %w", err)
	}

	return nil
}

// SizeStats returns the virtual size and actual allocated size of a file.
// For sparse files, allocatedSize will be less than virtualSize.
func SizeStats(path string) (virtualSize int64, allocatedSize int64, err error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, 0, err
	}
	virtualSize = fi.Size()
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return virtualSize, virtualSize, nil
	}
	return virtualSize, st.Blocks * 512, nil
}

func isSeekDataUnsupported(err error) bool {
	return errors.Is(err, syscall.ENOSYS) ||
		errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP)
}
