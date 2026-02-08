package lib

import (
	"context"
	"fmt"

	"github.com/slok/sbx/internal/app/imageinspect"
	"github.com/slok/sbx/internal/app/imagelist"
	"github.com/slok/sbx/internal/app/imagepull"
	"github.com/slok/sbx/internal/app/imagerm"
	"github.com/slok/sbx/internal/image"
)

// ListImages returns available image releases from the registry.
//
// Each release indicates whether it is installed locally. Use [Client.PullImage]
// to download a release.
func (c *Client) ListImages(ctx context.Context) ([]ImageRelease, error) {
	mgr, err := c.newImageManager()
	if err != nil {
		return nil, fmt.Errorf("could not create image manager: %w", err)
	}

	svc, err := imagelist.NewService(imagelist.ServiceConfig{
		Manager: mgr,
		Logger:  c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx)
	if err != nil {
		return nil, mapError(err)
	}

	return fromInternalImageReleaseList(result), nil
}

// PullImage downloads an image release (kernel, rootfs, firecracker binary).
//
// Pass nil opts for defaults (no force, silent). Use opts.Force to re-download
// even if already installed. Use opts.StatusWriter to receive progress output.
//
// The returned [PullResult] contains local paths to the downloaded artifacts.
func (c *Client) PullImage(ctx context.Context, version string, opts *PullImageOpts) (*PullResult, error) {
	mgr, err := c.newImageManager()
	if err != nil {
		return nil, fmt.Errorf("could not create image manager: %w", err)
	}

	svc, err := imagepull.NewService(imagepull.ServiceConfig{
		Manager: mgr,
		Logger:  c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	pullOpts := imagepull.Request{
		Version: version,
	}
	if opts != nil {
		pullOpts.Force = opts.Force
		pullOpts.StatusWriter = opts.StatusWriter
	}

	result, err := svc.Run(ctx, pullOpts)
	if err != nil {
		return nil, mapError(err)
	}

	return &PullResult{
		Version:         result.Version,
		Skipped:         result.Skipped,
		KernelPath:      result.KernelPath,
		RootFSPath:      result.RootFSPath,
		FirecrackerPath: result.FirecrackerPath,
	}, nil
}

// RemoveImage deletes a locally installed image release.
//
// This removes all downloaded artifacts (kernel, rootfs, firecracker binary)
// for the given version.
func (c *Client) RemoveImage(ctx context.Context, version string) error {
	mgr, err := c.newImageManager()
	if err != nil {
		return fmt.Errorf("could not create image manager: %w", err)
	}

	svc, err := imagerm.NewService(imagerm.ServiceConfig{
		Manager: mgr,
		Logger:  c.logger,
	})
	if err != nil {
		return fmt.Errorf("could not create service: %w", err)
	}

	if err := svc.Run(ctx, imagerm.Request{Version: version}); err != nil {
		return mapError(err)
	}

	return nil
}

// InspectImage returns the manifest for a locally installed image release.
//
// The manifest contains artifact metadata, Firecracker version info, and
// build details for all supported architectures.
func (c *Client) InspectImage(ctx context.Context, version string) (*ImageManifest, error) {
	mgr, err := c.newImageManager()
	if err != nil {
		return nil, fmt.Errorf("could not create image manager: %w", err)
	}

	svc, err := imageinspect.NewService(imageinspect.ServiceConfig{
		Manager: mgr,
		Logger:  c.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("could not create service: %w", err)
	}

	result, err := svc.Run(ctx, imageinspect.Request{Version: version})
	if err != nil {
		return nil, mapError(err)
	}

	return fromInternalImageManifest(result), nil
}

// newImageManager creates the image manager for image operations.
func (c *Client) newImageManager() (image.ImageManager, error) {
	return image.NewGitHubImageManager(image.GitHubImageManagerConfig{
		Repo:      c.imageRepo,
		ImagesDir: c.imagesDir,
		Logger:    c.logger,
	})
}
