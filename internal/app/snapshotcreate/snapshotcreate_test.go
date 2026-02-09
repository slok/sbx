package snapshotcreate_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/snapshotcreate"
	"github.com/slok/sbx/internal/image"
	"github.com/slok/sbx/internal/image/imagemock"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/storagemock"
)

func TestNewService(t *testing.T) {
	tests := map[string]struct {
		config snapshotcreate.ServiceConfig
		expErr bool
	}{
		"Valid config should create service.": {
			config: snapshotcreate.ServiceConfig{
				ImageManager:    &imagemock.MockImageManager{},
				SnapshotCreator: &imagemock.MockSnapshotCreator{},
				Repository:      &storagemock.MockRepository{},
				DataDir:         "/tmp/sbx",
			},
		},

		"Missing image manager should fail.": {
			config: snapshotcreate.ServiceConfig{
				SnapshotCreator: &imagemock.MockSnapshotCreator{},
				Repository:      &storagemock.MockRepository{},
				DataDir:         "/tmp/sbx",
			},
			expErr: true,
		},

		"Missing snapshot creator should fail.": {
			config: snapshotcreate.ServiceConfig{
				ImageManager: &imagemock.MockImageManager{},
				Repository:   &storagemock.MockRepository{},
				DataDir:      "/tmp/sbx",
			},
			expErr: true,
		},

		"Missing repository should fail.": {
			config: snapshotcreate.ServiceConfig{
				ImageManager:    &imagemock.MockImageManager{},
				SnapshotCreator: &imagemock.MockSnapshotCreator{},
				DataDir:         "/tmp/sbx",
			},
			expErr: true,
		},

		"Missing data dir should fail.": {
			config: snapshotcreate.ServiceConfig{
				ImageManager:    &imagemock.MockImageManager{},
				SnapshotCreator: &imagemock.MockSnapshotCreator{},
				Repository:      &storagemock.MockRepository{},
			},
			expErr: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)
			svc, err := snapshotcreate.NewService(tc.config)
			if tc.expErr {
				require.Error(err)
			} else {
				require.NoError(err)
				require.NotNil(svc)
			}
		})
	}
}

