// Package clipboard detects and extracts images from the OS clipboard using
// platform-specific tools (wl-paste/xclip on Linux, osascript on macOS,
// PowerShell on Windows).
//
// The clipboard can carry an image in two shapes:
//
//  1. Raw image bytes, advertised by the OS as an "image/*" MIME type
//     (`image/png`, `image/jpeg`, etc.). Browsers and screenshot tools
//     typically use this form.
//  2. A reference to an existing image file on disk — `text/uri-list` on
//     Linux, `FileDropList` on Windows, or `«class furl»` on macOS. File
//     managers (Nautilus, Dolphin, Finder, Explorer) use this form when the
//     user copies an image file.
//
// Both forms are handled: when raw image data is not present the package
// falls back to scanning the file-reference form for an entry whose
// extension is a supported image type.
package clipboard

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"codient/internal/imageutil"
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

// HasImage reports whether the system clipboard currently contains an image,
// either as raw image data or as a reference to an image file on disk.
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
		if strings.Contains(out, "image/") {
			return true, nil
		}
		if hasURIListImage(ctx, e, b, out) {
			return true, nil
		}
		return false, nil

	case "x11":
		if b.tool == "" {
			return false, &UnsupportedError{Platform: "linux/x11", Detail: "xclip not found; install xclip"}
		}
		out, err := e.Run(ctx, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		if err != nil {
			return false, nil
		}
		if strings.Contains(out, "image/") {
			return true, nil
		}
		if hasURIListImage(ctx, e, b, out) {
			return true, nil
		}
		return false, nil

	case "darwin":
		if b.tool == "" {
			return false, &UnsupportedError{Platform: "darwin", Detail: "osascript not found"}
		}
		out, err := e.Run(ctx, "osascript", "-e", "clipboard info")
		if err != nil {
			return false, nil
		}
		if darwinImageRe.MatchString(out) {
			return true, nil
		}
		if darwinFileRefRe.MatchString(out) {
			if _, ok := darwinClipboardImagePath(ctx, e); ok {
				return true, nil
			}
		}
		return false, nil

	case "windows":
		if b.tool == "" {
			return false, &UnsupportedError{Platform: "windows", Detail: "powershell not found"}
		}
		out, err := e.Run(ctx, "powershell", "-NoProfile", "-Command",
			"Add-Type -AssemblyName System.Windows.Forms; "+
				"$c=[System.Windows.Forms.Clipboard]::ContainsImage(); "+
				"$f=[System.Windows.Forms.Clipboard]::ContainsFileDropList(); "+
				"\"image=$c;files=$f\"")
		if err != nil {
			return false, nil
		}
		line := strings.TrimSpace(out)
		if strings.Contains(line, "image=True") {
			return true, nil
		}
		if strings.Contains(line, "files=True") {
			if _, ok := windowsClipboardImagePath(ctx, e); ok {
				return true, nil
			}
		}
		return false, nil

	default:
		return false, &UnsupportedError{Platform: b.platform, Detail: "unsupported platform for clipboard image paste"}
	}
}

var darwinImageRe = regexp.MustCompile(`«class PNGf»|TIFF picture|JPEG picture|GIF picture|«class JPEG»|«class TIFF»`)
var darwinFileRefRe = regexp.MustCompile(`«class furl»|file URL`)

// hasURIListImage probes the clipboard's text/uri-list (Linux) and reports
// whether any URI in the list resolves to a supported image file.
func hasURIListImage(ctx context.Context, e Executor, b backend, listTypes string) bool {
	if !strings.Contains(listTypes, "text/uri-list") {
		return false
	}
	paths, err := readURIListPaths(ctx, e, b)
	if err != nil {
		return false
	}
	for _, p := range paths {
		if imageutil.SupportedImageExt(p) {
			if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
				return true
			}
		}
	}
	return false
}

// readURIListPaths fetches the clipboard's text/uri-list and returns the
// local filesystem paths of any `file://` URIs.
func readURIListPaths(ctx context.Context, e Executor, b backend) ([]string, error) {
	var raw string
	var err error
	switch b.platform {
	case "wayland":
		raw, err = e.Run(ctx, "wl-paste", "--no-newline", "--type", "text/uri-list")
	case "x11":
		raw, err = e.Run(ctx, "xclip", "-selection", "clipboard", "-t", "text/uri-list", "-o")
	default:
		return nil, fmt.Errorf("uri-list not supported on %s", b.platform)
	}
	if err != nil {
		return nil, err
	}
	return parseURIList(raw), nil
}

// parseURIList interprets a text/uri-list payload (RFC 2483) and returns the
// local filesystem paths corresponding to its `file://` URIs. Comment lines
// (starting with `#`) and non-file URIs are ignored.
func parseURIList(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if p, ok := fileURIToPath(line); ok {
			out = append(out, p)
		}
	}
	return out
}

// fileURIToPath converts a `file://` URI into a local filesystem path,
// decoding percent-encoded characters. Returns false for any other scheme.
func fileURIToPath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u == nil {
		return "", false
	}
	if !strings.EqualFold(u.Scheme, "file") {
		return "", false
	}
	p := u.Path
	if p == "" {
		// `file:relative` (rare) — fall back to opaque component.
		p = u.Opaque
	}
	if p == "" {
		return "", false
	}
	// On Windows, `file:///C:/path` → `/C:/path`; trim the leading slash.
	if runtime.GOOS == "windows" && len(p) >= 3 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return filepath.Clean(p), true
}

