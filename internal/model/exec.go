package model

import "io"

// ExecOpts contains options for executing a command in a sandbox.
type ExecOpts struct {
	// WorkingDir is the directory to run the command in (optional).
	WorkingDir string
	// Env contains additional environment variables for this exec.
	Env map[string]string
	// Stdin is the input stream for the command (optional).
	Stdin io.Reader
	// Stdout is the output stream for the command (optional, defaults to discard).
	Stdout io.Writer
	// Stderr is the error stream for the command (optional, defaults to discard).
	Stderr io.Writer
	// Tty allocates a pseudo-TTY for the command (useful for interactive shells).
	Tty bool
}

// ExecResult contains the result of an exec operation.
type ExecResult struct {
	// ExitCode is the exit code of the executed command.
	ExitCode int
}
