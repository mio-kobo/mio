//go:build unix

package bunkoscaffold

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Real SIGINT stays an integration check in its own process. The portable
// rollback tests use context cancellation on every host, including Windows.
func TestScaffoldSIGINTProcess(t *testing.T) {
	const helper = "MIO_SCAFFOLD_SIGINT_TEST_ROOT"
	if root := os.Getenv(helper); root != "" {
		ctx, stop := interruptContext()
		defer stop()
		writes := 0
		_, err := generateContext(ctx, root, fixture(), func(name string, data []byte, mode os.FileMode) error {
			if err := os.WriteFile(name, data, mode); err != nil {
				return err
			}
			writes++
			process, err := os.FindProcess(os.Getpid())
			if err != nil {
				return err
			}
			if err := process.Signal(os.Interrupt); err != nil {
				return err
			}
			// Wait for delivery, rather than sleeping and racing the signal goroutine.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(5 * time.Second):
				return errors.New("SIGINT was not delivered")
			}
		})
		var own *Error
		if !errors.As(err, &own) || own.Kind != "cancelled" || writes != 1 {
			t.Fatalf("writes=%d error=%v", writes, err)
		}
		return
	}
	root := t.TempDir()
	keep := filepath.Join(root, "keep")
	if err := os.WriteFile(keep, []byte("existing user file"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScaffoldSIGINTProcess$", "-test.count=1")
	child.Env = append(os.Environ(), helper+"="+root)
	if output, err := child.CombinedOutput(); err != nil {
		t.Fatalf("SIGINT child: %v\n%s", err, output)
	}
	if _, err := os.Lstat(filepath.Join(root, "json")); !os.IsNotExist(err) {
		t.Fatal("SIGINT left a partial project", err)
	}
	data, err := os.ReadFile(keep)
	if err != nil || string(data) != "existing user file" {
		t.Fatal("SIGINT changed existing files", err)
	}
}
