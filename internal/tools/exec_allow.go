package tools

import (
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// NormalizeCmdKey maps argv[0] or a resolved path to an allowlist key (basename, lower, strip .exe on Windows).
func NormalizeCmdKey(argv0 string) string {
	base := filepath.Base(strings.TrimSpace(argv0))
	s := strings.ToLower(base)
	if runtime.GOOS == "windows" {
		s = strings.TrimSuffix(s, ".exe")
		s = strings.TrimSuffix(s, ".bat")
		s = strings.TrimSuffix(s, ".cmd")
	}
	return s
}

// SessionExecAllow holds mutable allowlist state for run_command for one agent session.
type SessionExecAllow struct {
	mu       sync.Mutex
	names    map[string]struct{}
	allowAll bool
}

// NewSessionExecAllow creates session allow state from initial names (e.g. from config).
func NewSessionExecAllow(initial []string) *SessionExecAllow {
	s := &SessionExecAllow{names: make(map[string]struct{})}
	for _, a := range initial {
		k := NormalizeCmdKey(a)
		if k != "" {
			s.names[k] = struct{}{}
		}
	}
	return s
}

// AllowAll reports whether all commands are permitted without further checks.
func (s *SessionExecAllow) AllowAll() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allowAll
}

// IsAllowed returns true if the normalized command key is allowed or allow-all is set.
// Key may be a raw name or path; it is normalized with NormalizeCmdKey.
func (s *SessionExecAllow) IsAllowed(key string) bool {
	if s == nil {
		return false
	}
	k := NormalizeCmdKey(key)
	if k == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.allowAll {
		return true
	}
	_, ok := s.names[k]
	return ok
}

// Add records an additional allowed command name for the session.
func (s *SessionExecAllow) Add(key string) {
	if s == nil {
		return
	}
	k := NormalizeCmdKey(key)
	if k == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.names[k] = struct{}{}
}

// SetAllowAll permits any command for the rest of the session.
func (s *SessionExecAllow) SetAllowAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.allowAll = true
}
