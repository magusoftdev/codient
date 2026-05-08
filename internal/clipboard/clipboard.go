// Package clipboard detects and extracts images from the OS clipboard using
// platform-specific tools (wl-paste/xclip on Linux, osascript on macOS,
// PowerShell on Windows).
package clipboard

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Executor abstracts subprocess execution so callers can inject fakes in tests.
type Executor interface {
	// LookPath checks whether name is available on PATH.
	LookPath(name string) (string, error)
	// Run executes the command, returning combined stdout.
	Run(ctx context.Context, name string, args ...string) (string, error)
	// RunPipe executes the command and streams stdout into w.
	RunPipe(ctx context.Context, w io.Writer, name string, args ...string) error
}

type realExec struct{}

func (realExec) LookPath(name string) (string, error) { return exec.LookPath(name) }

func (realExec) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.Output()
	return string(out), err
}

func (realExec) RunPipe(ctx context.Context, w io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = w
	return cmd.Run()
}

// DefaultExecutor returns the real subprocess executor.
func DefaultExecutor() Executor { return realExec{} }

// backend holds the resolved platform clipboard tool.
type backend struct {
	platform string // "wayland", "x11", "darwin", "windows"
	tool     string // binary name or empty if unsupported
}

// resolve detects the platform and which clipboard tool is available.
func resolve(e Executor) backend {
	switch runtime.GOOS {
	case "darwin":
		if p, err := e.LookPath("osascript"); err == nil && p != "" {
			return backend{platform: "darwin", tool: "osascript"}
		}
		return backend{platform: "darwin"}
	case "windows":
		if p, err := e.LookPath("powershell"); err == nil && p != "" {
			return backend{platform: "windows", tool: "powershell"}
		}
		return backend{platform: "windows"}
	case "linux":
		ds := os.Getenv("XDG_SESSION_TYPE")
		switch ds {
		case "wayland":
			if p, err := e.LookPath("wl-paste"); err == nil && p != "" {
				return backend{platform: "wayland", tool: "wl-paste"}
			}
			return backend{platform: "wayland"}
		case "x11":
			if p, err := e.LookPath("xclip"); err == nil && p != "" {
				return backend{platform: "x11", tool: "xclip"}
			}
			return backend{platform: "x11"}
		default:
			// Unknown display server; try both tools.
			if p, err := e.LookPath("wl-paste"); err == nil && p != "" {
				return backend{platform: "wayland", tool: "wl-paste"}
			}
			if p, err := e.LookPath("xclip"); err == nil && p != "" {
				return backend{platform: "x11", tool: "xclip"}
			}
			return backend{platform: "linux"}
		}
	default:
		return backend{platform: runtime.GOOS}
	}
}

// UnsupportedError is returned when clipboard image access is not available
// on the current platform or when the required tool is missing.
type UnsupportedError struct {
	Platform string
	Detail   string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("clipboard image paste not available: %s", e.Detail)
}

// HasImage reports whether the system clipboard currently contains an image.
func HasImage(ctx context.Context, e Executor) (bool, error) {
	b := resolve(e)
	return hasImage(ctx, e, b)
}

