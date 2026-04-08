// Package astgrep locates or downloads the ast-grep binary for structural code search.
package astgrep

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	releasesAPI = "https://api.github.com/repos/ast-grep/ast-grep/releases/latest"
	binaryName  = "ast-grep"
)

// Resolve locates the ast-grep binary. It checks PATH first (both "ast-grep"
// and "sg"), then ~/.codient/bin/. Returns "" if not found (not an error).
func Resolve() string {
	if p, err := exec.LookPath("ast-grep"); err == nil {
		return p
	}
	if p, err := exec.LookPath("sg"); err == nil {
		return p
	}
	if d, err := BinDir(); err == nil {
		p := filepath.Join(d, binaryExeName())
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// BinDir returns ~/.codient/bin/, creating it if needed.
func BinDir() (string, error) {
	base, err := stateDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(base, "bin")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

// Download fetches the latest ast-grep release binary into destDir.
// Returns the full path to the extracted binary.
func Download(ctx context.Context, destDir string) (string, error) {
	asset, err := assetName()
	if err != nil {
		return "", err
	}

	tag, err := latestTag(ctx)
	if err != nil {
		return "", fmt.Errorf("fetch latest ast-grep release: %w", err)
	}

	url := fmt.Sprintf("https://github.com/ast-grep/ast-grep/releases/download/%s/%s", tag, asset)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download ast-grep: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download ast-grep: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "ast-grep-*.zip")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return "", fmt.Errorf("download ast-grep: %w", err)
	}
	tmp.Close()

	binName := binaryExeName()
	destPath := filepath.Join(destDir, binName)
	if err := extractBinaryFromZip(tmpPath, binName, destPath); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(destPath, 0o755); err != nil {
			return "", err
		}
	}
	return destPath, nil
}

// AssetName returns the release asset filename for the current platform.
// Exported for testing.
func AssetName() (string, error) {
	return assetName()
}

func assetName() (string, error) {
	arch, ok := archMap[runtime.GOARCH]
	if !ok {
		return "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}
	platform, ok := platformMap[runtime.GOOS]
	if !ok {
		return "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	return fmt.Sprintf("app-%s-%s.zip", arch, platform), nil
}

var archMap = map[string]string{
	"amd64": "x86_64",
	"arm64": "aarch64",
	"386":   "i686",
}

var platformMap = map[string]string{
	"linux":   "unknown-linux-gnu",
	"darwin":  "apple-darwin",
	"windows": "pc-windows-msvc",
}

func binaryExeName() string {
	if runtime.GOOS == "windows" {
		return binaryName + ".exe"
	}
	return binaryName
}

func latestTag(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API: HTTP %d", resp.StatusCode)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name in release response")
	}
	return release.TagName, nil
}

func extractBinaryFromZip(zipPath, binName, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		base := filepath.Base(f.Name)
		if !strings.EqualFold(base, binName) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		out, err := os.Create(destPath)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	}
	return fmt.Errorf("%s not found in zip archive", binName)
}

func stateDir() (string, error) {
	if d := strings.TrimSpace(os.Getenv("CODIENT_STATE_DIR")); d != "" {
		return filepath.Abs(d)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codient"), nil
}
