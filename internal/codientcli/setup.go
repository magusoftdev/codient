package codientcli

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
		if err := saveCurrentConfig(s.cfg); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not save config: %v\n", err)
		}
		return false
	}

	if len(models) == 0 {
		fmt.Fprintf(os.Stderr, "  Server returned no models.\n")
		fmt.Fprintf(os.Stderr, "  You can set the model manually later with /config model <name>\n\n")
		if err := saveCurrentConfig(s.cfg); err != nil {
			fmt.Fprintf(os.Stderr, "  Warning: could not save config: %v\n", err)
		}
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
			for _, m := range models {
				if strings.EqualFold(m, input) {
					s.cfg.Model = m
					if err := saveCurrentConfig(s.cfg); err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: could not save config: %v\n", err)
					}
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

	// Optional embedding model for semantic code search.
	fmt.Fprintf(os.Stderr, "\n  Embedding model for semantic code search (leave blank to skip).\n")
	fmt.Fprintf(os.Stderr, "  Examples: text-embedding-3-small (OpenAI), nomic-embed-text (local)\n")
	embModel := promptWithDefault(sc, "  Embedding model", s.cfg.EmbeddingModel)
	s.cfg.EmbeddingModel = strings.TrimSpace(embModel)

	// Optional dedicated embedding server (e.g. local LM Studio while chat targets Anthropic/Claude).
	if s.cfg.EmbeddingModel != "" {
		s.setupEmbeddingServerOverride(sc)
	}

	// Optional reasoning-tier overrides (orchestrator routes simpler tasks
	// to low-reasoning, complex planning to high-reasoning).
	s.setupReasoningTierOverrides(ctx, sc)

	if err := saveCurrentConfig(s.cfg); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not save config: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "\n  Configuration saved. Model set to %s.\n\n", s.cfg.Model)
	return true
}

// setupEmbeddingServerOverride optionally points /v1/embeddings at a different server than chat.
// Useful when the chat base URL (e.g. Anthropic/Claude) does not implement /v1/embeddings but
// the user has a local server (LM Studio, Ollama, etc.) that does.
func (s *session) setupEmbeddingServerOverride(sc *bufio.Scanner) {
	currentBase := s.cfg.EmbeddingBaseURL
	hasOverride := strings.TrimSpace(currentBase) != ""
	if hasOverride {
		fmt.Fprintf(os.Stderr, "\n  Use a different server for embeddings? (current: %s) (Y/n): ", currentBase)
	} else {
		fmt.Fprintf(os.Stderr, "\n  Use a different server for embeddings? (y/N): ")
	}
	if !sc.Scan() {
		return
	}
	ans := strings.TrimSpace(strings.ToLower(sc.Text()))
	if hasOverride {
		if ans == "n" || ans == "no" {
			s.cfg.EmbeddingBaseURL = ""
			s.cfg.EmbeddingAPIKey = ""
			fmt.Fprintf(os.Stderr, "  Embeddings will inherit the chat base_url / api_key.\n")
			return
		}
	} else if ans != "y" && ans != "yes" {
		return
	}

	defaultBase := currentBase
	if defaultBase == "" {
		defaultBase = s.cfg.BaseURL
	}
	defaultKey := s.cfg.EmbeddingAPIKey
	if defaultKey == "" {
		defaultKey = s.cfg.APIKey
	}
	embBase := promptWithDefault(sc, "  Embedding base URL", defaultBase)
	embKey := promptWithDefault(sc, "  Embedding API key", defaultKey)
	embBase = strings.TrimRight(strings.TrimSpace(embBase), "/")
	if embBase == s.cfg.BaseURL {
		s.cfg.EmbeddingBaseURL = ""
	} else {
		s.cfg.EmbeddingBaseURL = embBase
	}
	if strings.TrimSpace(embKey) == s.cfg.APIKey {
		s.cfg.EmbeddingAPIKey = ""
	} else {
		s.cfg.EmbeddingAPIKey = strings.TrimSpace(embKey)
	}
	if s.cfg.EmbeddingBaseURL == "" {
		fmt.Fprintf(os.Stderr, "  Embeddings will inherit the chat base_url.\n")
	} else {
		fmt.Fprintf(os.Stderr, "  Embeddings will target %s.\n", s.cfg.EmbeddingBaseURL)
	}
}