func hasImage(ctx context.Context, e Executor, b backend) (bool, error) {
	switch b.platform {
	case "wayland":
		if b.tool == "" {
			return false, &UnsupportedError{Platform: "linux/wayland", Detail: "wl-paste not found; install wl-clipboard"}
		}
		out, err := e.Run(ctx, "wl-paste", "--list-types")
		if err != nil {
			return false, nil
		}
		return strings.Contains(out, "image/"), nil

	case "x11":
		if b.tool == "" {
			return false, &UnsupportedError{Platform: "linux/x11", Detail: "xclip not found; install xclip"}
		}
		out, err := e.Run(ctx, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		if err != nil {
			return false, nil
		}
		return strings.Contains(out, "image/"), nil

	case "darwin":
		if b.tool == "" {
			return false, &UnsupportedError{Platform: "darwin", Detail: "osascript not found"}
		}
		out, err := e.Run(ctx, "osascript", "-e", "clipboard info")
		if err != nil {
			return false, nil
		}
		return darwinImageRe.MatchString(out), nil

	case "windows":
		if b.tool == "" {
			return false, &UnsupportedError{Platform: "windows", Detail: "powershell not found"}
		}
		out, err := e.Run(ctx, "powershell", "-NoProfile", "-Command",
			"Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.Clipboard]::ContainsImage()")
		if err != nil {
			return false, nil
		}
		return strings.TrimSpace(out) == "True", nil

	default:
		return false, &UnsupportedError{Platform: b.platform, Detail: "unsupported platform for clipboard image paste"}
	}
}

var darwinImageRe = regexp.MustCompile(`«class PNGf»|TIFF picture|JPEG picture|GIF picture|«class JPEG»|«class TIFF»`)

// SaveImage extracts clipboard image data to a PNG file in dir.
// Returns the path to the saved file. The caller should load it with
// imageutil.LoadImage and clean up old files with Cleanup.
func SaveImage(ctx context.Context, e Executor, dir string) (string, error) {
	b := resolve(e)
	has, err := hasImage(ctx, e, b)
	if err != nil {
		return "", err
	}
	if !has {
		return "", fmt.Errorf("no image found in clipboard")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create clipboard dir: %w", err)
	}

	ts := time.Now().UnixMilli()
	dest := filepath.Join(dir, fmt.Sprintf("clipboard-%d.png", ts))

	switch b.platform {
	case "wayland":
		return saveViaPipe(ctx, e, dest, "wl-paste", "--no-newline", "--type", "image/png")
	case "x11":
		return saveViaPipe(ctx, e, dest, "xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	case "darwin":
		return saveDarwin(ctx, e, dest)
	case "windows":
		return saveWindows(ctx, e, dest)
	default:
		return "", &UnsupportedError{Platform: b.platform, Detail: "unsupported platform"}
	}
}

func saveViaPipe(ctx context.Context, e Executor, dest, tool string, args ...string) (string, error) {
	f, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer f.Close()

	if err := e.RunPipe(ctx, f, tool, args...); err != nil {
		os.Remove(dest)
		return "", fmt.Errorf("%s: %w", tool, err)
	}

	info, err := os.Stat(dest)
	if err != nil || info.Size() == 0 {
		os.Remove(dest)
		return "", fmt.Errorf("clipboard image saved but file is empty")
	}
	return dest, nil
}

func saveDarwin(ctx context.Context, e Executor, dest string) (string, error) {
	type format struct {
		class string
		ext   string
	}
	formats := []format{
		{"PNGf", "png"},
		{"JPEG", "jpg"},
	}
	for _, f := range formats {
		target := dest
		if f.ext != "png" {
			target = strings.TrimSuffix(dest, ".png") + "." + f.ext
		}
		script := fmt.Sprintf(`try
	set imageData to the clipboard as «class %s»
	set fileRef to open for access POSIX file "%s" with write permission
	write imageData to fileRef
	close access fileRef
	return "success"
on error errMsg
	try
		close access POSIX file "%s"
	end try
	return "error"
end try`, f.class, target, target)

		out, err := e.Run(ctx, "osascript", "-e", script)
		if err != nil || strings.TrimSpace(out) != "success" {
			os.Remove(target)
			continue
		}
		info, err := os.Stat(target)
		if err != nil || info.Size() == 0 {
			os.Remove(target)
			continue
		}
		return target, nil
	}
	return "", fmt.Errorf("failed to save clipboard image (tried PNGf, JPEG)")
}

func saveWindows(ctx context.Context, e Executor, dest string) (string, error) {
	psPath := strings.ReplaceAll(dest, "'", "''")
	script := fmt.Sprintf(`Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
if ([System.Windows.Forms.Clipboard]::ContainsImage()) {
    $image = [System.Windows.Forms.Clipboard]::GetImage()
    $image.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png)
    Write-Output "success"
}`, psPath)

	out, err := e.Run(ctx, "powershell", "-NoProfile", "-Command", script)
	if err != nil {
		return "", fmt.Errorf("powershell: %w", err)
	}
	if strings.TrimSpace(out) != "success" {
		return "", fmt.Errorf("powershell did not report success")
	}
	info, statErr := os.Stat(dest)
	if statErr != nil || info.Size() == 0 {
		os.Remove(dest)
		return "", fmt.Errorf("clipboard image saved but file is empty")
	}
	return dest, nil
}

// Cleanup removes clipboard temp files in dir that are older than maxAge.
func Cleanup(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "clipboard-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ClipboardDir returns the standard clipboard temp directory under the
// workspace .codient folder.
func ClipboardDir(workspace string) string {
	return filepath.Join(workspace, ".codient", "clipboard")
}
