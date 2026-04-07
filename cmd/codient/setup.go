package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"codient/internal/config"
	"codient/internal/openaiclient"
)

// runSetupWizard walks the user through configuring their API connection
// and selecting a model. It returns true if the setup completed successfully.
func (s *session) runSetupWizard(ctx context.Context, sc *bufio.Scanner) bool {
	fmt.Fprintf(os.Stderr, "\n  Welcome! Let's connect to your OpenAI-compatible API.\n\n")

	baseURL := promptWithDefault(sc, "  Base URL", s.cfg.BaseURL)
	apiKey := promptWithDefault(sc, "  API key", s.cfg.APIKey)

	s.cfg.BaseURL = strings.TrimRight(baseURL, "/")
	s.cfg.APIKey = apiKey
	s.client = openaiclient.New(s.cfg)

	fmt.Fprintf(os.Stderr, "\n  Connecting to %s ...\n", s.cfg.BaseURL)
	models, err := s.client.ListModels(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Could not fetch models: %v\n", err)
		fmt.Fprintf(os.Stderr, "  You can set the model manually later with /config model <name>\n\n")
		saveCurrentConfig(s.cfg)
		return false
	}

	if len(models) == 0 {
		fmt.Fprintf(os.Stderr, "  Server returned no models.\n")
		fmt.Fprintf(os.Stderr, "  You can set the model manually later with /config model <name>\n\n")
		saveCurrentConfig(s.cfg)
		return false
	}

	fmt.Fprintf(os.Stderr, "\n  Available models:\n\n")
	for i, m := range models {
		fmt.Fprintf(os.Stderr, "    %d) %s\n", i+1, m)
	}
	fmt.Fprintf(os.Stderr, "\n")

	for {
		fmt.Fprintf(os.Stderr, "  Select a model [1-%d]: ", len(models))
		if !sc.Scan() {
			return false
		}
		input := strings.TrimSpace(sc.Text())
		if input == "" {
			continue
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(models) {
			// Allow typing the model name directly.
			for _, m := range models {
				if strings.EqualFold(m, input) {
					s.cfg.Model = m
					saveCurrentConfig(s.cfg)
					fmt.Fprintf(os.Stderr, "\n  Configuration saved. Model set to %s.\n\n", s.cfg.Model)
					return true
				}
			}
			fmt.Fprintf(os.Stderr, "  Please enter a number between 1 and %d.\n", len(models))
			continue
		}
		s.cfg.Model = models[n-1]
		break
	}

	s.setupWebSearch(sc)

	saveCurrentConfig(s.cfg)
	fmt.Fprintf(os.Stderr, "\n  Configuration saved. Model set to %s.\n\n", s.cfg.Model)
	return true
}

func (s *session) setupWebSearch(sc *bufio.Scanner) {
	fmt.Fprintf(os.Stderr, "\n  Web search (optional) lets the agent look up documentation and API references.\n")
	fmt.Fprintf(os.Stderr, "  It requires a SearXNG instance running in Docker.\n")
	fmt.Fprintf(os.Stderr, "  See: https://docs.searxng.org/admin/installation-docker.html\n\n")

	current := ""
	if s.cfg.SearchBaseURL != "" {
		current = s.cfg.SearchBaseURL
	}

	label := "  SearXNG base URL (leave empty to skip)"
	if current != "" {
		label = "  SearXNG base URL (empty to disable)"
	}
	u := promptWithDefault(sc, label, current)
	u = strings.TrimSpace(u)
	if u == "" {
		if current != "" {
			s.cfg.SearchBaseURL = ""
			fmt.Fprintf(os.Stderr, "  Web search disabled.\n")
		} else {
			fmt.Fprintf(os.Stderr, "  Skipped.\n")
		}
		return
	}
	s.cfg.SearchBaseURL = strings.TrimRight(u, "/")
	fmt.Fprintf(os.Stderr, "  Web search enabled (%s).\n", s.cfg.SearchBaseURL)
}

func promptWithDefault(sc *bufio.Scanner, label, def string) string {
	if def != "" {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	}
	if !sc.Scan() {
		return def
	}
	v := strings.TrimSpace(sc.Text())
	if v == "" {
		return def
	}
	return v
}

func saveCurrentConfig(cfg *config.Config) {
	pc := &config.PersistentConfig{
		BaseURL:       cfg.BaseURL,
		APIKey:        cfg.APIKey,
		Model:         cfg.Model,
		SearchBaseURL: cfg.SearchBaseURL,
	}
	if err := config.SavePersistentConfig(pc); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not save config: %v\n", err)
	}
}
