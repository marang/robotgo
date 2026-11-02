// Package command provides the bounded lifecycle used by RobotGo's one-shot
// external command backends.
package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

// CleanupDelay bounds the time spent waiting for inherited command I/O to
// close after the direct process exits or its context is canceled.
const CleanupDelay = 250 * time.Millisecond

// Output runs a one-shot command and returns its standard output. It returns
// exec.ErrWaitDelay when inherited I/O remains open past CleanupDelay.
func Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := newContextCommand(ctx, name, args...)
	output, err := cmd.Output()
	return output, finish(cmd, err)
}

// Run runs a one-shot command with the supplied standard input. It returns
// exec.ErrWaitDelay when inherited I/O remains open past CleanupDelay.
func Run(ctx context.Context, stdin io.Reader, name string, args ...string) error {
	return RunWithCleanup(ctx, stdin, nil, nil, name, args...)
}

// RunWithCleanup is Run with a child environment and an optional finalizer.
// A nil environment inherits the parent environment, which is never changed.
// After running, command errors terminate the process tree before cleanup is
// called exactly once, including on start failure. A cleanup error also
// terminates an otherwise successful command's tree. All errors are retained.
// Success preserves background owners once their inherited I/O has closed;
// canceling the context after this handoff does not revoke those owners.
func RunWithCleanup(ctx context.Context, stdin io.Reader, environment []string, cleanup func() error, name string, args ...string) error {
	cmd := newContextCommand(ctx, name, args...)
	cmd.Stdin = stdin
	cmd.Env = environment
	commandErr := cmd.Run()
	if commandErr != nil && ctx != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			commandErr = errors.Join(commandErr, contextErr)
		}
	}
	commandErr = finish(cmd, commandErr)
	if cleanup == nil {
		return commandErr
	}
	cleanupErr := cleanup()
	if cleanupErr == nil {
		return commandErr
	}
	if commandErr == nil {
		return finish(cmd, cleanupErr)
	}
	return errors.Join(commandErr, cleanupErr)
}

func newContextCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = CleanupDelay
	configureProcessTree(cmd)
	return cmd
}

func finish(cmd *exec.Cmd, commandErr error) error {
	if commandErr == nil {
		return nil
	}
	cleanupErr := terminateProcessTree(cmd)
	if cleanupErr == nil || errors.Is(cleanupErr, os.ErrProcessDone) {
		return commandErr
	}
	return errors.Join(
		commandErr,
		fmt.Errorf("terminate external command process tree: %w", cleanupErr),
	)
}
