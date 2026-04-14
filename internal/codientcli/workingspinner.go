package codientcli

import (
	"io"
	"os"
	"sync"
	"time"
)

// stderrIsInteractive reports whether os.Stderr is a character device (TTY).
func stderrIsInteractive() bool {
	st, err := os.Stderr.Stat()
	if err != nil {
		return false
	}
	return (st.Mode() & os.ModeCharDevice) != 0
}

// startWorkingSpinner draws an indeterminate spinner on w until stop is called.
// It is a no-op when w is not a TTY (avoids escape sequences in logs/pipes).
func startWorkingSpinner(w io.Writer) (stop func()) {
	if w == nil || !stderrIsInteractive() {
		return func() {}
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	var once sync.Once
	stop = func() {
		once.Do(func() {
			close(done)
			wg.Wait()
			_, _ = io.WriteString(w, "\r\x1b[K") // clear line after goroutine exits
		})
	}

	go func() {
		defer wg.Done()
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		t := time.NewTicker(90 * time.Millisecond)
		defer t.Stop()
		i := 0
		for {
			select {
			case <-done:
				return
			case <-t.C:
				_, _ = io.WriteString(w, "\r\x1b[K"+frames[i%len(frames)]+" Agent is working…")
				i++
			}
		}
	}()

	return stop
}

// firstWriteStop forwards writes to w and invokes stop once on the first non-empty write.
type firstWriteStop struct {
	w    io.Writer
	stop func()
	once sync.Once
}

func (f *firstWriteStop) Write(p []byte) (int, error) {
	if len(p) > 0 {
		f.once.Do(f.stop)
	}
	if f.w == nil {
		return len(p), nil
	}
	return f.w.Write(p)
}
