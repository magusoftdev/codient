package codientcli

import (
	"bytes"
	"io"
)

// prefixWriter prepends a prefix to the start of each line written.
// Used to indent sub-agent progress output beneath the parent.
// Create with newPrefixWriter to ensure correct initialization.
type prefixWriter struct {
	prefix      []byte
	w           io.Writer
	needsPrefix bool // true when the next byte written starts a new line
}

func newPrefixWriter(prefix []byte, w io.Writer) *prefixWriter {
	return &prefixWriter{prefix: prefix, w: w, needsPrefix: true}
}

func (pw *prefixWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		if pw.needsPrefix {
			if _, err := pw.w.Write(pw.prefix); err != nil {
				return total - len(p), err
			}
			pw.needsPrefix = false
		}
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			_, err := pw.w.Write(p)
			return total, err
		}
		line := p[:idx+1]
		if _, err := pw.w.Write(line); err != nil {
			return total - len(p), err
		}
		p = p[idx+1:]
		pw.needsPrefix = true
	}
	return total, nil
}
