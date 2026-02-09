// Package file provides file utility functions including sparse-aware file copying.
package file

import "errors"

// ErrSparseUnsupported is returned when the filesystem or kernel does not support
// SEEK_DATA/SEEK_HOLE for sparse-aware file copying.
var ErrSparseUnsupported = errors.New("sparse copy not supported")
