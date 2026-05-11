package sandbox

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildContainerRunArgs_NetworkNone(t *testing.T) {
	dir := t.TempDir()
	args, err := BuildContainerRunArgs("docker", "alpine:3.20", dir, Policy{}, []string{"echo", "hi"}, "/tmp/env")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	if !strings.Contains(s, "--network=none") {
		t.Fatalf("missing --network=none: %s", s)
	}
	if !strings.Contains(s, "--read-only") {
		t.Fatalf("missing --read-only: %s", s)
	}
}

func TestBuildContainerRunArgs_NetworkBridge(t *testing.T) {
	dir := t.TempDir()
	p := Policy{NetworkPolicy: "bridge"}
	args, err := BuildContainerRunArgs("docker", "alpine:3.20", dir, p, []string{"echo"}, "/tmp/env")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	if !strings.Contains(s, "--network=bridge") {
		t.Fatalf("expected --network=bridge: %s", s)
	}
}

func TestBuildContainerRunArgs_NetworkHost(t *testing.T) {
	dir := t.TempDir()
	p := Policy{NetworkPolicy: "host"}
	args, err := BuildContainerRunArgs("docker", "alpine:3.20", dir, p, []string{"echo"}, "/tmp/env")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	if !strings.Contains(s, "--network=host") {
		t.Fatalf("expected --network=host: %s", s)
	}
}

func TestNetworkFlag_Default(t *testing.T) {
	if got := NetworkFlag(Policy{}); got != "--network=none" {
		t.Fatalf("empty policy: got %q", got)
	}
	if got := NetworkFlag(Policy{NetworkPolicy: "unknown"}); got != "--network=none" {
		t.Fatalf("unknown: got %q", got)
	}
}

func TestNetworkPolicyIsValid(t *testing.T) {
	for _, valid := range []string{"", "none", "bridge", "host", " Bridge "} {
		if !NetworkPolicyIsValid(valid) {
			t.Fatalf("expected valid: %q", valid)
		}
	}
	for _, invalid := range []string{"private", "custom"} {
		if NetworkPolicyIsValid(invalid) {
			t.Fatalf("expected invalid: %q", invalid)
		}
	}
}

func TestBuildContainerRunArgs_ResourceLimits(t *testing.T) {
	dir := t.TempDir()
	p := Policy{MaxMemoryMB: 512, MaxCPUPercent: 50, MaxProcesses: 100}
	args, err := BuildContainerRunArgs("podman", "img", dir, p, []string{"id"}, "/e")
	if err != nil {
		t.Fatal(err)
	}
	s := strings.Join(args, " ")
	for _, want := range []string{"--memory=512m", "--cpus=0.5", "--pids-limit=100"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in %s", want, s)
		}
	}
}

func TestBuildContainerRunArgs_WorkspaceVolume(t *testing.T) {
	dir := t.TempDir()
	abs, _ := filepath.Abs(dir)
	args, err := BuildContainerRunArgs("docker", "", abs, Policy{}, []string{"pwd"}, "/tmp/e")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-v" && strings.HasPrefix(args[i+1], abs) && strings.Contains(args[i+1], ":/workspace:rw") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("volume mount not found: %v", args)
	}
}

func TestContainerRunner_Available(t *testing.T) {
	c := NewContainerRunner("")
	_ = c.Available() // may be true or false depending on host
}

func TestBuildContainerSessionStartArgs_Basic(t *testing.T) {
	dir := t.TempDir()
	abs, _ := filepath.Abs(dir)
	args := BuildContainerSessionStartArgs("golang:1.22", abs, Policy{MaxMemoryMB: 1024})
	s := strings.Join(args, " ")
	if !strings.Contains(s, "run -d --rm") {
		t.Fatalf("expected run -d --rm: %s", s)
	}
	if !strings.Contains(s, "--network=none") {
		t.Fatalf("expected --network=none: %s", s)
	}
	if !strings.Contains(s, "--memory=1024m") {
		t.Fatalf("expected --memory=1024m: %s", s)
	}
	if !strings.Contains(s, "golang:1.22") {
		t.Fatalf("expected image: %s", s)
	}
	if !strings.Contains(s, "sleep infinity") {
		t.Fatalf("expected sleep infinity: %s", s)
	}
}

func TestBuildContainerSessionStartArgs_BridgeNetwork(t *testing.T) {
	dir := t.TempDir()
	abs, _ := filepath.Abs(dir)
	args := BuildContainerSessionStartArgs("alpine", abs, Policy{NetworkPolicy: "bridge"})
	s := strings.Join(args, " ")
	if !strings.Contains(s, "--network=bridge") {
		t.Fatalf("expected --network=bridge: %s", s)
	}
}

func TestBuildContainerSessionExecArgs_Basic(t *testing.T) {
	args := BuildContainerSessionExecArgs("abc123", []string{"go", "build", "./..."}, []string{"GOPATH=/go", "HOME=/root"})
	s := strings.Join(args, " ")
	if !strings.Contains(s, "exec -w /workspace") {
		t.Fatalf("expected exec -w /workspace: %s", s)
	}
	if !strings.Contains(s, "-e GOPATH=/go") {
		t.Fatalf("expected env: %s", s)
	}
	if !strings.Contains(s, "abc123 go build ./...") {
		t.Fatalf("expected containerID + argv: %s", s)
	}
}

func TestSessionRunner_NilSession(t *testing.T) {
	r := &SessionRunner{}
	if r.Available() {
		t.Fatal("expected not available when session is nil")
	}
	if r.Name() != "container-session" {
		t.Fatalf("name: got %q", r.Name())
	}
}
