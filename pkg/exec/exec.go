package exec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
)

func LocalExecContext(ctx context.Context, command string, out ...io.Writer) error {
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", command)
	var errOut bytes.Buffer
	cmd.Stderr = &errOut

	if len(out) > 0 {
		cmd.Stdout = out[0]
	}

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("local exec: %s %w", errOut.String(), err)
	}
	return nil
}
