package image_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/model"
)

func newTestLocalManager(t *testing.T) (*image.LocalImageManager, string) {
	t.Helper()
	imagesDir := t.TempDir()
	m, err := image.NewLocalImageManager(image.LocalImageManagerConfig{
		ImagesDir: imagesDir,
	})
	require.NoError(t, err)
	return m, imagesDir
}

func writeTestManifest(t *testing.T, imagesDir, name string, snapshot bool) {
	t.Helper()
	vDir := filepath.Join(imagesDir, name)
	require.NoError(t, os.MkdirAll(vDir, 0o755))

	mj := map[string]any{
		"schema_version": 1,
		"version":        name,
		"artifacts": map[string]any{
			"x86_64": map[string]any{
				"kernel": map[string]any{"file": "vmlinux-x86_64", "version": "6.1", "source": "test", "size_bytes": 100},
				"rootfs": map[string]any{"file": "rootfs-x86_64.ext4", "distro": "alpine", "distro_version": "3.23", "profile": "balanced", "size_bytes": 200},
			},
		},
		"firecracker": map[string]any{"version": "v1.14.1", "source": "test"},
		"build":       map[string]any{"date": "2026-01-01", "commit": "abc"},
	}
	if snapshot {
		mj["snapshot"] = map[string]any{
			"source_sandbox_id":   "AAAABBBBCCCCDDDDEEEEFFFF01",
			"source_sandbox_name": "test-sb",
			"source_image":        "v0.1.0",
			"parent_snapshot":     "",
			"created_at":          "2026-02-09T10:00:00Z",
		}
	}

	data, err := json.MarshalIndent(mj, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(vDir, "manifest.json"), data, 0o644))
}

func TestLocalImageManagerList(t *testing.T) {
	tests := map[string]struct {
		setup       func(t *testing.T, imagesDir string)
		expVersions map[string]model.ImageSource
	}{
		"Listing with releases and snapshots should return both.": {
			setup: func(t *testing.T, imagesDir string) {
				writeTestManifest(t, imagesDir, "v0.1.0", false)
				writeTestManifest(t, imagesDir, "my-snap", true)
			},
			expVersions: map[string]model.ImageSource{
				"v0.1.0":  model.ImageSourceRelease,
				"my-snap": model.ImageSourceSnapshot,
			},
		},
		"Listing an empty images dir should return nil.": {
			setup:       func(t *testing.T, imagesDir string) {},
			expVersions: map[string]model.ImageSource{},
		},
		"Directories without manifest should be skipped.": {
			setup: func(t *testing.T, imagesDir string) {
				writeTestManifest(t, imagesDir, "v0.1.0", false)
				// Create a dir without manifest.
				require.NoError(t, os.MkdirAll(filepath.Join(imagesDir, "orphan"), 0o755))
			},
			expVersions: map[string]model.ImageSource{
				"v0.1.0": model.ImageSourceRelease,
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			m, imagesDir := newTestLocalManager(t)
			tc.setup(t, imagesDir)

			releases, err := m.List(context.Background())
			require.NoError(t, err)

			got := make(map[string]model.ImageSource, len(releases))
			for _, r := range releases {
				assert.True(t, r.Installed)
				got[r.Version] = r.Source
			}
			assert.Equal(t, tc.expVersions, got)
		})
	}
}

func TestLocalImageManagerGetManifest(t *testing.T) {
	m, imagesDir := newTestLocalManager(t)
	writeTestManifest(t, imagesDir, "v0.1.0", false)

	got, err := m.GetManifest(context.Background(), "v0.1.0")
	require.NoError(t, err)

	assert.Equal(t, "v0.1.0", got.Version)
	assert.Equal(t, "v1.14.1", got.Firecracker.Version)
	assert.Equal(t, "abc", got.Build.Commit)

	arch, ok := got.Artifacts["x86_64"]
	require.True(t, ok)
	assert.Equal(t, "vmlinux-x86_64", arch.Kernel.File)
	assert.Equal(t, "rootfs-x86_64.ext4", arch.Rootfs.File)
}