func TestServiceRun(t *testing.T) {
	const (
		dataDir    = "/home/user/.sbx"
		sandboxID  = "01JKQWERTYASDFGZXCVBNMLKJH"
		sbxName    = "my-sandbox"
		kernelPath = "/home/user/.sbx/images/v0.1.0/vmlinux-x86_64"
	)

	stoppedSandbox := &model.Sandbox{
		ID:     sandboxID,
		Name:   sbxName,
		Status: model.SandboxStatusStopped,
		Config: model.SandboxConfig{
			FirecrackerEngine: &model.FirecrackerEngineConfig{
				KernelImage: kernelPath,
				RootFS:      "/home/user/.sbx/vms/" + sandboxID + "/rootfs.ext4",
			},
		},
	}

	sourceManifest := &model.ImageManifest{
		SchemaVersion: 1,
		Version:       "v0.1.0",
		Artifacts: map[string]model.ArchArtifacts{
			"x86_64": {
				Kernel: model.KernelInfo{Version: "5.10.217"},
				Rootfs: model.RootfsInfo{Distro: "alpine", DistroVersion: "3.20"},
			},
		},
		Firecracker: model.FirecrackerInfo{Version: "1.10.1"},
	}

	tests := map[string]struct {
		mockRepo   func(m *storagemock.MockRepository)
		mockImgMgr func(m *imagemock.MockImageManager)
		mockSnapC  func(m *imagemock.MockSnapshotCreator)
		req        snapshotcreate.Request
		expName    string
		expErr     bool
	}{
		"Successful snapshot with explicit name should return the name.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(stoppedSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "my-snap").Once().Return(false, nil)
				m.On("GetManifest", mock.Anything, "v0.1.0").Once().Return(sourceManifest, nil)
				m.On("FirecrackerPath", "v0.1.0").Once().Return("/home/user/.sbx/images/v0.1.0/firecracker")
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {
				m.On("Create", mock.Anything, mock.MatchedBy(func(opts image.CreateSnapshotOptions) bool {
					return opts.Name == "my-snap" &&
						opts.KernelSrc == kernelPath &&
						opts.RootFSSrc == dataDir+"/vms/"+sandboxID+"/rootfs.ext4" &&
						opts.FirecrackerSrc == "/home/user/.sbx/images/v0.1.0/firecracker" &&
						opts.SourceSandboxID == sandboxID &&
						opts.SourceSandboxName == sbxName &&
						opts.SourceImage == "v0.1.0" &&
						opts.SourceManifest == sourceManifest
				})).Once().Return(nil)
			},
			req:     snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expName: "my-snap",
		},

		"Successful snapshot with auto-generated name should return a generated name.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(stoppedSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				// Auto-generated name doesn't exist.
				m.On("Exists", mock.Anything, mock.AnythingOfType("string")).Once().Return(false, nil)
				m.On("GetManifest", mock.Anything, "v0.1.0").Once().Return(sourceManifest, nil)
				m.On("FirecrackerPath", "v0.1.0").Once().Return("/home/user/.sbx/images/v0.1.0/firecracker")
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {
				m.On("Create", mock.Anything, mock.MatchedBy(func(opts image.CreateSnapshotOptions) bool {
					return opts.SourceSandboxID == sandboxID && opts.SourceSandboxName == sbxName
				})).Once().Return(nil)
			},
			req: snapshotcreate.Request{NameOrID: sbxName},
		},

		"Invalid image name should fail early.": {
			mockRepo:   func(m *storagemock.MockRepository) {},
			mockImgMgr: func(m *imagemock.MockImageManager) {},
			mockSnapC:  func(m *imagemock.MockSnapshotCreator) {},
			req:        snapshotcreate.Request{NameOrID: sbxName, ImageName: "invalid name!"},
			expErr:     true,
		},

		"Sandbox not found by name should fail.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, "nonexistent").Once().Return(nil, model.ErrNotFound)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {},
			mockSnapC:  func(m *imagemock.MockSnapshotCreator) {},
			req:        snapshotcreate.Request{NameOrID: "nonexistent", ImageName: "my-snap"},
			expErr:     true,
		},

		"Sandbox found by ULID when name lookup fails.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sandboxID).Once().Return(nil, model.ErrNotFound)
				m.On("GetSandbox", mock.Anything, sandboxID).Once().Return(stoppedSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "my-snap").Once().Return(false, nil)
				m.On("GetManifest", mock.Anything, "v0.1.0").Once().Return(sourceManifest, nil)
				m.On("FirecrackerPath", "v0.1.0").Once().Return("/home/user/.sbx/images/v0.1.0/firecracker")
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {
				m.On("Create", mock.Anything, mock.Anything).Once().Return(nil)
			},
			req:     snapshotcreate.Request{NameOrID: sandboxID, ImageName: "my-snap"},
			expName: "my-snap",
		},

		"Sandbox not found by ULID should fail.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sandboxID).Once().Return(nil, model.ErrNotFound)
				m.On("GetSandbox", mock.Anything, sandboxID).Once().Return(nil, model.ErrNotFound)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {},
			mockSnapC:  func(m *imagemock.MockSnapshotCreator) {},
			req:        snapshotcreate.Request{NameOrID: sandboxID, ImageName: "my-snap"},
			expErr:     true,
		},

		"Running sandbox should fail with not valid error.": {
			mockRepo: func(m *storagemock.MockRepository) {
				runningSandbox := *stoppedSandbox
				runningSandbox.Status = model.SandboxStatusRunning
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(&runningSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {},
			mockSnapC:  func(m *imagemock.MockSnapshotCreator) {},
			req:        snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expErr:     true,
		},

		"Stopped sandbox (freshly created) should succeed.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(stoppedSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "my-snap").Once().Return(false, nil)
				m.On("GetManifest", mock.Anything, "v0.1.0").Once().Return(sourceManifest, nil)
				m.On("FirecrackerPath", "v0.1.0").Once().Return("/home/user/.sbx/images/v0.1.0/firecracker")
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {
				m.On("Create", mock.Anything, mock.Anything).Once().Return(nil)
			},
			req:     snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expName: "my-snap",
		},

		"Image name already exists with explicit name should fail.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(stoppedSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "existing-img").Once().Return(true, nil)
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {},
			req:       snapshotcreate.Request{NameOrID: sbxName, ImageName: "existing-img"},
			expErr:    true,
		},

		"Snapshot creator error should propagate.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(stoppedSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "my-snap").Once().Return(false, nil)
				m.On("GetManifest", mock.Anything, "v0.1.0").Once().Return(sourceManifest, nil)
				m.On("FirecrackerPath", "v0.1.0").Once().Return("/home/user/.sbx/images/v0.1.0/firecracker")
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {
				m.On("Create", mock.Anything, mock.Anything).Once().Return(fmt.Errorf("disk full"))
			},
			req:    snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expErr: true,
		},

		"Sandbox without kernel image should fail.": {
			mockRepo: func(m *storagemock.MockRepository) {
				noKernelSandbox := &model.Sandbox{
					ID:     sandboxID,
					Name:   sbxName,
					Status: model.SandboxStatusStopped,
					Config: model.SandboxConfig{
						FirecrackerEngine: &model.FirecrackerEngineConfig{
							RootFS: "/some/rootfs.ext4",
						},
					},
				}
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(noKernelSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "my-snap").Once().Return(false, nil)
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {},
			req:       snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expErr:    true,
		},

		"Snapshot without source image manifest should still succeed.": {
			mockRepo: func(m *storagemock.MockRepository) {
				customKernelSandbox := &model.Sandbox{
					ID:     sandboxID,
					Name:   sbxName,
					Status: model.SandboxStatusStopped,
					Config: model.SandboxConfig{
						FirecrackerEngine: &model.FirecrackerEngineConfig{
							KernelImage: "/custom/vmlinux",
							RootFS:      "/custom/rootfs.ext4",
						},
					},
				}
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(customKernelSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "my-snap").Once().Return(false, nil)
				// No source image detected from custom kernel path, so GetManifest is not called.
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {
				m.On("Create", mock.Anything, mock.MatchedBy(func(opts image.CreateSnapshotOptions) bool {
					return opts.Name == "my-snap" &&
						opts.KernelSrc == "/custom/vmlinux" &&
						opts.FirecrackerSrc == "" &&
						opts.SourceImage == "" &&
						opts.SourceManifest == nil
				})).Once().Return(nil)
			},
			req:     snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expName: "my-snap",
		},

		"Exists check error should propagate.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(stoppedSandbox, nil)
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {
				m.On("Exists", mock.Anything, "my-snap").Once().Return(false, fmt.Errorf("io error"))
			},
			mockSnapC: func(m *imagemock.MockSnapshotCreator) {},
			req:       snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expErr:    true,
		},

		"Repository error should propagate.": {
			mockRepo: func(m *storagemock.MockRepository) {
				m.On("GetSandboxByName", mock.Anything, sbxName).Once().Return(nil, fmt.Errorf("db error"))
			},
			mockImgMgr: func(m *imagemock.MockImageManager) {},
			mockSnapC:  func(m *imagemock.MockSnapshotCreator) {},
			req:        snapshotcreate.Request{NameOrID: sbxName, ImageName: "my-snap"},
			expErr:     true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)

			mRepo := &storagemock.MockRepository{}
			mImgMgr := &imagemock.MockImageManager{}
			mSnapC := &imagemock.MockSnapshotCreator{}
			tc.mockRepo(mRepo)
			tc.mockImgMgr(mImgMgr)
			tc.mockSnapC(mSnapC)

			svc, err := snapshotcreate.NewService(snapshotcreate.ServiceConfig{
				ImageManager:    mImgMgr,
				SnapshotCreator: mSnapC,
				Repository:      mRepo,
				DataDir:         dataDir,
			})
			require.NoError(t, err)

			result, err := svc.Run(context.Background(), tc.req)

			if tc.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				if tc.expName != "" {
					assert.Equal(tc.expName, result)
				} else {
					assert.NotEmpty(result)
				}
			}

			mRepo.AssertExpectations(t)
			mImgMgr.AssertExpectations(t)
			mSnapC.AssertExpectations(t)
		})
	}
}
