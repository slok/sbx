package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage/memory"
)

func TestRepositorySnapshots(t *testing.T) {
	repo, err := memory.NewRepository(memory.RepositoryConfig{Logger: log.Noop})
	require.NoError(t, err)

	snapshot := model.Snapshot{
		ID:                 "01ARZ3NDEKTSV4RRFFQ69G5FAB",
		Name:               "snapshot-1",
		Path:               "/tmp/snapshot-1.ext4",
		SourceSandboxID:    "sb-1",
		SourceSandboxName:  "sandbox-1",
		VirtualSizeBytes:   1024,
		AllocatedSizeBytes: 512,
		CreatedAt:          time.Now().UTC(),
	}

	err = repo.CreateSnapshot(context.Background(), snapshot)
	require.NoError(t, err)

	gotByID, err := repo.GetSnapshot(context.Background(), snapshot.ID)
	require.NoError(t, err)
	assert.Equal(t, snapshot.Name, gotByID.Name)

	gotByName, err := repo.GetSnapshotByName(context.Background(), snapshot.Name)
	require.NoError(t, err)
	assert.Equal(t, snapshot.ID, gotByName.ID)

	list, err := repo.ListSnapshots(context.Background())
	require.NoError(t, err)
	assert.Len(t, list, 1)

	err = repo.CreateSnapshot(context.Background(), model.Snapshot{
		ID:                 "01ARZ3NDEKTSV4RRFFQ69G5FAC",
		Name:               "snapshot-1",
		Path:               "/tmp/snapshot-2.ext4",
		VirtualSizeBytes:   1,
		AllocatedSizeBytes: 1,
		CreatedAt:          time.Now().UTC(),
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrAlreadyExists))

	_, err = repo.GetSnapshot(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrNotFound))

	_, err = repo.GetSnapshotByName(context.Background(), "missing")
	require.Error(t, err)
	assert.True(t, errors.Is(err, model.ErrNotFound))
}
