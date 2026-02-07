package model

import (
	"fmt"
	"regexp"
	"time"
)

var snapshotNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// Snapshot represents a persisted disk snapshot that can outlive sandboxes.
type Snapshot struct {
	ID                 string
	Name               string
	Path               string
	SourceSandboxID    string
	SourceSandboxName  string
	VirtualSizeBytes   int64
	AllocatedSizeBytes int64
	CreatedAt          time.Time
}

// Validate validates the snapshot model.
func (s Snapshot) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("snapshot id is required: %w", ErrNotValid)
	}

	if err := ValidateSnapshotName(s.Name); err != nil {
		return err
	}

	if s.Path == "" {
		return fmt.Errorf("snapshot path is required: %w", ErrNotValid)
	}

	if s.VirtualSizeBytes < 0 {
		return fmt.Errorf("virtual size cannot be negative: %w", ErrNotValid)
	}

	if s.AllocatedSizeBytes < 0 {
		return fmt.Errorf("allocated size cannot be negative: %w", ErrNotValid)
	}

	if s.CreatedAt.IsZero() {
		return fmt.Errorf("created at is required: %w", ErrNotValid)
	}

	return nil
}

// ValidateSnapshotName validates a snapshot friendly name.
func ValidateSnapshotName(name string) error {
	if name == "" {
		return fmt.Errorf("snapshot name is required: %w", ErrNotValid)
	}

	if !snapshotNameRegexp.MatchString(name) {
		return fmt.Errorf("snapshot name %q is invalid (allowed: [a-zA-Z0-9._-]): %w", name, ErrNotValid)
	}

	return nil
}
