package tools

import (
	"net"
	"strings"
	"sync"
)

// FetchHostChoice is the user's answer when fetch_url targets a host outside the allowlist.
type FetchHostChoice int

const (
	// FetchHostDeny refuses the fetch.
	FetchHostDeny FetchHostChoice = iota
	// FetchHostAllowOnce allows only this tool invocation (initial URL host only; not remembered).
	FetchHostAllowOnce
	// FetchHostAllowSession remembers the host for the rest of this process.
	FetchHostAllowSession
	// FetchHostAllowAlways persists the host to config (and session) when PersistFetchHost succeeds.
	FetchHostAllowAlways
)

// SessionFetchAllow holds hostnames the user approved for the current session (mutable).
type SessionFetchAllow struct {
	mu    sync.Mutex
	hosts map[string]struct{}
}

// NewSessionFetchAllow creates empty session-scoped fetch approval state.
func NewSessionFetchAllow() *SessionFetchAllow {
	return &SessionFetchAllow{hosts: make(map[string]struct{})}
}

// normalizeFetchHostKey returns a lowercase hostname without port for allow checks.
func normalizeFetchHostKey(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return host
}

// Add records that host is allowed for fetch_url for this session.
func (s *SessionFetchAllow) Add(host string) {
	if s == nil {
		return
	}
	h := normalizeFetchHostKey(host)
	if h == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hosts == nil {
		s.hosts = make(map[string]struct{})
	}
	s.hosts[h] = struct{}{}
}

// IsAllowed reports whether the session allowlist includes host.
func (s *SessionFetchAllow) IsAllowed(host string) bool {
	if s == nil {
		return false
	}
	h := normalizeFetchHostKey(host)
	if h == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.hosts[h]
	return ok
}
