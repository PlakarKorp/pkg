package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCmdArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{"no argument", nil},
		{"two arguments", []string{"a.ptar", "b.ptar"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout, stderr bytes.Buffer
			cmd := newRootCmd()
			cmd.SetArgs(tt.args)
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)

			require.ErrorContains(t, cmd.Execute(), "accepts 1 arg(s)")
			require.Empty(t, stdout.String())
		})
	}
}
