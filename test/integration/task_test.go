package integration

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/app/create"
	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/sandbox/fake"
	"github.com/slok/sbx/internal/storage"
	"github.com/slok/sbx/internal/storage/sqlite"
)

func TestTaskTracking(t *testing.T) {
	tests := map[string]struct {
		operation    string
		expTaskNames []string
	}{
		"Create operation should track tasks": {
			operation:    "create",
			expTaskNames: []string{"create_sandbox"}, // Fake engine task name
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Setup temp database
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			// Initialize storage
			repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
				DBPath: dbPath,
				Logger: log.Noop,
			})
			require.NoError(err)

			// Initialize task repository
			taskRepo, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{
				DB:     repo.DB(),
				Logger: log.Noop,
			})
			require.NoError(err)

			// Initialize fake engine with task tracking
			eng, err := fake.NewEngine(fake.EngineConfig{
				TaskRepo: taskRepo,
				Logger:   log.Noop,
			})
			require.NoError(err)

			// Create service
			svc, err := create.NewService(create.ServiceConfig{
				Engine:     eng,
				Repository: repo,
				Logger:     log.Noop,
			})
			require.NoError(err)

			// Execute create
			cfg := model.SandboxConfig{
				Name: "test-sandbox",
				Resources: model.Resources{
					VCPUs:    1,
					MemoryMB: 512,
					DiskGB:   10,
				},
				DockerEngine: &model.DockerEngineConfig{
					Image: "ubuntu:22.04",
				},
			}

			sandbox, err := svc.Create(context.Background(), create.CreateOptions{
				Config: cfg,
			})
			require.NoError(err)
			require.NotNil(sandbox)

			// Verify tasks were created and completed
			db := repo.DB()

			// Check tasks exist
			query := `SELECT name, status FROM tasks WHERE sandbox_id = ? AND operation = ? ORDER BY sequence`
			rows, err := db.Query(query, sandbox.ID, test.operation)
			require.NoError(err)
			defer rows.Close()

			var tasks []struct {
				Name   string
				Status string
			}
			for rows.Next() {
				var tsk struct {
					Name   string
					Status string
				}
				err := rows.Scan(&tsk.Name, &tsk.Status)
				require.NoError(err)
				tasks = append(tasks, tsk)
			}

			// Verify we got the expected tasks
			assert.Len(tasks, len(test.expTaskNames))
			for i, expName := range test.expTaskNames {
				if i < len(tasks) {
					assert.Equal(expName, tasks[i].Name)
					assert.Equal(string(model.TaskStatusDone), tasks[i].Status)
				}
			}

			// Verify progress
			progress, err := taskRepo.Progress(context.Background(), sandbox.ID, test.operation)
			require.NoError(err)
			assert.Equal(len(test.expTaskNames), progress.Total)
			assert.Equal(len(test.expTaskNames), progress.Done)
		})
	}
}

func TestTaskPendingOperation(t *testing.T) {
	tests := map[string]struct {
		setupTasks func(taskRepo storage.TaskRepository, sandboxID string)
		expOp      string
		expPending bool
	}{
		"Should detect pending create operation": {
			setupTasks: func(taskRepo storage.TaskRepository, sandboxID string) {
				err := taskRepo.AddTask(context.Background(), sandboxID, "create", "test_task")
				if err != nil {
					panic(err)
				}
			},
			expOp:      "create",
			expPending: true,
		},

		"Should return false when all tasks completed": {
			setupTasks: func(taskRepo storage.TaskRepository, sandboxID string) {
				err := taskRepo.AddTask(context.Background(), sandboxID, "create", "test_task")
				if err != nil {
					panic(err)
				}
				tsk, err := taskRepo.NextTask(context.Background(), sandboxID, "create")
				if err != nil {
					panic(err)
				}
				if tsk != nil {
					err = taskRepo.CompleteTask(context.Background(), tsk.ID)
					if err != nil {
						panic(err)
					}
				}
			},
			expPending: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			// Setup temp database
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "test.db")

			// Initialize storage (this runs migrations)
			repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
				DBPath: dbPath,
				Logger: log.Noop,
			})
			require.NoError(err)

			// Initialize task manager with the same DB
			taskRepo, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{
				DB:     repo.DB(),
				Logger: log.Noop,
			})
			require.NoError(err)

			// Create a sandbox in the DB (required for FK constraint)
			sandboxID := "test-sandbox-123"
			testSandbox := model.Sandbox{
				ID:     sandboxID,
				Name:   "test",
				Status: model.SandboxStatusPending,
				Config: model.SandboxConfig{
					Name: "test",
					Resources: model.Resources{
						VCPUs:    1,
						MemoryMB: 512,
						DiskGB:   10,
					},
					DockerEngine: &model.DockerEngineConfig{
						Image: "ubuntu:22.04",
					},
				},
			}
			err = repo.CreateSandbox(context.Background(), testSandbox)
			require.NoError(err)

			// Setup tasks
			test.setupTasks(taskRepo, sandboxID)

			// Check for pending operation
			op, hasPending, err := taskRepo.HasPendingOperation(context.Background(), sandboxID)
			require.NoError(err)

			assert.Equal(test.expPending, hasPending)
			if test.expPending {
				assert.Equal(test.expOp, op)
			}
		})
	}
}

func TestTaskClearOperation(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	// Setup temp database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	// Initialize storage
	repo, err := sqlite.NewRepository(context.Background(), sqlite.RepositoryConfig{
		DBPath: dbPath,
		Logger: log.Noop,
	})
	require.NoError(err)

	// Initialize task manager
	taskRepo, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{
		DB:     repo.DB(),
		Logger: log.Noop,
	})
	require.NoError(err)

	// Add tasks
	sandboxID := "test-sandbox-123"
	err = taskRepo.AddTasks(context.Background(), sandboxID, "create", []string{"task1", "task2", "task3"})
	require.NoError(err)

	// Clear operation
	err = taskRepo.ClearOperation(context.Background(), sandboxID, "create")
	require.NoError(err)

	// Verify tasks are gone
	tsk, err := taskRepo.NextTask(context.Background(), sandboxID, "create")
	require.NoError(err)
	assert.Nil(tsk)
}
