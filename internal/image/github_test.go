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

// newTestPuller creates a GitHubImagePuller backed by httptest servers.
func newTestPuller(t *testing.T, apiHandler, downloadHandler http.Handler) (*image.GitHubImagePuller, string) {
	t.Helper()

	apiServer := httptest.NewServer(apiHandler)
	t.Cleanup(apiServer.Close)

	downloadServer := httptest.NewServer(downloadHandler)
	t.Cleanup(downloadServer.Close)

	imagesDir := t.TempDir()
	p, err := image.NewGitHubImagePullerWithBaseURL(image.GitHubImagePullerConfig{
		Repo:      "test/images",
		ImagesDir: imagesDir,
	}, apiServer.URL, downloadServer.URL)
	require.NoError(t, err)

	return p, imagesDir
}

func TestGitHubImagePullerListRemote(t *testing.T) {
	tests := map[string]struct {
		releases    []map[string]string
		expVersions []string
	}{
		"Multiple remote releases should be listed.": {
			releases:    []map[string]string{{"tag_name": "v0.2.0"}, {"tag_name": "v0.1.0"}},
			expVersions: []string{"v0.2.0", "v0.1.0"},
		},
		"No remote releases should return empty.": {
			releases:    []map[string]string{},
			expVersions: nil,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("page") == "2" {
					_ = json.NewEncoder(w).Encode([]map[string]string{})
					return
				}
				_ = json.NewEncoder(w).Encode(tc.releases)
			})

			p, _ := newTestPuller(t, apiHandler, http.NotFoundHandler())

			releases, err := p.ListRemote(context.Background())
			require.NoError(t, err)

			var gotVersions []string
			for _, r := range releases {
				gotVersions = append(gotVersions, r.Version)
			}
			assert.Equal(t, tc.expVersions, gotVersions)
		})
	}
}

func TestGitHubImagePullerPull(t *testing.T) {
	kernelData := []byte("fake-kernel-binary-data")
	rootfsData := []byte("fake-rootfs-binary-data")
	fcBinaryData := []byte("fake-firecracker-binary")

	manifest := map[string]any{
		"schema_version": 1,
		"version":        "v0.1.0",
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

	fcTgz := buildFakeFCTgz(t, "v1.14.1", "x86_64", fcBinaryData)

	downloadHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/test/images/releases/download/v0.1.0/manifest.json":
			_ = json.NewEncoder(w).Encode(manifest)
		case "/test/images/releases/download/v0.1.0/vmlinux-x86_64":
			_, _ = w.Write(kernelData)
		case "/test/images/releases/download/v0.1.0/rootfs-x86_64.ext4":
			_, _ = w.Write(rootfsData)
		case "/firecracker-microvm/firecracker/releases/download/v1.14.1/firecracker-v1.14.1-x86_64.tgz":
			_, _ = w.Write(fcTgz)
		default:
			http.NotFound(w, r)
		}
	})

	p, imagesDir := newTestPuller(t, http.NotFoundHandler(), downloadHandler)

	result, err := p.Pull(context.Background(), "v0.1.0", image.PullOptions{})
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

func TestGitHubImagePullerPullSkipsIfInstalled(t *testing.T) {
	p, imagesDir := newTestPuller(t, http.NotFoundHandler(), http.NotFoundHandler())

	// Pre-create the version directory with a valid manifest.
	writeTestManifest(t, imagesDir, "v0.1.0", false)

	result, err := p.Pull(context.Background(), "v0.1.0", image.PullOptions{})
	require.NoError(t, err)
	assert.True(t, result.Skipped)
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
