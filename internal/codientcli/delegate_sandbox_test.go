package codientcli

import (
	"testing"

	"codient/internal/config"
)

func TestResolveDelegateSandboxProfile_ByName(t *testing.T) {
	cfg := &config.Config{
		DelegateSandboxProfiles: map[string]config.DelegateSandboxProfile{
			"go-build": {Image: "golang:1.22", LongLived: true},
			"node":     {Image: "node:20"},
		},
	}
	p, ok := resolveDelegateSandboxProfile(cfg, "go-build")
	if !ok {
		t.Fatal("expected profile match")
	}
	if p.Image != "golang:1.22" || !p.LongLived {
		t.Fatalf("profile: %+v", p)
	}
}

func TestResolveDelegateSandboxProfile_FallsBackToDefault(t *testing.T) {
	cfg := &config.Config{
		DelegateSandboxProfiles: map[string]config.DelegateSandboxProfile{
			"go-build": {Image: "golang:1.22"},
		},
		DelegateSandboxDefault: "go-build",
	}
	p, ok := resolveDelegateSandboxProfile(cfg, "")
	if !ok {
		t.Fatal("expected default profile")
	}
	if p.Image != "golang:1.22" {
		t.Fatalf("profile: %+v", p)
	}
}

func TestResolveDelegateSandboxProfile_NoProfiles(t *testing.T) {
	cfg := &config.Config{}
	_, ok := resolveDelegateSandboxProfile(cfg, "anything")
	if ok {
		t.Fatal("expected no profile when none configured")
	}
}

func TestResolveDelegateSandboxProfile_EmptyNameNoDefault(t *testing.T) {
	cfg := &config.Config{
		DelegateSandboxProfiles: map[string]config.DelegateSandboxProfile{
			"go-build": {Image: "golang:1.22"},
		},
	}
	_, ok := resolveDelegateSandboxProfile(cfg, "")
	if ok {
		t.Fatal("expected no profile when name empty and no default")
	}
}
