package lib

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/slok/sbx/internal/app/snapshotcreate"
	"github.com/slok/sbx/internal/app/snapshotlist"
	"github.com/slok/sbx/internal/app/snapshotremove"
	"github.com/slok/sbx/internal/sandbox/firecracker"
)

// CreateSnapshot creates a point-in-time rootfs snapshot of a sandbox.
//
// The sandbox must be in [SandboxStatusCreated] or [SandboxStatusStopped] state.
// Snapshots can later be used to create new sandboxes via
// [CreateSandboxOpts].FromSnapshot.
//
// Pass nil opts to auto-generate a snapshot name from the sandbox name and
// timestamp. Use opts.SnapshotName to specify a custom name.
//
// Returns [ErrNotFound] if the sandbox does not exist, [ErrNotValid] if the
// sandbox is running, or [ErrAlreadyExists] if the snapshot name is taken.
func (c *Client) CreateSnapshot(ctx context.Context, nameOrID string, opts *CreateSnapshotOpts) (*Snapshot, error) {
	sb, err := c.getInternalSandbox(ctx, nameOrID)
	if err != nil {
		return nil, mapError(err)
	}

	eng, err := c.newEngine(sb.Config)
	if err != nil {
		return nil, mapError(fmt.Errorf("could not create engine: %w", err))
	}

	svc, err := snapshotcreate.NewService(snapshotcreate.ServiceConfig{
		Engine:       eng,
		Repository:   c.repo,
		Logger:       c.logger,
		SnapshotsDir: filepath.Join(c.dataDir, firecracker.SnapshotsDir),
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	var snapshotName string
	if opts != nil {
		snapshotName = opts.SnapshotName
	}

	result, err := svc.Run(ctx, snapshotcreate.Request{
		NameOrID:     nameOrID,
		SnapshotName: snapshotName,
	})
	if err != nil {
		return nil, mapError(err)
	}

	out := fromInternalSnapshot(*result)
	return &out, nil
}

// ListSnapshots returns all snapshots ordered by creation time (newest first).
func (c *Client) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	svc, err := snapshotlist.NewService(snapshotlist.ServiceConfig{
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, snapshotlist.Request{})
	if err != nil {
		return nil, mapError(err)
	}

	return fromInternalSnapshotList(result), nil
}

// RemoveSnapshot deletes a snapshot by name or ID.
//
// The snapshot file is removed from disk and the record is deleted from storage.
//
// Returns [ErrNotFound] if the snapshot does not exist.
func (c *Client) RemoveSnapshot(ctx context.Context, nameOrID string) (*Snapshot, error) {
	svc, err := snapshotremove.NewService(snapshotremove.ServiceConfig{
		Repository: c.repo,
		Logger:     c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, snapshotremove.Request{
		NameOrID: nameOrID,
	})
	if err != nil {
		return nil, mapError(err)
	}

	out := fromInternalSnapshot(*result)
	return &out, nil
}
