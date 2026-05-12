// Package errorsink provides an append-only process error log (failures and panics).
package errorsink

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EnvDisable is the environment variable name; set to "0", "false", or "off" to disable the default error log.
const EnvDisable = "CODIENT_ERROR_LOG"

// Disabled reports whether the default error log should be skipped.
func Disabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(EnvDisable)))
	switch v {
	case "0", "false", "off", "no":
		return true
	default:
		return false
	}
}

// Sink is a thread-safe append-only log file. A nil *Sink is a no-op on all methods.
type Sink struct {
	f         *os.File
	path      string
	sessionID string
	mu        sync.Mutex
}

// Open creates or appends to $stateDir/logs/errors-<UTC>-<pid>.log.
func Open(stateDir string) (*Sink, string, error) {
	if stateDir == "" {
		return nil, "", fmt.Errorf("errorsink: empty state dir")
	}
	logDir := filepath.Join(stateDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, "", err
	}
	name := fmt.Sprintf("errors-%s-%d.log", time.Now().UTC().Format("20060102-150405"), os.Getpid())
	path := filepath.Join(logDir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, "", err
	}
	return &Sink{f: f, path: path}, path, nil
}

// Path returns the log file path, or empty if s is nil or not backed by a file.
func (s *Sink) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// SetSessionID records the session id for line prefixes (call after the id is known).
func (s *Sink) SetSessionID(id string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.sessionID = strings.TrimSpace(id)
	s.mu.Unlock()
}

func (s *Sink) prefixLocked() string {
	if s.sessionID != "" {
		return fmt.Sprintf("[session_id=%s] ", s.sessionID)
	}
	return ""
}

// Logf writes a timestamped line. Safe if s is nil.
func (s *Sink) Logf(format string, args ...any) {
	if s == nil || s.f == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	line := fmt.Sprintf("%s %s%s\n", time.Now().UTC().Format(time.RFC3339Nano), s.prefixLocked(), fmt.Sprintf(format, args...))
	_, _ = s.f.WriteString(line)
}

// LogError writes a timestamped error line with optional context. Safe if s is nil.
func (s *Sink) LogError(context string, err error) {
	if s == nil || s.f == nil || err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx := strings.TrimSpace(context)
	var line string
	if ctx != "" {
		line = fmt.Sprintf("%s %serror context=%q: %v\n", time.Now().UTC().Format(time.RFC3339Nano), s.prefixLocked(), ctx, err)
	} else {
		line = fmt.Sprintf("%s %serror: %v\n", time.Now().UTC().Format(time.RFC3339Nano), s.prefixLocked(), err)
	}
	_, _ = s.f.WriteString(line)
	_ = s.f.Sync()
}

// LogPanic writes the panic value and stack, then syncs. Safe if s is nil.
func (s *Sink) LogPanic(recovered any, stack []byte) {
	if s == nil || s.f == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	pfx := s.prefixLocked()
	_, _ = fmt.Fprintf(s.f, "%s %spanic: %v\n", ts, pfx, recovered)
	if len(stack) > 0 {
		_, _ = s.f.Write(stack)
		if stack[len(stack)-1] != '\n' {
			_, _ = s.f.WriteString("\n")
		}
	}
	_ = s.f.Sync()
}

// Close closes the backing file. Safe if s is nil.
func (s *Sink) Close() error {
	if s == nil || s.f == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.f.Close()
	s.f = nil
	return err
}
