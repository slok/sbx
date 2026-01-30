package engine

import (
	"context"

	"github.com/slok/sbx/internal/model"
)

// Engine is the interface for sandbox lifecycle management.
type Engine interface {
	Create(ctx context.Context, cfg model.SandboxConfig) (*model.Sandbox, error)
	Start(ctx context.Context, id string) error
	Stop(ctx context.Context, id string) error
	Remove(ctx context.Context, id string) error
	Status(ctx context.Context, id string) (*model.Sandbox, error)
}