// SaveImage extracts clipboard image data to a file in dir.
// Returns the path to the saved file. The caller should load it with
// imageutil.LoadImage and clean up old files with Cleanup.
//
// When the clipboard holds raw image bytes, a `clipboard-<ts>.png` (or
// matching extension on Darwin) is written into dir. When the clipboard
// instead references an existing image file on disk (text/uri-list,
// FileDropList, `«class furl»`), the original file's path is returned
// unchanged so no copy is needed.
func SaveImage(ctx context.Context, e Executor, dir string) (string, error) {
	b := resolve(e)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create clipboard dir: %w", err)
	}

	ts := time.Now().UnixMilli()
	dest := filepath.Join(dir, fmt.Sprintf("clipboard-%d.png", ts))

	switch b.platform {
	case "wayland":
		if b.tool == "" {
			return "", &UnsupportedError{Platform: "linux/wayland", Detail: "wl-paste not found; install wl-clipboard"}
		}
		out, err := e.Run(ctx, "wl-paste", "--list-types")
		if err == nil && strings.Contains(out, "image/") {
			return saveViaPipe(ctx, e, dest, "wl-paste", "--no-newline", "--type", "image/png")
		}
		if err == nil {
			if p, ok := pickURIListImage(ctx, e, b); ok {
				return p, nil
			}
		}
		return "", fmt.Errorf("no image found in clipboard")

	case "x11":
		if b.tool == "" {
			return "", &UnsupportedError{Platform: "linux/x11", Detail: "xclip not found; install xclip"}
		}
		out, err := e.Run(ctx, "xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		if err == nil && strings.Contains(out, "image/") {
			return saveViaPipe(ctx, e, dest, "xclip", "-selection", "clipboard", "-t", "image/png", "-o")
		}
		if err == nil {
			if p, ok := pickURIListImage(ctx, e, b); ok {
				return p, nil
			}
		}
		return "", fmt.Errorf("no image found in clipboard")

	case "darwin":
		if b.tool == "" {
			return "", &UnsupportedError{Platform: "darwin", Detail: "osascript not found"}
		}
		out, err := e.Run(ctx, "osascript", "-e", "clipboard info")
		if err == nil && darwinImageRe.MatchString(out) {
			return saveDarwin(ctx, e, dest)
		}
		if err == nil && darwinFileRefRe.MatchString(out) {
			if p, ok := darwinClipboardImagePath(ctx, e); ok {
				return p, nil
			}
		}
		return "", fmt.Errorf("no image found in clipboard")

	case "windows":
		if b.tool == "" {
			return "", &UnsupportedError{Platform: "windows", Detail: "powershell not found"}
		}
		has, err := hasImage(ctx, e, b)
		if err != nil || !has {
			return "", fmt.Errorf("no image found in clipboard")
		}
		// Re-check what kind of clipboard data we have.
		info, _ := e.Run(ctx, "powershell", "-NoProfile", "-Command",
			"Add-Type -AssemblyName System.Windows.Forms; "+
				"[System.Windows.Forms.Clipboard]::ContainsImage()")
		if strings.TrimSpace(info) == "True" {
			return saveWindows(ctx, e, dest)
		}
		if p, ok := windowsClipboardImagePath(ctx, e); ok {
			return p, nil
		}
		return "", fmt.Errorf("no image found in clipboard")

	default:
		return "", &UnsupportedError{Platform: b.platform, Detail: "unsupported platform"}
	}
}

// pickURIListImage returns the first text/uri-list entry on the Linux
// clipboard that points to an existing image file.
func pickURIListImage(ctx context.Context, e Executor, b backend) (string, bool) {
	paths, err := readURIListPaths(ctx, e, b)
	if err != nil {
		return "", false
	}
	for _, p := range paths {
		if !imageutil.SupportedImageExt(p) {
			continue
		}
		info, statErr := os.Stat(p)
		if statErr != nil || info.IsDir() {
			continue
		}
		return p, true
	}
	return "", false
}

// darwinClipboardImagePath returns the first `«class furl»` entry on the
// macOS clipboard that points to an existing image file.
func darwinClipboardImagePath(ctx context.Context, e Executor) (string, bool) {
	// `the clipboard as «class furl»` returns a single furl; for multi-file
	// drags, `clipboard info` reports furl too but `as list` is needed. We
	// query both forms and pick the first image file.
	script := `try
	set the_list to (the clipboard as «class furl»)
	if class of the_list is list then
		set out to ""
		repeat with item_ in the_list
			set out to out & (POSIX path of item_) & linefeed
		end repeat
		return out
	else
		return POSIX path of the_list
	end if
on error
	return ""
end try`
	out, err := e.Run(ctx, "osascript", "-e", script)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimSpace(line)
		if p == "" {
			continue
		}
		if !imageutil.SupportedImageExt(p) {
			continue
		}
		if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
			return p, true
		}
	}
	return "", false
}

// windowsClipboardImagePath returns the first FileDropList entry on the
// Windows clipboard that points to an existing image file.
func windowsClipboardImagePath(ctx context.Context, e Executor) (string, bool) {
	script := `Add-Type -AssemblyName System.Windows.Forms
if ([System.Windows.Forms.Clipboard]::ContainsFileDropList()) {
    [System.Windows.Forms.Clipboard]::GetFileDropList() | ForEach-Object { $_ }
}`
	out, err := e.Run(ctx, "powershell", "-NoProfile", "-Command", script)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(out, "\n") {
		p := strings.TrimSpace(line)
		p = strings.TrimRight(p, "\r")
		if p == "" {
			continue
		}
		if !imageutil.SupportedImageExt(p) {
			continue
		}
		if info, statErr := os.Stat(p); statErr == nil && !info.IsDir() {
			return p, true
		}
	}
	return "", false
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
