package image_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/image"
)

// newTestManager creates a GitHubImageManager backed by httptest servers.
func newTestManager(t *testing.T, apiHandler, downloadHandler http.Handler) (*image.GitHubImageManager, string) {
	t.Helper()

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	downloadServer := httptest.NewServer(downloadHandler)
	t.Cleanup(downloadServer.Close)

	imagesDir := t.TempDir()
	m, err := image.NewGitHubImageManagerWithBaseURL(image.GitHubImageManagerConfig{
		Repo:      "test/images",
		ImagesDir: imagesDir,
	}, apiServer.URL, downloadServer.URL)
	require.NoError(t, err)

	return m, imagesDir
}

func TestGitHubImageManagerListReleases(t *testing.T) {
	tests := map[string]struct {
		releases     []map[string]string
		installed    []string
		expVersions  []string
		expInstalled map[string]bool
	}{
		"multiple releases with one installed": {
			releases:     []map[string]string{{"tag_name": "v0.2.0"}, {"tag_name": "v0.1.0"}},
			installed:    []string{"v0.1.0"},
			expVersions:  []string{"v0.2.0", "v0.1.0"},
			expInstalled: map[string]bool{"v0.2.0": false, "v0.1.0": true},
		},
		"no releases": {
			releases:     []map[string]string{},
			expVersions:  nil,
			expInstalled: map[string]bool{},
		},
		"all installed": {
			releases:     []map[string]string{{"tag_name": "v0.1.0"}},
			installed:    []string{"v0.1.0"},
			expVersions:  []string{"v0.1.0"},
			expInstalled: map[string]bool{"v0.1.0": true},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Return releases on page 1, empty on page 2+ (stops pagination).
				if r.URL.Query().Get("page") == "2" {
					json.NewEncoder(w).Encode([]map[string]string{})
					return
				}
				json.NewEncoder(w).Encode(tc.releases)
			})

			m, imagesDir := newTestManager(t, apiHandler, http.NotFoundHandler())

			// Create installed version directories.
			for _, v := range tc.installed {
				require.NoError(t, os.MkdirAll(filepath.Join(imagesDir, v), 0o755))
			}

			releases, err := m.ListReleases(context.Background())
			require.NoError(t, err)

			var gotVersions []string
			for _, r := range releases {
				gotVersions = append(gotVersions, r.Version)
				assert.Equal(t, tc.expInstalled[r.Version], r.Installed, "installed status for %s", r.Version)
			}
			assert.Equal(t, tc.expVersions, gotVersions)
		})
	}
}

func TestGitHubImageManagerGetManifest(t *testing.T) {
	manifest := map[string]any{
		"version": "v0.1.0",
		"artifacts": map[string]any{
			"x86_64": map[string]any{
				"kernel": map[string]any{
					"file": "vmlinux-x86_64", "version": "6.1.155",
					"source": "firecracker-ci/v1.15", "size_bytes": 44279576,
				},
				"rootfs": map[string]any{
					"file": "rootfs-x86_64.ext4", "distro": "alpine",
					"distro_version": "3.23", "profile": "balanced", "size_bytes": 679034880,
				},
			},
		},
		"firecracker": map[string]any{"version": "v1.14.1", "source": "github.com/firecracker-microvm/firecracker"},
		"build":       map[string]any{"date": "2026-02-08T09:54:17Z", "commit": "adc9bc1"},
	}

	downloadHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/test/images/releases/download/v0.1.0/manifest.json" {
			json.NewEncoder(w).Encode(manifest)
			return
		}
		http.NotFound(w, r)
	})

	m, _ := newTestManager(t, http.NotFoundHandler(), downloadHandler)

	got, err := m.GetManifest(context.Background(), "v0.1.0")
	require.NoError(t, err)

	assert.Equal(t, "v0.1.0", got.Version)
	assert.Equal(t, "v1.14.1", got.Firecracker.Version)
	assert.Equal(t, "adc9bc1", got.Build.Commit)

	arch, ok := got.Artifacts["x86_64"]
	require.True(t, ok)
	assert.Equal(t, "vmlinux-x86_64", arch.Kernel.File)
	assert.Equal(t, "rootfs-x86_64.ext4", arch.Rootfs.File)
	assert.Equal(t, int64(44279576), arch.Kernel.SizeBytes)
}

func TestGitHubImageManagerGetManifestNotFound(t *testing.T) {
	downloadHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	m, _ := newTestManager(t, http.NotFoundHandler(), downloadHandler)
	_, err := m.GetManifest(context.Background(), "v99.0.0")
	assert.Error(t, err)
}

