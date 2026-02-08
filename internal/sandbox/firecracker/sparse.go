package firecracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func copyFileSparse(ctx context.Context, src, dst *os.File) error {
	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("could not stat source file: %w", err)
	}

	size := info.Size()
	if size == 0 {
		return nil
	}

	srcFD := int(src.Fd())
	offset := int64(0)
	buf := make([]byte, 1024*1024)

	for offset < size {
		if err := ctx.Err(); err != nil {
			return err
		}

		data, err := unix.Seek(srcFD, offset, unix.SEEK_DATA)
		if err != nil {
			if errors.Is(err, syscall.ENXIO) {
				break
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
			return fmt.Errorf("could not seek source data extent: %w", err)
		}
		if _, err := dst.Seek(data, io.SeekStart); err != nil {
			return fmt.Errorf("could not seek destination data extent: %w", err)
		}

		if err := copyNWithContext(ctx, dst, src, hole-data, buf); err != nil {
			return err
		}

		offset = hole
	}

	if err := dst.Truncate(size); err != nil {
		return fmt.Errorf("could not preserve sparse file virtual size: %w", err)
	}

	return nil
}

func copyFileRegular(ctx context.Context, src, dst *os.File) error {
	buf := make([]byte, 1024*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}

		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func copyNWithContext(ctx context.Context, dst io.Writer, src io.Reader, n int64, buf []byte) error {
	remaining := n
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
				if remaining == 0 {
					return nil
				}
				return io.ErrUnexpectedEOF
			}
			return err
		}
	}

	return nil
}

// snapshotSizeStats returns the virtual size and actual allocated size of a file.
// For sparse files, allocatedSize will be less than virtualSize.
func snapshotSizeStats(path string) (virtualSize int64, allocatedSize int64, err error) {
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
