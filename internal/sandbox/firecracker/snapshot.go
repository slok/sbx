package firecracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/slok/sbx/internal/model"
)

// CreateSnapshot creates a rootfs snapshot for an existing sandbox.
func (e *Engine) CreateSnapshot(ctx context.Context, sandboxID string, snapshotID string, dstPath string) (virtualSize int64, allocatedSize int64, err error) {
	if sandboxID == "" {
		return 0, 0, fmt.Errorf("sandbox id cannot be empty: %w", model.ErrNotValid)
	}

	if snapshotID == "" {
		return 0, 0, fmt.Errorf("snapshot id cannot be empty: %w", model.ErrNotValid)
	}

	if dstPath == "" {
		return 0, 0, fmt.Errorf("destination path cannot be empty: %w", model.ErrNotValid)
	}

	srcPath := e.RootFSPath(e.VMDir(sandboxID))
	if _, err := os.Stat(srcPath); err != nil {
		if os.IsNotExist(err) {
			return 0, 0, fmt.Errorf("sandbox %s rootfs not found at %s: %w", sandboxID, srcPath, model.ErrNotFound)
		}
		return 0, 0, fmt.Errorf("could not stat sandbox rootfs: %w", err)
	}

	if _, err := os.Stat(dstPath); err == nil {
		return 0, 0, fmt.Errorf("destination snapshot file %q already exists: %w", dstPath, model.ErrAlreadyExists)
	}

	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return 0, 0, fmt.Errorf("could not create destination directory: %w", err)
	}

	srcFile, err := os.Open(srcPath)
	if err != nil {
		return 0, 0, fmt.Errorf("could not open source rootfs: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dstPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if os.IsExist(err) {
			return 0, 0, fmt.Errorf("destination snapshot file %q already exists: %w", dstPath, model.ErrAlreadyExists)
		}
		return 0, 0, fmt.Errorf("could not create destination snapshot file: %w", err)
	}

	copyErr := e.copySparseAware(ctx, srcFile, dstFile)
	closeErr := dstFile.Close()

	if copyErr != nil {
		_ = os.Remove(dstPath)
		return 0, 0, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(dstPath)
		return 0, 0, fmt.Errorf("could not close destination snapshot file: %w", closeErr)
	}

	virtualSize, allocatedSize, err = snapshotSizeStats(dstPath)
	if err != nil {
		return 0, 0, fmt.Errorf("could not get snapshot size stats: %w", err)
	}

	e.logger.Infof("Created snapshot %s for sandbox %s at %s", snapshotID, sandboxID, dstPath)
	return virtualSize, allocatedSize, nil
}

func (e *Engine) copySparseAware(ctx context.Context, src, dst *os.File) error {
	if err := copyFileSparse(ctx, src, dst); err != nil {
		if isSeekDataUnsupported(err) {
			if _, seekErr := src.Seek(0, io.SeekStart); seekErr != nil {
				return fmt.Errorf("could not seek source file before fallback copy: %w", seekErr)
			}
			if _, seekErr := dst.Seek(0, io.SeekStart); seekErr != nil {
				return fmt.Errorf("could not seek destination file before fallback copy: %w", seekErr)
			}

			e.logger.Debugf("Sparse copy unsupported by filesystem/kernel, using regular copy fallback")
			if err := copyFileRegular(ctx, src, dst); err != nil {
				return fmt.Errorf("could not copy rootfs snapshot: %w", err)
			}

			return dst.Sync()
		}

		return fmt.Errorf("could not copy rootfs snapshot: %w", err)
	}

	if err := dst.Sync(); err != nil {
		return fmt.Errorf("could not sync snapshot file: %w", err)
	}

	return nil
}

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

func isSeekDataUnsupported(err error) bool {
	return errors.Is(err, syscall.ENOSYS) ||
		errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP)
}

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
