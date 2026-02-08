package image

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProgressWriterWithTotal(t *testing.T) {
	var dst bytes.Buffer
	var status bytes.Buffer

	pw := NewProgressWriter(&dst, &status, 100)

	n, err := pw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, 5, dst.Len())
	assert.NotEmpty(t, status.String())
	assert.Contains(t, status.String(), "%")

	pw.Finish()
}

func TestProgressWriterWithoutTotal(t *testing.T) {
	var dst bytes.Buffer
	var status bytes.Buffer

	pw := NewProgressWriter(&dst, &status, 0)

	_, err := pw.Write([]byte("data"))
	require.NoError(t, err)
	assert.Equal(t, 4, dst.Len())
	assert.Contains(t, status.String(), "downloaded")

	pw.Finish()
}
