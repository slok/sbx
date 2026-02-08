package sbx_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	intsbx "github.com/slok/sbx/test/integration/sbx"
)

// imageListItem matches the JSON output of `sbx image list --format json`.
type imageListItem struct {
	Version   string `json:"version"`
	Installed bool   `json:"installed"`
}

// imageManifestOutput matches the JSON output of `sbx image inspect --format json`.
type imageManifestOutput struct {
	Version     string                        `json:"version"`
	Artifacts   map[string]imageArchArtifacts `json:"artifacts"`
	Firecracker imageFirecrackerInfo          `json:"firecracker"`
	Build       imageBuildInfo                `json:"build"`
}

type imageArchArtifacts struct {
	Kernel imageKernelInfo `json:"kernel"`
	Rootfs imageRootfsInfo `json:"rootfs"`
}

type imageKernelInfo struct {
	File      string `json:"file"`
	Version   string `json:"version"`
	Source    string `json:"source"`
	SizeBytes int64  `json:"size_bytes"`
}

type imageRootfsInfo struct {
	File          string `json:"file"`
	Distro        string `json:"distro"`
	DistroVersion string `json:"distro_version"`
	Profile       string `json:"profile"`
	SizeBytes     int64  `json:"size_bytes"`
}

type imageFirecrackerInfo struct {
	Version string `json:"version"`
	Source  string `json:"source"`
}

type imageBuildInfo struct {
	Date   string `json:"date"`
	Commit string `json:"commit"`
}

func parseImageList(t *testing.T, data []byte) []imageListItem {
	t.Helper()
	var items []imageListItem
	require.NoError(t, json.Unmarshal(data, &items))
	return items
}

func findImageInList(items []imageListItem, version string) *imageListItem {
	for _, item := range items {
		if item.Version == version {
			return &item
		}
	}
	return nil
}

// TestImageListAndInspect tests listing remote releases and inspecting a manifest.
// This requires network access to GitHub API.
func TestImageListAndInspect(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	imagesDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	imgArgs := "--images-dir " + imagesDir

	// 1. List image releases - should show at least v0.1.0-rc.1 from slok/sbx-images.
	stdout, stderr, err := intsbx.RunImageList(ctx, config, dbPath, imgArgs)
	require.NoError(t, err, "image list failed: stdout=%s stderr=%s", stdout, stderr)

	items := parseImageList(t, stdout)
	require.NotEmpty(t, items, "expected at least one release from slok/sbx-images")

	found := findImageInList(items, "v0.1.0-rc.1")
	require.NotNil(t, found, "v0.1.0-rc.1 should be in the release list")
	assert.False(t, found.Installed, "v0.1.0-rc.1 should not be installed yet")

	// 2. Inspect the release manifest.
	stdout, stderr, err = intsbx.RunImageInspect(ctx, config, dbPath, "v0.1.0-rc.1", "")
	require.NoError(t, err, "image inspect failed: stdout=%s stderr=%s", stdout, stderr)

	var manifest imageManifestOutput
	require.NoError(t, json.Unmarshal(stdout, &manifest))
	assert.Equal(t, "v0.1.0-rc.1", manifest.Version)
	assert.NotEmpty(t, manifest.Firecracker.Version)
	assert.NotEmpty(t, manifest.Build.Commit)

	arch, ok := manifest.Artifacts["x86_64"]
	require.True(t, ok, "expected x86_64 artifacts in manifest")
	assert.NotEmpty(t, arch.Kernel.File)
	assert.NotEmpty(t, arch.Rootfs.File)
	assert.Greater(t, arch.Kernel.SizeBytes, int64(0))
	assert.Greater(t, arch.Rootfs.SizeBytes, int64(0))
}

// TestImagePullAndRemove tests the full pull/list-installed/rm cycle.
// This downloads real artifacts from GitHub, so it can be slow.
func TestImagePullAndRemove(t *testing.T) {
	config := intsbx.NewConfig(t)
	dbPath := newTestDB(t)
	imagesDir := t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	imgArgs := "--images-dir " + imagesDir
	version := "v0.1.0-rc.1"

	// Cleanup on failure.
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanCancel()
		_, _, _ = intsbx.RunImageRm(cleanCtx, config, dbPath, version, imgArgs)
	})

	// 1. Pull the image.
	stdout, stderr, err := intsbx.RunImagePull(ctx, config, dbPath, version, imgArgs)
	require.NoError(t, err, "image pull failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "Successfully pulled image")

	// 2. List should show it as installed.
	stdout, stderr, err = intsbx.RunImageList(ctx, config, dbPath, imgArgs)
	require.NoError(t, err, "image list after pull failed: stdout=%s stderr=%s", stdout, stderr)
	items := parseImageList(t, stdout)
	found := findImageInList(items, version)
	require.NotNil(t, found, "%s should be in the list after pull", version)
	assert.True(t, found.Installed, "%s should be marked as installed", version)

	// 3. Pull again without --force should skip.
	stdout, stderr, err = intsbx.RunImagePull(ctx, config, dbPath, version, imgArgs)
	require.NoError(t, err, "image pull (skip) failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "already installed")

	// 4. Remove the image.
	stdout, stderr, err = intsbx.RunImageRm(ctx, config, dbPath, version, imgArgs)
	require.NoError(t, err, "image rm failed: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, string(stdout), "Removed image")

	// 5. List should show it as not installed.
	stdout, stderr, err = intsbx.RunImageList(ctx, config, dbPath, imgArgs)
	require.NoError(t, err, "image list after rm failed: stdout=%s stderr=%s", stdout, stderr)
	items = parseImageList(t, stdout)
	found = findImageInList(items, version)
	if found != nil {
		assert.False(t, found.Installed, "%s should not be installed after rm", version)
	}
}
