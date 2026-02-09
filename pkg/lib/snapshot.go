package lib

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/slok/sbx/internal/app/snapshotcreate"
)

// CreateImageFromSandboxOpts configures snapshot image creation.
//
// Pass nil to [Client.CreateImageFromSandbox] to auto-generate the image name.
type CreateImageFromSandboxOpts struct {
	// ImageName is an optional name for the snapshot image.
	// If empty, a name is auto-generated from the sandbox name and timestamp.
	ImageName string
}

// CreateImageFromSandbox creates a local snapshot image from a sandbox.
//
// The sandbox must be in [SandboxStatusStopped] state.
// The resulting image can be used with [CreateSandboxOpts].FromImage to create
// new sandboxes.
//
// Pass nil opts to auto-generate an image name from the sandbox name and
// timestamp. Use opts.ImageName to specify a custom name.
//
// Returns the image name, or [ErrNotFound] if the sandbox does not exist,
// [ErrNotValid] if the sandbox is running, or [ErrAlreadyExists] if the
// image name is taken.
func (c *Client) CreateImageFromSandbox(ctx context.Context, nameOrID string, opts *CreateImageFromSandboxOpts) (string, error) {
	imgMgr, err := c.newLocalImageManager()
	if err != nil {
		return "", fmt.Errorf("could not create image manager: %w", err)
	}

	snapCrt, err := c.newSnapshotCreator()
	if err != nil {
		return "", fmt.Errorf("could not create snapshot creator: %w", err)
	}

	// Determine data dir.
	dataDir := c.dataDir
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("could not get user home dir: %w", err)
		}
		dataDir = filepath.Join(home, ".sbx")
	}

	svc, err := snapshotcreate.NewService(snapshotcreate.ServiceConfig{
		ImageManager:    imgMgr,
		SnapshotCreator: snapCrt,
		Repository:      c.repo,
		Logger:          c.logger,
		DataDir:         dataDir,
	})
	if err != nil {
		return "", fmt.Errorf("could not create service: %w", err)
	}

	var imgName string
	if opts != nil {
		imgName = opts.ImageName
	}

	result, err := svc.Run(ctx, snapshotcreate.Request{
		NameOrID:  nameOrID,
		ImageName: imgName,
	})
	if err != nil {
		return "", mapError(err)
	}

	return result, nil
}
