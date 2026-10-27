package framework

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// PortForward manages a background "oc port-forward" process.
type PortForward struct {
	cmd *exec.Cmd
}

// StartPortForward starts "oc port-forward" for the adapter service and
// blocks until the forwarding line appears in stdout or ctx expires.
// The process outlives ctx; the caller must call Stop when done.
func StartPortForward(ctx context.Context, namespace, localAddr string) (*PortForward, error) {
	parts := strings.SplitN(localAddr, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid local address %q", localAddr)
	}
	localPort := parts[1]

	cmd := exec.Command("oc", "-n", namespace, "port-forward",
		"service/lightspeed-agentic-alerts-adapter",
		localPort+":8080", "--address=127.0.0.1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start port-forward: %w", err)
	}

	ready := make(chan error, 1)
	expected := fmt.Sprintf("Forwarding from %s -> 8080", localAddr)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.TrimSpace(scanner.Text()) == expected {
				ready <- nil
				return
			}
		}
		ready <- fmt.Errorf("port-forward exited before becoming ready")
	}()

	select {
	case err := <-ready:
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return nil, err
		}
		return &PortForward{cmd: cmd}, nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("timed out waiting for port-forward: %w", ctx.Err())
	}
}

func (pf *PortForward) Stop() {
	if pf.cmd != nil && pf.cmd.Process != nil {
		_ = pf.cmd.Process.Kill()
		_ = pf.cmd.Wait()
	}
}
