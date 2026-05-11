package sandbox

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const defaultContainerImage = "alpine:3.20"

// ContainerRunner runs commands in Docker or Podman with a read-only root and a workspace bind mount.
type ContainerRunner struct {
	Image string
}

// NewContainerRunner returns a runner that uses Docker or Podman when available.
func NewContainerRunner(image string) *ContainerRunner {
	return &ContainerRunner{Image: strings.TrimSpace(image)}
}

func (c *ContainerRunner) Name() string { return "container" }

func (c *ContainerRunner) image() string {
	if c.Image != "" {
		return c.Image
	}
	return defaultContainerImage
}

func (c *ContainerRunner) runtimePath() (string, error) {
	return containerRuntimePath()
}

// Available reports whether a container runtime is installed.
func (c *ContainerRunner) Available() bool {
	_, err := c.runtimePath()
	return err == nil
}

// Exec runs argv inside a disposable container. workDir is mounted at /workspace read-write.
func (c *ContainerRunner) Exec(ctx context.Context, policy Policy, workDir string, argv []string, env []string, timeout time.Duration, stdout, stderr io.Writer) (int, error) {
	if len(argv) == 0 {
		return -1, fmt.Errorf("sandbox: empty argv")
	}
	rt, err := c.runtimePath()
	if err != nil {
		return -1, err
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return -1, err
	}

	envFile, err := os.CreateTemp("", "codient-sandbox-env-*")
	if err != nil {
		return -1, err
	}
	envPath := envFile.Name()
	defer os.Remove(envPath)
	for _, e := range env {
		if _, err := envFile.WriteString(e + "\n"); err != nil {
			envFile.Close()
			return -1, err
		}
	}
	if err := envFile.Close(); err != nil {
		return -1, err
	}

	args := []string{
		"run", "--rm",
		NetworkFlag(policy),
		"-w", "/workspace",
		"-v", volumeMountArg(absWork),
		"--env-file", envPath,
	}
	if policy.MaxMemoryMB > 0 {
		args = append(args, "--memory="+strconv.Itoa(policy.MaxMemoryMB)+"m")
	}
	if policy.MaxCPUPercent > 0 && policy.MaxCPUPercent <= 100 {
		cpus := float64(policy.MaxCPUPercent) / 100.0
		args = append(args, fmt.Sprintf("--cpus=%g", cpus))
	}
	if policy.MaxProcesses > 0 {
		args = append(args, fmt.Sprintf("--pids-limit=%d", policy.MaxProcesses))
	}
	args = append(args, "--read-only")
	args = append(args, c.image())
	args = append(args, argv...)

	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, rt, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return -1, fmt.Errorf("command timed out after %v", timeout)
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return -1, runCtx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

func volumeMountArg(hostPath string) string {
	// Docker Desktop on Windows accepts forward slashes; use clean path.
	if runtime.GOOS == "windows" {
		return hostPath + ":/workspace:rw"
	}
	return hostPath + ":/workspace:rw"
}

// ContainerSession keeps a single container running for a delegate's lifetime.
// Commands are executed with "docker exec" instead of "docker run", preserving
// filesystem state (caches, build artifacts) across calls.
type ContainerSession struct {
	rt          string // docker or podman binary path
	containerID string
	image       string
	policy      Policy
}

// StartContainerSession starts a long-lived container with workDir mounted at
// /workspace. The container runs "sleep infinity" and commands are later
// dispatched via Exec. Close must be called when done.
func StartContainerSession(ctx context.Context, image, workDir string, policy Policy) (*ContainerSession, error) {
	if image == "" {
		image = defaultContainerImage
	}
	rt, err := containerRuntimePath()
	if err != nil {
		return nil, err
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	args := BuildContainerSessionStartArgs(image, absWork, policy)
	cmd := exec.CommandContext(ctx, rt, args...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("container start: %w: %s", err, string(ee.Stderr))
		}
		return nil, fmt.Errorf("container start: %w", err)
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return nil, fmt.Errorf("container start: empty container ID")
	}
	return &ContainerSession{
		rt:          rt,
		containerID: id,
		image:       image,
		policy:      policy,
	}, nil
}

// Exec runs argv inside the long-lived container via "docker exec".
func (s *ContainerSession) Exec(ctx context.Context, argv []string, env []string, stdout, stderr io.Writer, timeout time.Duration) (int, error) {
	if len(argv) == 0 {
		return -1, fmt.Errorf("sandbox: empty argv")
	}
	args := BuildContainerSessionExecArgs(s.containerID, argv, env)

	runCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(runCtx, s.rt, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return -1, fmt.Errorf("command timed out after %v", timeout)
		}
		if errors.Is(runCtx.Err(), context.Canceled) {
			return -1, runCtx.Err()
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// Close stops and removes the container.
func (s *ContainerSession) Close(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, s.rt, "rm", "-f", s.containerID)
	return cmd.Run()
}

// ID returns the container ID for diagnostics.
func (s *ContainerSession) ID() string { return s.containerID }

// BuildContainerSessionStartArgs returns the docker/podman argv to start a
// long-lived container (for unit tests that assert argv without running docker).
func BuildContainerSessionStartArgs(image, absWorkDir string, policy Policy) []string {
	args := []string{
		"run", "-d", "--rm",
		NetworkFlag(policy),
		"-w", "/workspace",
		"-v", volumeMountArg(absWorkDir),
	}
	if policy.MaxMemoryMB > 0 {
		args = append(args, "--memory="+strconv.Itoa(policy.MaxMemoryMB)+"m")
	}
	if policy.MaxCPUPercent > 0 && policy.MaxCPUPercent <= 100 {
		cpus := float64(policy.MaxCPUPercent) / 100.0
		args = append(args, fmt.Sprintf("--cpus=%g", cpus))
	}
	if policy.MaxProcesses > 0 {
		args = append(args, fmt.Sprintf("--pids-limit=%d", policy.MaxProcesses))
	}
	args = append(args, image, "sleep", "infinity")
	return args
}

// BuildContainerSessionExecArgs returns the docker/podman argv to exec a
// command inside a running container (for unit tests).
func BuildContainerSessionExecArgs(containerID string, argv []string, env []string) []string {
	args := []string{"exec", "-w", "/workspace"}
	for _, e := range env {
		args = append(args, "-e", e)
	}
	args = append(args, containerID)
	args = append(args, argv...)
	return args
}

// SessionRunner adapts a ContainerSession to the sandbox.Runner interface.
type SessionRunner struct {
	Sess *ContainerSession
}

func (r *SessionRunner) Name() string    { return "container-session" }
func (r *SessionRunner) Available() bool { return r.Sess != nil }

func (r *SessionRunner) Exec(ctx context.Context, policy Policy, workDir string, argv []string, env []string, timeout time.Duration, stdout, stderr io.Writer) (int, error) {
	if r.Sess == nil {
		return -1, fmt.Errorf("container session not started")
	}
	return r.Sess.Exec(ctx, argv, env, stdout, stderr, timeout)
}

// containerRuntimePath finds docker or podman on PATH (shared by
// ContainerRunner.runtimePath and ContainerSession startup).
func containerRuntimePath() (string, error) {
	if p, err := exec.LookPath("docker"); err == nil {
		return p, nil
	}
	if p, err := exec.LookPath("podman"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("docker or podman not found in PATH")
}

// BuildContainerRunArgs returns the docker/podman argv for tests (no execution).
func BuildContainerRunArgs(rt string, image string, workDir string, policy Policy, argv []string, envFile string) ([]string, error) {
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	if image == "" {
		image = defaultContainerImage
	}
	args := []string{
		rt,
		"run", "--rm",
		NetworkFlag(policy),
		"-w", "/workspace",
		"-v", volumeMountArg(absWork),
		"--env-file", envFile,
	}
	if policy.MaxMemoryMB > 0 {
		args = append(args, "--memory="+strconv.Itoa(policy.MaxMemoryMB)+"m")
	}
	if policy.MaxCPUPercent > 0 && policy.MaxCPUPercent <= 100 {
		cpus := float64(policy.MaxCPUPercent) / 100.0
		args = append(args, fmt.Sprintf("--cpus=%g", cpus))
	}
	if policy.MaxProcesses > 0 {
		args = append(args, fmt.Sprintf("--pids-limit=%d", policy.MaxProcesses))
	}
	args = append(args, "--read-only", image)
	args = append(args, argv...)
	return args, nil
}
