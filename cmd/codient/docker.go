package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	searxngContainerName = "codient-searxng"
	defaultSearxngPort   = 8888
)

var searxngComposeYML = `services:
  searxng:
    image: searxng/searxng:latest
    container_name: codient-searxng
    ports:
      - "${SEARXNG_PORT:-8888}:8080"
    volumes:
      - ./settings.yml:/etc/searxng/settings.yml:ro
    restart: unless-stopped
`

var searxngSettingsYML = `use_default_settings: true
server:
  secret_key: "codient-searxng-local"
  limiter: false
  image_proxy: false
search:
  formats:
    - html
    - json
`

// searxngAssetsDir returns ~/.codient/docker/searxng/, creating it if needed,
// and ensures the compose and settings files are written.
func searxngAssetsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	if d := strings.TrimSpace(os.Getenv("CODIENT_STATE_DIR")); d != "" {
		home, err = filepath.Abs(d)
		if err != nil {
			return "", err
		}
	} else {
		home = filepath.Join(home, ".codient")
	}
	dir := filepath.Join(home, "docker", "searxng")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create %s: %w", dir, err)
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	settingsPath := filepath.Join(dir, "settings.yml")
	if err := os.WriteFile(composePath, []byte(searxngComposeYML), 0o644); err != nil {
		return "", fmt.Errorf("write docker-compose.yml: %w", err)
	}
	if err := os.WriteFile(settingsPath, []byte(searxngSettingsYML), 0o644); err != nil {
		return "", fmt.Errorf("write settings.yml: %w", err)
	}
	return dir, nil
}

func dockerAvailable() bool {
	cmd := exec.Command("docker", "version")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func searxngContainerRunning() (bool, int) {
	out, err := exec.Command("docker", "ps",
		"--filter", "name="+searxngContainerName,
		"--format", "{{.Ports}}",
	).Output()
	if err != nil {
		return false, 0
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return false, 0
	}
	port := parseMappedPort(line)
	return true, port
}

// parseMappedPort extracts the host port from docker ps Ports output
// like "0.0.0.0:8888->8080/tcp".
func parseMappedPort(ports string) int {
	for _, seg := range strings.Split(ports, ",") {
		seg = strings.TrimSpace(seg)
		arrow := strings.Index(seg, "->")
		if arrow < 0 {
			continue
		}
		hostPart := seg[:arrow]
		if colon := strings.LastIndex(hostPart, ":"); colon >= 0 {
			hostPart = hostPart[colon+1:]
		}
		if p, err := strconv.Atoi(hostPart); err == nil {
			return p
		}
	}
	return 0
}

func searxngContainerExists() bool {
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "name="+searxngContainerName,
		"--format", "{{.Names}}",
	).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

func startSearxng(port int) error {
	dir, err := searxngAssetsDir()
	if err != nil {
		return err
	}

	if searxngContainerExists() {
		stop := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "down")
		stop.Stdout = os.Stderr
		stop.Stderr = os.Stderr
		_ = stop.Run()
	}

	cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "up", "-d")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fmt.Sprintf("SEARXNG_PORT=%d", port))
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stopSearxng() error {
	dir, err := searxngAssetsDir()
	if err != nil {
		return err
	}
	cmd := exec.Command("docker", "compose", "-f", filepath.Join(dir, "docker-compose.yml"), "down")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func waitForSearxng(baseURL string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("SearXNG did not respond at %s within %s", baseURL, timeout)
}

// searxngDiscoveryURLs returns ordered unique base URLs to probe for an existing SearXNG instance.
func searxngDiscoveryURLs(configured string) []string {
	var out []string
	add := func(u string) {
		u = strings.TrimRight(strings.TrimSpace(u), "/")
		if u == "" {
			return
		}
		for _, existing := range out {
			if existing == u {
				return
			}
		}
		out = append(out, u)
	}

	add(configured)

	if dockerAvailable() {
		if running, port := searxngContainerRunning(); running {
			if port < 1 {
				port = defaultSearxngPort
			}
			add(fmt.Sprintf("http://127.0.0.1:%d", port))
		}
	}

	add("http://127.0.0.1:8888")
	add("http://127.0.0.1:8080")

	return out
}
