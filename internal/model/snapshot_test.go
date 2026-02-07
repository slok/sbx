package model_test

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/slok/sbx/internal/model"
)

func TestSnapshotValidate(t *testing.T) {
	base := model.Snapshot{
		ID:                 "01ARZ3NDEKTSV4RRFFQ69G5FAV",
		Name:               "my-snapshot-1",
		Path:               "/home/user/.sbx/snapshots/01ARZ3NDEKTSV4RRFFQ69G5FAV.ext4",
		SourceSandboxID:    "01ARZ3NDEKTSV4RRFFQ69G5FAA",
		SourceSandboxName:  "sandbox-1",
		VirtualSizeBytes:   1024,
		AllocatedSizeBytes: 512,
		CreatedAt:          time.Now().UTC(),
	}

	tests := map[string]struct {
		snapshot model.Snapshot
		expErr   bool
	}{
		"valid snapshot": {
			snapshot: base,
		},
		"missing id": {
			snapshot: model.Snapshot{
				Name:      base.Name,
				Path:      base.Path,
				CreatedAt: base.CreatedAt,
			},
			expErr: true,
		},
		"invalid name": {
			snapshot: func() model.Snapshot {
				s := base
				s.Name = "bad name"
				return s
			}(),
			expErr: true,
		},
		"missing path": {
			snapshot: func() model.Snapshot {
				s := base
				s.Path = ""
				return s
			}(),
			expErr: true,
		},
		"negative virtual size": {
			snapshot: func() model.Snapshot {
				s := base
				s.VirtualSizeBytes = -1
				return s
			}(),
			expErr: true,
		},
		"negative allocated size": {
			snapshot: func() model.Snapshot {
				s := base
				s.AllocatedSizeBytes = -1
				return s
			}(),
			expErr: true,
		},
		"missing created at": {
			snapshot: func() model.Snapshot {
				s := base
				s.CreatedAt = time.Time{}
				return s
			}(),
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := test.snapshot.Validate()
			if test.expErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, model.ErrNotValid))
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestValidateSnapshotName(t *testing.T) {
	tests := map[string]struct {
		name   string
		expErr bool
	}{
		"valid simple":       {name: "snap"},
		"valid with symbols": {name: "snap-1.2_3"},
		"invalid empty":      {name: "", expErr: true},
		"invalid spaces":     {name: "snap one", expErr: true},
		"invalid slash":      {name: "snap/one", expErr: true},
		"invalid unicode":    {name: "snap-ñ", expErr: true},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			err := model.ValidateSnapshotName(test.name)
			if test.expErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, model.ErrNotValid))
				return
			}
			assert.NoError(t, err)
		})
	}
}
