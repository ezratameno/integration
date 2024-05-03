package exec

import (
	"context"
	"fmt"
	"io"
	"os/exec"
)

func LocalExecContext(ctx context.Context, command string, out ...io.Writer) error {
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", command)

	if len(out) > 0 {
		cmd.Stdout = out[0]

		// TODO: i did it only because flux write the logs to stderr
		cmd.Stderr = out[0]

	}

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("local exec: %w", err)
	}
	return nil
}