func TestLocalImageManagerGetManifestNotFound(t *testing.T) {
	m, _ := newTestLocalManager(t)
	_, err := m.GetManifest(context.Background(), "v99.0.0")
	assert.Error(t, err)
}

func TestLocalImageManagerGetManifestUnsupportedSchema(t *testing.T) {
	m, imagesDir := newTestLocalManager(t)
	vDir := filepath.Join(imagesDir, "v0.1.0")
	require.NoError(t, os.MkdirAll(vDir, 0o755))

	mj := map[string]any{
		"schema_version": 999,
		"version":        "v0.1.0",
		"artifacts":      map[string]any{},
		"firecracker":    map[string]any{"version": "v1.14.1", "source": "test"},
		"build":          map[string]any{"date": "2026-01-01", "commit": "abc"},
	}
	data, err := json.MarshalIndent(mj, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(vDir, "manifest.json"), data, 0o644))

	_, err = m.GetManifest(context.Background(), "v0.1.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported manifest schema version 999")
}

func TestLocalImageManagerGetManifestMissingSchemaDefaultsToOne(t *testing.T) {
	m, imagesDir := newTestLocalManager(t)
	vDir := filepath.Join(imagesDir, "v0.1.0")
	require.NoError(t, os.MkdirAll(vDir, 0o755))

	mj := map[string]any{
		"version": "v0.1.0",
		"artifacts": map[string]any{
			"x86_64": map[string]any{
				"kernel": map[string]any{"file": "vmlinux-x86_64", "version": "6.1", "source": "test", "size_bytes": 100},
				"rootfs": map[string]any{"file": "rootfs.ext4", "distro": "alpine", "distro_version": "3.23", "profile": "balanced", "size_bytes": 200},
			},
		},
		"firecracker": map[string]any{"version": "v1.14.1", "source": "test"},
		"build":       map[string]any{"date": "2026-01-01", "commit": "abc"},
	}
	data, err := json.MarshalIndent(mj, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(vDir, "manifest.json"), data, 0o644))

	got, err := m.GetManifest(context.Background(), "v0.1.0")
	require.NoError(t, err)
	assert.Equal(t, 1, got.SchemaVersion)
	assert.Equal(t, "v0.1.0", got.Version)
}

func TestLocalImageManagerRemove(t *testing.T) {
	m, imagesDir := newTestLocalManager(t)

	vDir := filepath.Join(imagesDir, "v0.1.0")
	require.NoError(t, os.MkdirAll(vDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vDir, "vmlinux"), []byte("data"), 0o644))

	err := m.Remove(context.Background(), "v0.1.0")
	require.NoError(t, err)

	_, statErr := os.Stat(vDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestLocalImageManagerRemoveNotInstalled(t *testing.T) {
	m, _ := newTestLocalManager(t)
	err := m.Remove(context.Background(), "v99.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestLocalImageManagerExists(t *testing.T) {
	m, imagesDir := newTestLocalManager(t)

	exists, err := m.Exists(context.Background(), "v0.1.0")
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, os.MkdirAll(filepath.Join(imagesDir, "v0.1.0"), 0o755))

	exists, err = m.Exists(context.Background(), "v0.1.0")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestLocalImageManagerPaths(t *testing.T) {
	m, imagesDir := newTestLocalManager(t)

	assert.Equal(t, filepath.Join(imagesDir, "v0.1.0", "vmlinux-x86_64"), m.KernelPath("v0.1.0"))
	assert.Equal(t, filepath.Join(imagesDir, "v0.1.0", "rootfs-x86_64.ext4"), m.RootFSPath("v0.1.0"))
	assert.Equal(t, filepath.Join(imagesDir, "v0.1.0", "firecracker"), m.FirecrackerPath("v0.1.0"))
}
