package testutils

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var multiSpaceRegex = regexp.MustCompile(" +")

// RunSBX executes an sbx command with the given arguments string (split by spaces).
// Use RunSBXArgs when arguments contain spaces that should be preserved.
func RunSBX(ctx context.Context, env []string, binary, cmdArgs string, nolog bool) (stdout, stderr []byte, err error) {
	// Sanitize command.
	cmdArgs = strings.TrimSpace(cmdArgs)
	cmdArgs = multiSpaceRegex.ReplaceAllString(cmdArgs, " ")

	// Split into args.
	var args []string
	if cmdArgs != "" {
		args = strings.Split(cmdArgs, " ")
	}

	return RunSBXArgs(ctx, env, binary, args, nolog)
}

// RunSBXArgs executes an sbx command with pre-split arguments.
// This preserves arguments that contain spaces (e.g., sh -c "echo hello > /tmp/file").
func RunSBXArgs(ctx context.Context, env []string, binary string, args []string, nolog bool) (stdout, stderr []byte, err error) {
	var outData, errData bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout = &outData
	cmd.Stderr = &errData

	// Set env: os.Environ() first, then custom env overrides on top.
	// In Go's exec.Cmd, when duplicate keys exist, the last one wins.
	newEnv := append([]string{}, os.Environ()...)
	newEnv = append(newEnv, env...)
	if nolog {
		newEnv = append(newEnv, "SBX_NO_LOG=true")
	}
	cmd.Env = newEnv

	err = cmd.Run()

	return outData.Bytes(), errData.Bytes(), err
}
