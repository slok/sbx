package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
	"github.com/slok/sbx/internal/storage"
	"github.com/slok/sbx/internal/storage/sqlite"
	"github.com/slok/sbx/internal/storage/sqlite/migrations"
)

func getTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Create temp database
	tmpFile, err := os.CreateTemp("", "sbx-test-*.db")
	require.NoError(t, err)
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	// Open database
	db, err := sql.Open("sqlite", tmpFile.Name())
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	// Run migrations
	migrator, err := migrations.NewMigrator(db, log.Noop)
	require.NoError(t, err)
	err = migrator.Up(context.Background())
	require.NoError(t, err)

	return db
}

func TestAddTask(t *testing.T) {
	tests := map[string]struct {
		sandboxID string
		operation string
		name      string
		expErr    bool
	}{
		"Adding a single task should work": {
			sandboxID: "sandbox-1",
			operation: "create",
			name:      "pull_image",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			err = mgr.AddTask(context.Background(), test.sandboxID, test.operation, test.name)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)

				// Verify task was added
				tsk, err := mgr.NextTask(context.Background(), test.sandboxID, test.operation)
				require.NoError(err)
				require.NotNil(tsk)
				assert.Equal(test.sandboxID, tsk.SandboxID)
				assert.Equal(test.operation, tsk.Operation)
				assert.Equal(test.name, tsk.Name)
				assert.Equal(1, tsk.Sequence)
				assert.Equal(model.TaskStatusPending, tsk.Status)
			}
		})
	}
}

func TestAddTasks(t *testing.T) {
	tests := map[string]struct {
		sandboxID string
		operation string
		names     []string
		expSeqs   []int
		expErr    bool
	}{
		"Adding multiple tasks should assign sequential numbers": {
			sandboxID: "sandbox-1",
			operation: "create",
			names:     []string{"pull_image", "create_container", "start_container"},
			expSeqs:   []int{1, 2, 3},
		},

		"Adding tasks to existing operation should continue sequence": {
			sandboxID: "sandbox-1",
			operation: "create",
			names:     []string{"task4", "task5"},
			expSeqs:   []int{1, 2, 3, 4, 5},
		},

		"Adding empty task list should not fail": {
			sandboxID: "sandbox-1",
			operation: "create",
			names:     []string{},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			// For the "continue sequence" test, add initial tasks
			if name == "Adding tasks to existing operation should continue sequence" {
				err = mgr.AddTasks(context.Background(), test.sandboxID, test.operation, []string{"task1", "task2", "task3"})
				require.NoError(err)
			}

			err = mgr.AddTasks(context.Background(), test.sandboxID, test.operation, test.names)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)

				// Verify tasks were added with correct sequences
				for _, expSeq := range test.expSeqs {
					tsk, err := mgr.NextTask(context.Background(), test.sandboxID, test.operation)
					require.NoError(err)
					require.NotNil(tsk)
					assert.Equal(expSeq, tsk.Sequence)
					// Mark as done to get next
					err = mgr.CompleteTask(context.Background(), tsk.ID)
					require.NoError(err)
				}
			}
		})
	}
}

func TestNextTask(t *testing.T) {
	tests := map[string]struct {
		setup     func(mgr storage.TaskRepository)
		sandboxID string
		operation string
		expName   string
		expNil    bool
		expErr    bool
	}{
		"NextTask should return tasks in sequence order": {
			setup: func(mgr storage.TaskRepository) {
				err := mgr.AddTasks(context.Background(), "sandbox-1", "create", []string{"task1", "task2", "task3"})
				if err != nil {
					panic(err)
				}
			},
			sandboxID: "sandbox-1",
			operation: "create",
			expName:   "task1",
		},

		"NextTask should return nil when no pending tasks": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
				tsk, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				_ = mgr.CompleteTask(context.Background(), tsk.ID)
			},
			sandboxID: "sandbox-1",
			operation: "create",
			expNil:    true,
		},

		"NextTask should skip failed tasks and return next pending": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTasks(context.Background(), "sandbox-1", "create", []string{"task1", "task2", "task3"})
				tsk, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				_ = mgr.FailTask(context.Background(), tsk.ID, fmt.Errorf("test error"))
			},
			sandboxID: "sandbox-1",
			operation: "create",
			expName:   "task2",
		},

		"NextTask for non-existent operation should return nil": {
			setup:     func(mgr storage.TaskRepository) {},
			sandboxID: "sandbox-1",
			operation: "nonexistent",
			expNil:    true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			test.setup(mgr)

			tsk, err := mgr.NextTask(context.Background(), test.sandboxID, test.operation)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				if test.expNil {
					assert.Nil(tsk)
				} else {
					require.NotNil(tsk)
					assert.Equal(test.expName, tsk.Name)
					assert.Equal(model.TaskStatusPending, tsk.Status)
				}
			}
		})
	}
}

func TestCompleteTask(t *testing.T) {
	tests := map[string]struct {
		setup  func(mgr storage.TaskRepository) string
		expErr bool
	}{
		"Completing a pending task should update status": {
			setup: func(mgr storage.TaskRepository) string {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
				tsk, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				return tsk.ID
			},
		},

		"Completing a non-existent task should fail": {
			setup: func(mgr storage.TaskRepository) string {
				return "non-existent-id"
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			taskID := test.setup(mgr)

			err = mgr.CompleteTask(context.Background(), taskID)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
			}
		})
	}
}