// setupReasoningTierOverrides optionally configures distinct models / servers
// for the high-reasoning tier (DESIGN advice, COMPLEX_TASK plans). The
// low-reasoning tier (supervisor + ask + simple build) defaults to the
// top-level model unless explicitly overridden via /config.
func (s *session) setupReasoningTierOverrides(ctx context.Context, sc *bufio.Scanner) {
	fmt.Fprintf(os.Stderr, "\n  Use a different model/server for the high-reasoning tier?\n")
	fmt.Fprintf(os.Stderr, "  (Used for design advice and complex-task plans; the supervisor and simple fixes use %s.) (y/N): ", s.cfg.Model)
	if !sc.Scan() {
		return
	}
	ans := strings.TrimSpace(strings.ToLower(sc.Text()))
	if ans != "y" && ans != "yes" {
		return
	}

	currentBase := s.cfg.BaseURL
	currentKey := s.cfg.APIKey
	if s.cfg.HighReasoning.BaseURL != "" {
		currentBase = s.cfg.HighReasoning.BaseURL
	}
	if s.cfg.HighReasoning.APIKey != "" {
		currentKey = s.cfg.HighReasoning.APIKey
	}

	hiBase := promptWithDefault(sc, "  High-reasoning base URL", currentBase)
	hiKey := promptWithDefault(sc, "  High-reasoning API key", currentKey)

	hiBase = strings.TrimRight(hiBase, "/")
	tempClient := openaiclient.NewFromParams(hiBase, hiKey, "", s.cfg.MaxConcurrent)

	fmt.Fprintf(os.Stderr, "\n  Connecting to %s ...\n", hiBase)
	models, err := tempClient.ListModels(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Could not fetch models: %v\n", err)
		fmt.Fprintf(os.Stderr, "  You can set it manually with /config high_reasoning_model <name>\n\n")
		return
	}
	if len(models) == 0 {
		fmt.Fprintf(os.Stderr, "  Server returned no models.\n")
		return
	}

	fmt.Fprintf(os.Stderr, "\n  Available models:\n\n")
	for i, m := range models {
		fmt.Fprintf(os.Stderr, "    %d) %s\n", i+1, m)
	}
	fmt.Fprintf(os.Stderr, "\n")

	var hiModel string
	for {
		fmt.Fprintf(os.Stderr, "  Select high-reasoning model [1-%d]: ", len(models))
		if !sc.Scan() {
			return
		}
		input := strings.TrimSpace(sc.Text())
		if input == "" {
			continue
		}
		n, err := strconv.Atoi(input)
		if err != nil || n < 1 || n > len(models) {
			for _, m := range models {
				if strings.EqualFold(m, input) {
					hiModel = m
					break
				}
			}
			if hiModel != "" {
				break
			}
			fmt.Fprintf(os.Stderr, "  Please enter a number between 1 and %d.\n", len(models))
			continue
		}
		hiModel = models[n-1]
		break
	}

	if hiBase != s.cfg.BaseURL {
		s.cfg.HighReasoning.BaseURL = hiBase
	} else {
		s.cfg.HighReasoning.BaseURL = ""
	}
	if hiKey != s.cfg.APIKey {
		s.cfg.HighReasoning.APIKey = hiKey
	} else {
		s.cfg.HighReasoning.APIKey = ""
	}
	s.cfg.HighReasoning.Model = hiModel
	fmt.Fprintf(os.Stderr, "  High-reasoning tier will use model %s.\n", hiModel)
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

func saveCurrentConfig(cfg *config.Config) error {
	pc := config.ConfigToPersistent(cfg)
	return config.SavePersistentConfig(pc)
}
