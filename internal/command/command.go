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
	cmd := newContextCommand(ctx, name, args...)
	cmd.Stdin = stdin
	return finish(cmd, cmd.Run())
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