func TestFailTask(t *testing.T) {
	tests := map[string]struct {
		setup    func(mgr storage.TaskRepository) string
		taskErr  error
		expErr   bool
		expErrIn string
	}{
		"Failing a pending task should update status and error": {
			setup: func(mgr storage.TaskRepository) string {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
				tsk, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				return tsk.ID
			},
			taskErr:  fmt.Errorf("test error message"),
			expErrIn: "test error message",
		},

		"Failing a task with nil error should work": {
			setup: func(mgr storage.TaskRepository) string {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
				tsk, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				return tsk.ID
			},
			taskErr:  nil,
			expErrIn: "",
		},

		"Failing a non-existent task should fail": {
			setup: func(mgr storage.TaskRepository) string {
				return "non-existent-id"
			},
			taskErr: fmt.Errorf("test error"),
			expErr:  true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			taskID := test.setup(mgr)

			err = mgr.FailTask(context.Background(), taskID, test.taskErr)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)

				// Verify error was stored (query db directly)
				var storedErr string
				query := `SELECT error FROM tasks WHERE id = ?`
				err = db.QueryRow(query, taskID).Scan(&storedErr)
				require.NoError(err)
				assert.Equal(test.expErrIn, storedErr)
			}
		})
	}
}

func TestProgress(t *testing.T) {
	tests := map[string]struct {
		setup     func(mgr storage.TaskRepository)
		sandboxID string
		operation string
		expDone   int
		expTotal  int
		expErr    bool
	}{
		"Progress should count done and total tasks": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTasks(context.Background(), "sandbox-1", "create", []string{"task1", "task2", "task3"})
				tsk1, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				_ = mgr.CompleteTask(context.Background(), tsk1.ID)
			},
			sandboxID: "sandbox-1",
			operation: "create",
			expDone:   1,
			expTotal:  3,
		},

		"Progress for operation with no tasks should return zero": {
			setup:     func(mgr storage.TaskRepository) {},
			sandboxID: "sandbox-1",
			operation: "create",
			expDone:   0,
			expTotal:  0,
		},

		"Progress should count all done tasks": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTasks(context.Background(), "sandbox-1", "create", []string{"task1", "task2", "task3"})
				tsk1, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				_ = mgr.CompleteTask(context.Background(), tsk1.ID)
				tsk2, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				_ = mgr.CompleteTask(context.Background(), tsk2.ID)
				tsk3, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				_ = mgr.CompleteTask(context.Background(), tsk3.ID)
			},
			sandboxID: "sandbox-1",
			operation: "create",
			expDone:   3,
			expTotal:  3,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			test.setup(mgr)

			progress, err := mgr.Progress(context.Background(), test.sandboxID, test.operation)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				require.NotNil(progress)
				assert.Equal(test.expDone, progress.Done)
				assert.Equal(test.expTotal, progress.Total)
			}
		})
	}
}

func TestHasPendingOperation(t *testing.T) {
	tests := map[string]struct {
		setup      func(mgr storage.TaskRepository)
		sandboxID  string
		expOp      string
		expPending bool
		expErr     bool
	}{
		"Should detect pending operation": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
			},
			sandboxID:  "sandbox-1",
			expOp:      "create",
			expPending: true,
		},

		"Should return false when no pending operations": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
				tsk, _ := mgr.NextTask(context.Background(), "sandbox-1", "create")
				_ = mgr.CompleteTask(context.Background(), tsk.ID)
			},
			sandboxID:  "sandbox-1",
			expPending: false,
		},

		"Should return false for non-existent sandbox": {
			setup:      func(mgr storage.TaskRepository) {},
			sandboxID:  "non-existent",
			expPending: false,
		},

		"Should return first pending operation when multiple exist": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
				_ = mgr.AddTask(context.Background(), "sandbox-1", "start", "task2")
			},
			sandboxID:  "sandbox-1",
			expOp:      "create",
			expPending: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			test.setup(mgr)

			op, hasPending, err := mgr.HasPendingOperation(context.Background(), test.sandboxID)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)
				assert.Equal(test.expPending, hasPending)
				if test.expPending {
					assert.Equal(test.expOp, op)
				}
			}
		})
	}
}

func TestClearOperation(t *testing.T) {
	tests := map[string]struct {
		setup     func(mgr storage.TaskRepository)
		sandboxID string
		operation string
		expErr    bool
	}{
		"Clearing operation should delete all tasks": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTasks(context.Background(), "sandbox-1", "create", []string{"task1", "task2", "task3"})
			},
			sandboxID: "sandbox-1",
			operation: "create",
		},

		"Clearing non-existent operation should not fail": {
			setup:     func(mgr storage.TaskRepository) {},
			sandboxID: "sandbox-1",
			operation: "create",
		},

		"Clearing should only affect specified operation": {
			setup: func(mgr storage.TaskRepository) {
				_ = mgr.AddTask(context.Background(), "sandbox-1", "create", "task1")
				_ = mgr.AddTask(context.Background(), "sandbox-1", "start", "task2")
			},
			sandboxID: "sandbox-1",
			operation: "create",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			db := getTestDB(t)
			mgr, err := sqlite.NewTaskRepository(sqlite.TaskRepositoryConfig{DB: db, Logger: log.Noop})
			require.NoError(err)

			test.setup(mgr)

			err = mgr.ClearOperation(context.Background(), test.sandboxID, test.operation)

			if test.expErr {
				assert.Error(err)
			} else {
				assert.NoError(err)

				// Verify tasks were deleted
				tsk, err := mgr.NextTask(context.Background(), test.sandboxID, test.operation)
				require.NoError(err)
				assert.Nil(tsk)

				// For the "only affect specified operation" test, verify other operation still exists
				if name == "Clearing should only affect specified operation" {
					tsk, err := mgr.NextTask(context.Background(), test.sandboxID, "start")
					require.NoError(err)
					assert.NotNil(tsk)
					assert.Equal("task2", tsk.Name)
				}
			}
		})
	}
}
