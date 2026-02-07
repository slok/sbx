package sqlite_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/sqlite"
)

func sandboxFixture(id, name string) model.Sandbox {
	now := time.Now().UTC()
	return model.Sandbox{
		ID:        id,
		Name:      name,
		Status:    model.SandboxStatusCreated,
		CreatedAt: now,
		Config: model.SandboxConfig{
			Name: name,
			FirecrackerEngine: &model.FirecrackerEngineConfig{
				RootFS:      "/images/rootfs.ext4",
				KernelImage: "/images/vmlinux",
			},
			Resources: model.Resources{VCPUs: 2, MemoryMB: 2048, DiskGB: 10},
		},
		InternalIP: "10.0.0.2",
	}
}

func snapshotFixture(id, name string) model.Snapshot {
	now := time.Now().UTC()
	return model.Snapshot{
		ID:                 id,
		Name:               name,
		Path:               "/home/user/.sbx/snapshots/" + id + ".ext4",
		SourceSandboxID:    "sb-id-1",
		SourceSandboxName:  "sandbox-1",
		VirtualSizeBytes:   1024,
		AllocatedSizeBytes: 512,
		CreatedAt:          now,
	}
}

func newRepo(t *testing.T) *sqlite.Repository {
	t.Helper()
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: filepath.Join(t.TempDir(), "test.db"),
		Logger: log.Noop,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = repo.Close() })
	return repo
}

func TestRepositoryCRUD(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	sb := sandboxFixture("id-1", "sb-1")
	require.NoError(t, repo.CreateSandbox(ctx, sb))

	got, err := repo.GetSandbox(ctx, "id-1")
	require.NoError(t, err)
	assert.Equal(t, "sb-1", got.Name)
	assert.Equal(t, "10.0.0.2", got.InternalIP)
	assert.Equal(t, "/images/rootfs.ext4", got.Config.FirecrackerEngine.RootFS)

	gotByName, err := repo.GetSandboxByName(ctx, "sb-1")
	require.NoError(t, err)
	assert.Equal(t, "id-1", gotByName.ID)

	all, err := repo.ListSandboxes(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	now := time.Now().UTC()
	sb.Status = model.SandboxStatusRunning
	sb.StartedAt = &now
	sb.InternalIP = "10.0.0.3"
	require.NoError(t, repo.UpdateSandbox(ctx, sb))

	updated, err := repo.GetSandbox(ctx, "id-1")
	require.NoError(t, err)
	assert.Equal(t, model.SandboxStatusRunning, updated.Status)
	assert.Equal(t, "10.0.0.3", updated.InternalIP)
	assert.NotNil(t, updated.StartedAt)

	require.NoError(t, repo.DeleteSandbox(ctx, "id-1"))
	_, err = repo.GetSandbox(ctx, "id-1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrNotFound))
}

func TestRepositoryConstraints(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	sb := sandboxFixture("id-1", "sb-1")
	require.NoError(t, repo.CreateSandbox(ctx, sb))

	dupID := sandboxFixture("id-1", "sb-2")
	err := repo.CreateSandbox(ctx, dupID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrAlreadyExists))

	dupName := sandboxFixture("id-2", "sb-1")
	err = repo.CreateSandbox(ctx, dupName)
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrAlreadyExists))

	nonExistent := sandboxFixture("id-x", "sb-x")
	err = repo.UpdateSandbox(ctx, nonExistent)
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrNotFound))

	err = repo.DeleteSandbox(ctx, "id-x")
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrNotFound))
}

func TestRepositorySnapshotCRUD(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	snap := snapshotFixture("snap-1", "snapshot-1")
	require.NoError(t, repo.CreateSnapshot(ctx, snap))

	got, err := repo.GetSnapshot(ctx, "snap-1")
	require.NoError(t, err)
	assert.Equal(t, "snapshot-1", got.Name)
	assert.Equal(t, "/home/user/.sbx/snapshots/snap-1.ext4", got.Path)
	assert.Equal(t, int64(1024), got.VirtualSizeBytes)
	assert.Equal(t, int64(512), got.AllocatedSizeBytes)

	gotByName, err := repo.GetSnapshotByName(ctx, "snapshot-1")
	require.NoError(t, err)
	assert.Equal(t, "snap-1", gotByName.ID)

	all, err := repo.ListSnapshots(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1)

	_, err = repo.GetSnapshot(ctx, "snap-x")
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrNotFound))
}

func TestRepositorySnapshotConstraints(t *testing.T) {
	ctx := context.Background()
	repo := newRepo(t)

	snap := snapshotFixture("snap-1", "snapshot-1")
	require.NoError(t, repo.CreateSnapshot(ctx, snap))

	dupID := snapshotFixture("snap-1", "snapshot-2")
	err := repo.CreateSnapshot(ctx, dupID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrAlreadyExists))

	dupName := snapshotFixture("snap-2", "snapshot-1")
	err = repo.CreateSnapshot(ctx, dupName)
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrAlreadyExists))

	invalid := snapshotFixture("", "bad")
	err = repo.CreateSnapshot(ctx, invalid)
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrNotValid))
}
