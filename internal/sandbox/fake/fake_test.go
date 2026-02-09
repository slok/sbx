package fake_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/fake"
)

func testConfig(name string) model.SandboxConfig {
	return model.SandboxConfig{
		Name: name,
		FirecrackerEngine: &model.FirecrackerEngineConfig{
			RootFS:      "/fake/rootfs.ext4",
			KernelImage: "/fake/vmlinux",
		},
		Resources: model.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 10},
	}
}

func TestLifecycle(t *testing.T) {
	eng, err := fake.NewEngine(fake.EngineConfig{Logger: log.Noop})
	require.NoError(t, err)

	sb, err := eng.Create(context.Background(), testConfig("test"))
	require.NoError(t, err)
	require.Equal(t, model.SandboxStatusStopped, sb.Status)

	err = eng.Start(context.Background(), sb.ID)
	require.NoError(t, err)

	status, err := eng.Status(context.Background(), sb.ID)
	require.NoError(t, err)
	assert.Equal(t, model.SandboxStatusRunning, status.Status)

	err = eng.Stop(context.Background(), sb.ID)
	require.NoError(t, err)

	status, err = eng.Status(context.Background(), sb.ID)
	require.NoError(t, err)
	assert.Equal(t, model.SandboxStatusStopped, status.Status)

	err = eng.Remove(context.Background(), sb.ID)
	require.NoError(t, err)
}

func TestExecRequiresRunningSandbox(t *testing.T) {
	eng, err := fake.NewEngine(fake.EngineConfig{Logger: log.Noop})
	require.NoError(t, err)

	sb, err := eng.Create(context.Background(), testConfig("test"))
	require.NoError(t, err)

	_, err = eng.Exec(context.Background(), sb.ID, []string{"echo", "ok"}, model.ExecOpts{})
	require.Error(t, err)
	assert.ErrorIs(t, err, model.ErrNotValid)
}
