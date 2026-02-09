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

func newTestSnapshotCreator(t *testing.T) (*image.LocalSnapshotCreator, string) {
	t.Helper()
	imagesDir := t.TempDir()
	sc, err := image.NewLocalSnapshotCreator(image.LocalSnapshotCreatorConfig{
		ImagesDir: imagesDir,
	})
	require.NoError(t, err)
	return sc, imagesDir
}

// writeTestSourceFiles creates fake kernel and rootfs source files in a temp dir.
func writeTestSourceFiles(t *testing.T) (kernelPath, rootfsPath, fcPath string) {
	t.Helper()
	srcDir := t.TempDir()

	kernelPath = filepath.Join(srcDir, "vmlinux")
	require.NoError(t, os.WriteFile(kernelPath, []byte("fake-kernel-data"), 0o644))

	rootfsPath = filepath.Join(srcDir, "rootfs.ext4")
	require.NoError(t, os.WriteFile(rootfsPath, []byte("fake-rootfs-data"), 0o644))

	fcPath = filepath.Join(srcDir, "firecracker")
	require.NoError(t, os.WriteFile(fcPath, []byte("fake-fc-binary"), 0o755))

	return kernelPath, rootfsPath, fcPath
}

func TestLocalSnapshotCreatorCreate(t *testing.T) {
	kernelSrc, rootfsSrc, fcSrc := writeTestSourceFiles(t)

	tests := map[string]struct {
		setup      func(t *testing.T, imagesDir string)
		opts       func(imagesDir string) image.CreateSnapshotOptions
		expErr     bool
		expErrMsg  string
		assertions func(t *testing.T, imagesDir string)
	}{
		"Successful snapshot with source manifest should copy files and write manifest.": {
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:              "my-snap",
					KernelSrc:         kernelSrc,
					RootFSSrc:         rootfsSrc,
					FirecrackerSrc:    fcSrc,
					SourceSandboxID:   "01JKQWERTYASDFGZXCVBNMLKJH",
					SourceSandboxName: "test-sb",
					SourceImage:       "v0.1.0",
					SourceManifest: &model.ImageManifest{
						SchemaVersion: 1,
						Version:       "v0.1.0",
						Artifacts: map[string]model.ArchArtifacts{
							image.HostArch(): {
								Kernel: model.KernelInfo{Version: "5.10.217", Source: "kernel.org"},
								Rootfs: model.RootfsInfo{Distro: "alpine", DistroVersion: "3.20", Profile: "balanced"},
							},
						},
						Firecracker: model.FirecrackerInfo{Version: "1.10.1", Source: "github"},
						Build:       model.BuildInfo{Date: "2026-01-01", Commit: "abc123"},
					},
				}
			},
			assertions: func(t *testing.T, imagesDir string) {
				vDir := filepath.Join(imagesDir, "my-snap")

				// Verify files exist.
				arch := image.HostArch()
				assertFileExists(t, filepath.Join(vDir, "vmlinux-"+arch))
				assertFileExists(t, filepath.Join(vDir, "rootfs-"+arch+".ext4"))
				assertFileExists(t, filepath.Join(vDir, "firecracker"))
				assertFileExists(t, filepath.Join(vDir, "manifest.json"))

				// Verify file content was copied.
				data, err := os.ReadFile(filepath.Join(vDir, "vmlinux-"+arch))
				require.NoError(t, err)
				assert.Equal(t, "fake-kernel-data", string(data))

				data, err = os.ReadFile(filepath.Join(vDir, "rootfs-"+arch+".ext4"))
				require.NoError(t, err)
				assert.Equal(t, "fake-rootfs-data", string(data))

				// Verify firecracker is executable.
				info, err := os.Stat(filepath.Join(vDir, "firecracker"))
				require.NoError(t, err)
				assert.True(t, info.Mode().Perm()&0o100 != 0, "firecracker should be executable")

				// Verify manifest content.
				mData, err := os.ReadFile(filepath.Join(vDir, "manifest.json"))
				require.NoError(t, err)

				var mj map[string]any
				require.NoError(t, json.Unmarshal(mData, &mj))
				assert.Equal(t, float64(1), mj["schema_version"])
				assert.Equal(t, "my-snap", mj["version"])

				// Verify snapshot metadata.
				snap, ok := mj["snapshot"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "01JKQWERTYASDFGZXCVBNMLKJH", snap["source_sandbox_id"])
				assert.Equal(t, "test-sb", snap["source_sandbox_name"])
				assert.Equal(t, "v0.1.0", snap["source_image"])

				// Verify inherited metadata.
				arts, ok := mj["artifacts"].(map[string]any)
				require.True(t, ok)
				archArts, ok := arts[arch].(map[string]any)
				require.True(t, ok)
				kernel, ok := archArts["kernel"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "5.10.217", kernel["version"])

				fc, ok := mj["firecracker"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "1.10.1", fc["version"])
			},
		},

		"Successful snapshot without source manifest should write manifest without inherited metadata.": {
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:              "no-manifest-snap",
					KernelSrc:         kernelSrc,
					RootFSSrc:         rootfsSrc,
					SourceSandboxID:   "01JKQWERTYASDFGZXCVBNMLKJH",
					SourceSandboxName: "test-sb",
				}
			},
			assertions: func(t *testing.T, imagesDir string) {
				vDir := filepath.Join(imagesDir, "no-manifest-snap")
				assertFileExists(t, filepath.Join(vDir, "manifest.json"))

				// No firecracker binary should exist.
				_, err := os.Stat(filepath.Join(vDir, "firecracker"))
				assert.True(t, os.IsNotExist(err))

				// Manifest should still have snapshot info.
				mData, err := os.ReadFile(filepath.Join(vDir, "manifest.json"))
				require.NoError(t, err)

				var mj map[string]any
				require.NoError(t, json.Unmarshal(mData, &mj))

				snap, ok := mj["snapshot"].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "01JKQWERTYASDFGZXCVBNMLKJH", snap["source_sandbox_id"])

				// Inherited fields should be empty/zero.
				fc, ok := mj["firecracker"].(map[string]any)
				require.True(t, ok)
				assert.Empty(t, fc["version"])
			},
		},

		"Invalid image name should fail.": {
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:      "invalid name!",
					KernelSrc: kernelSrc,
					RootFSSrc: rootfsSrc,
				}
			},
			expErr:    true,
			expErrMsg: "invalid snapshot image name",
		},

		"Empty image name should fail.": {
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:      "",
					KernelSrc: kernelSrc,
					RootFSSrc: rootfsSrc,
				}
			},
			expErr:    true,
			expErrMsg: "invalid snapshot image name",
		},

		"Name collision should fail with already exists error.": {
			setup: func(t *testing.T, imagesDir string) {
				require.NoError(t, os.MkdirAll(filepath.Join(imagesDir, "existing"), 0o755))
			},
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:      "existing",
					KernelSrc: kernelSrc,
					RootFSSrc: rootfsSrc,
				}
			},
			expErr:    true,
			expErrMsg: "already exists",
		},

		"Missing kernel source should fail and clean up.": {
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:      "fail-kernel",
					KernelSrc: "/nonexistent/vmlinux",
					RootFSSrc: rootfsSrc,
				}
			},
			expErr:    true,
			expErrMsg: "copying kernel",
			assertions: func(t *testing.T, imagesDir string) {
				// Directory should be cleaned up on error.
				_, err := os.Stat(filepath.Join(imagesDir, "fail-kernel"))
				assert.True(t, os.IsNotExist(err), "snapshot dir should be cleaned up on failure")
			},
		},

		"Missing rootfs source should fail and clean up.": {
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:      "fail-rootfs",
					KernelSrc: kernelSrc,
					RootFSSrc: "/nonexistent/rootfs.ext4",
				}
			},
			expErr:    true,
			expErrMsg: "copying rootfs",
			assertions: func(t *testing.T, imagesDir string) {
				_, err := os.Stat(filepath.Join(imagesDir, "fail-rootfs"))
				assert.True(t, os.IsNotExist(err), "snapshot dir should be cleaned up on failure")
			},
		},

		"Missing firecracker source should not fail, just skip the binary.": {
			opts: func(imagesDir string) image.CreateSnapshotOptions {
				return image.CreateSnapshotOptions{
					Name:              "no-fc",
					KernelSrc:         kernelSrc,
					RootFSSrc:         rootfsSrc,
					FirecrackerSrc:    "/nonexistent/firecracker",
					SourceSandboxID:   "01JKQWERTYASDFGZXCVBNMLKJH",
					SourceSandboxName: "test-sb",
				}
			},
			assertions: func(t *testing.T, imagesDir string) {
				vDir := filepath.Join(imagesDir, "no-fc")
				assertFileExists(t, filepath.Join(vDir, "manifest.json"))

				// Firecracker binary should not exist (copy failed silently).
				_, err := os.Stat(filepath.Join(vDir, "firecracker"))
				assert.True(t, os.IsNotExist(err))
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			sc, imagesDir := newTestSnapshotCreator(t)
			if tc.setup != nil {
				tc.setup(t, imagesDir)
			}

			opts := tc.opts(imagesDir)
			err := sc.Create(context.Background(), opts)

			if tc.expErr {
				require.Error(t, err)
				if tc.expErrMsg != "" {
					assert.Contains(t, err.Error(), tc.expErrMsg)
				}
			} else {
				require.NoError(t, err)
			}

			if tc.assertions != nil {
				tc.assertions(t, imagesDir)
			}
		})
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.NoError(t, err, "expected file to exist: %s", path)
}
