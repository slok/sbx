package printer

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	tests := map[string]struct {
		input int64
		exp   string
	}{
		"zero bytes": {
			input: 0,
			exp:   "0 B",
		},
		"negative bytes should return zero": {
			input: -100,
			exp:   "0 B",
		},
		"small bytes": {
			input: 512,
			exp:   "512 B",
		},
		"one kilobyte": {
			input: 1024,
			exp:   "1.0 KB",
		},
		"kilobytes": {
			input: 1536,
			exp:   "1.5 KB",
		},
		"one megabyte": {
			input: 1024 * 1024,
			exp:   "1.0 MB",
		},
		"hundreds of megabytes": {
			input: 700 * 1024 * 1024,
			exp:   "700.0 MB",
		},
		"one gigabyte": {
			input: 1024 * 1024 * 1024,
			exp:   "1.0 GB",
		},
		"ten gigabytes": {
			input: 10 * 1024 * 1024 * 1024,
			exp:   "10.0 GB",
		},
		"one terabyte": {
			input: 1024 * 1024 * 1024 * 1024,
			exp:   "1.0 TB",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, test.exp, FormatBytes(test.input))
		})
	}
}
