//go:build !linux

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func projectRoot(t *testing.T) string {
	t.Helper()
	// internal/sandbox -> project root is ../../
	dir, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve project root: %v", err)
	}
	return dir
}

func TestLinuxSandbox_ViaContainer(t *testing.T) {
	if os.Getenv("SKIP_CONTAINER_TESTS") != "" {
		t.Skip("SKIP_CONTAINER_TESTS set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	root := projectRoot(t)

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    filepath.Join(root, "internal", "sandbox", "testdata"),
			Dockerfile: "Dockerfile",
		},
		Mounts: testcontainers.Mounts(
			testcontainers.BindMount(root, "/workspace"),
		),
		Cmd: []string{
			"go", "test", "-v", "-count=1",
			"-tags", "integration",
			"./internal/sandbox/...",
		},
		Privileged: true, // needed for bwrap namespace operations
		WaitingFor: wait.ForExit(),
	}

	container, err := testcontainers.GenericContainer(ctx,
		testcontainers.GenericContainerRequest{
			ContainerRequest: req,
			Started:          true,
		})
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	defer func() {
		_ = container.Terminate(ctx)
	}()

	// Read logs
	logs, err := container.Logs(ctx)
	if err != nil {
		t.Fatalf("read container logs: %v", err)
	}
	defer logs.Close()

	buf := make([]byte, 64*1024)
	for {
		n, readErr := logs.Read(buf)
		if n > 0 {
			t.Log(string(buf[:n]))
		}
		if readErr != nil {
			break
		}
	}

	// Check exit code
	state, err := container.State(ctx)
	if err != nil {
		t.Fatalf("get container state: %v", err)
	}
	if state.ExitCode != 0 {
		t.Fatalf("linux tests failed with exit code %d", state.ExitCode)
	}
}
