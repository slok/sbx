package io

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/model"
)

func TestSessionYAMLRepository_GetSessionConfig(t *testing.T) {
	tests := map[string]struct {
		fs     fstest.MapFS
		path   string
		expCfg model.SessionConfig
		expErr bool
		errMsg string
	}{
		"Valid session config with name should load successfully": {
			fs: fstest.MapFS{
				"session.yaml": &fstest.MapFile{
					Data: []byte(`name: dev-session
`),
				},
			},
			path: "session.yaml",
			expCfg: model.SessionConfig{
				Name: "dev-session",
			},
			expErr: false,
		},
		"Valid session config with env should load successfully": {
			fs: fstest.MapFS{
				"session.yaml": &fstest.MapFile{
					Data: []byte(`name: dev-session
env:
  FOO: bar
  BAZ: qux
`),
				},
			},
			path: "session.yaml",
			expCfg: model.SessionConfig{
				Name: "dev-session",
				Env: map[string]string{
					"FOO": "bar",
					"BAZ": "qux",
				},
			},
			expErr: false,
		},
		"Empty session config should load successfully": {
			fs: fstest.MapFS{
				"empty.yaml": &fstest.MapFile{
					Data: []byte(`---
`),
				},
			},
			path:   "empty.yaml",
			expCfg: model.SessionConfig{},
			expErr: false,
		},
		"Missing file should return error": {
			fs:     fstest.MapFS{},
			path:   "nonexistent.yaml",
			expErr: true,
			errMsg: "reading session config file",
		},
		"Invalid YAML should return error": {
			fs: fstest.MapFS{
				"invalid.yaml": &fstest.MapFile{
					Data: []byte(`invalid: yaml: content: {}`),
				},
			},
			path:   "invalid.yaml",
			expErr: true,
			errMsg: "parsing YAML",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			repo := NewSessionYAMLRepository(tc.fs)
			cfg, err := repo.GetSessionConfig(context.Background(), tc.path)

			if tc.expErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errMsg)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.expCfg, cfg)
		})
	}
}

func TestSessionYAMLRepository_GetSessionConfig_ContextCancellation(t *testing.T) {
	fs := fstest.MapFS{
		"test.yaml": &fstest.MapFile{
			Data: []byte(`name: test-session
`),
		},
	}

	repo := NewSessionYAMLRepository(fs)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := repo.GetSessionConfig(ctx, "test.yaml")
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}