func TestGitHubImageManagerPull(t *testing.T) {
	kernelData := []byte("fake-kernel-binary-data")
	rootfsData := []byte("fake-rootfs-binary-data")
	fcBinaryData := []byte("fake-firecracker-binary")

	manifest := map[string]any{
		"version": "v0.1.0",
		"artifacts": map[string]any{
			"x86_64": map[string]any{
				"kernel": map[string]any{
					"file": "vmlinux-x86_64", "version": "6.1.155",
					"source": "firecracker-ci/v1.15", "size_bytes": len(kernelData),
				},
				"rootfs": map[string]any{
					"file": "rootfs-x86_64.ext4", "distro": "alpine",
					"distro_version": "3.23", "profile": "balanced", "size_bytes": len(rootfsData),
				},
			},
		},
		"firecracker": map[string]any{"version": "v1.14.1", "source": "github.com/firecracker-microvm/firecracker"},
		"build":       map[string]any{"date": "2026-02-08T09:54:17Z", "commit": "abc"},
	}

	// Build a fake firecracker tgz.
	fcTgz := buildFakeFCTgz(t, "v1.14.1", "x86_64", fcBinaryData)

	downloadHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/images/releases/download/v0.1.0/manifest.json":
			json.NewEncoder(w).Encode(manifest)
		case "/test/images/releases/download/v0.1.0/vmlinux-x86_64":
			w.Write(kernelData)
		case "/test/images/releases/download/v0.1.0/rootfs-x86_64.ext4":
			w.Write(rootfsData)
		case "/firecracker-microvm/firecracker/releases/download/v1.14.1/firecracker-v1.14.1-x86_64.tgz":
			w.Write(fcTgz)
		default:
			http.NotFound(w, r)
		}
	})

	m, imagesDir := newTestManager(t, http.NotFoundHandler(), downloadHandler)

	result, err := m.Pull(context.Background(), "v0.1.0", image.PullOptions{})
	require.NoError(t, err)

	assert.False(t, result.Skipped)
	assert.Equal(t, "v0.1.0", result.Version)

	// Verify files written.
	gotKernel, err := os.ReadFile(filepath.Join(imagesDir, "v0.1.0", "vmlinux-x86_64"))
	require.NoError(t, err)
	assert.Equal(t, kernelData, gotKernel)

	gotRootfs, err := os.ReadFile(filepath.Join(imagesDir, "v0.1.0", "rootfs-x86_64.ext4"))
	require.NoError(t, err)
	assert.Equal(t, rootfsData, gotRootfs)

	gotFC, err := os.ReadFile(filepath.Join(imagesDir, "v0.1.0", "firecracker"))
	require.NoError(t, err)
	assert.Equal(t, fcBinaryData, gotFC)
}

func TestGitHubImageManagerPullSkipsIfInstalled(t *testing.T) {
	m, imagesDir := newTestManager(t, http.NotFoundHandler(), http.NotFoundHandler())

	// Pre-create the version directory.
	require.NoError(t, os.MkdirAll(filepath.Join(imagesDir, "v0.1.0"), 0o755))

	result, err := m.Pull(context.Background(), "v0.1.0", image.PullOptions{})
	require.NoError(t, err)
	assert.True(t, result.Skipped)
}

func TestGitHubImageManagerRemove(t *testing.T) {
	m, imagesDir := newTestManager(t, http.NotFoundHandler(), http.NotFoundHandler())

	// Create a version dir with a file.
	vDir := filepath.Join(imagesDir, "v0.1.0")
	require.NoError(t, os.MkdirAll(vDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(vDir, "vmlinux"), []byte("data"), 0o644))

	err := m.Remove(context.Background(), "v0.1.0")
	require.NoError(t, err)

	_, statErr := os.Stat(vDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestGitHubImageManagerRemoveNotInstalled(t *testing.T) {
	m, _ := newTestManager(t, http.NotFoundHandler(), http.NotFoundHandler())

	err := m.Remove(context.Background(), "v99.0.0")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestGitHubImageManagerExists(t *testing.T) {
	m, imagesDir := newTestManager(t, http.NotFoundHandler(), http.NotFoundHandler())

	exists, err := m.Exists(context.Background(), "v0.1.0")
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, os.MkdirAll(filepath.Join(imagesDir, "v0.1.0"), 0o755))

	exists, err = m.Exists(context.Background(), "v0.1.0")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestGitHubImageManagerPaths(t *testing.T) {
	m, imagesDir := newTestManager(t, http.NotFoundHandler(), http.NotFoundHandler())

	assert.Equal(t, filepath.Join(imagesDir, "v0.1.0", "vmlinux-x86_64"), m.KernelPath("v0.1.0"))
	assert.Equal(t, filepath.Join(imagesDir, "v0.1.0", "rootfs-x86_64.ext4"), m.RootFSPath("v0.1.0"))
	assert.Equal(t, filepath.Join(imagesDir, "v0.1.0", "firecracker"), m.FirecrackerPath("v0.1.0"))
}

// buildFakeFCTgz creates a gzipped tar archive with a fake firecracker binary.
func buildFakeFCTgz(t *testing.T, version, arch string, binaryData []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	name := "release-" + version + "-" + arch + "/firecracker-" + version + "-" + arch
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: name,
		Mode: 0o755,
		Size: int64(len(binaryData)),
	}))
	_, err := tw.Write(binaryData)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}
