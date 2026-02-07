package storage

import (
	"context"

	"github.com/slok/sbx/internal/model"
)

// Repository is the interface for sandbox persistence.
type Repository interface {
	CreateSandbox(ctx context.Context, s model.Sandbox) error
	GetSandbox(ctx context.Context, id string) (*model.Sandbox, error)
	GetSandboxByName(ctx context.Context, name string) (*model.Sandbox, error)
	ListSandboxes(ctx context.Context) ([]model.Sandbox, error)
	UpdateSandbox(ctx context.Context, s model.Sandbox) error
	DeleteSandbox(ctx context.Context, id string) error

	CreateSnapshot(ctx context.Context, s model.Snapshot) error
	GetSnapshot(ctx context.Context, id string) (*model.Snapshot, error)
	GetSnapshotByName(ctx context.Context, name string) (*model.Snapshot, error)
	ListSnapshots(ctx context.Context) ([]model.Snapshot, error)
}
