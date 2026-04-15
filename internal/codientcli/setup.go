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

	// Optional per-mode plan server override.
	s.setupPlanModeOverride(ctx, sc)

	if err := saveCurrentConfig(s.cfg); err != nil {
		fmt.Fprintf(os.Stderr, "  Warning: could not save config: %v\n", err)
	}
	fmt.Fprintf(os.Stderr, "\n  Configuration saved. Model set to %s.\n\n", s.cfg.Model)
	return true
}

// setupPlanModeOverride optionally configures a separate model/server for plan mode.
func (s *session) setupPlanModeOverride(ctx context.Context, sc *bufio.Scanner) {
	fmt.Fprintf(os.Stderr, "\n  Use a different model/server for plan mode? (y/N): ")
	if !sc.Scan() {
		return
	}
	ans := strings.TrimSpace(strings.ToLower(sc.Text()))
	if ans != "y" && ans != "yes" {
		return
	}

	currentBase := s.cfg.BaseURL
	currentKey := s.cfg.APIKey
	if ov, ok := s.cfg.ModeModels["plan"]; ok {
		if ov.BaseURL != "" {
			currentBase = ov.BaseURL
		}
		if ov.APIKey != "" {
			currentKey = ov.APIKey
		}
	}

	planBase := promptWithDefault(sc, "  Plan base URL", currentBase)
	planKey := promptWithDefault(sc, "  Plan API key", currentKey)

	planBase = strings.TrimRight(planBase, "/")
	tempClient := openaiclient.NewFromParams(planBase, planKey, "", s.cfg.MaxConcurrent)

	fmt.Fprintf(os.Stderr, "\n  Connecting to %s ...\n", planBase)
	models, err := tempClient.ListModels(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Could not fetch models: %v\n", err)
		fmt.Fprintf(os.Stderr, "  You can set the plan model manually with /config plan_model <name>\n\n")
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

	var planModel string
	for {
		fmt.Fprintf(os.Stderr, "  Select plan model [1-%d]: ", len(models))
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
					planModel = m
					break
				}
			}
			if planModel != "" {
				break
			}
			fmt.Fprintf(os.Stderr, "  Please enter a number between 1 and %d.\n", len(models))
			continue
		}
		planModel = models[n-1]
		break
	}

	if s.cfg.ModeModels == nil {
		s.cfg.ModeModels = make(map[string]config.ModeConnectionOverride)
	}
	ov := s.cfg.ModeModels["plan"]
	// Only store overrides that differ from top-level to keep config clean.
	if planBase != s.cfg.BaseURL {
		ov.BaseURL = planBase
	} else {
		ov.BaseURL = ""
	}
	if planKey != s.cfg.APIKey {
		ov.APIKey = planKey
	} else {
		ov.APIKey = ""
	}
	ov.Model = planModel
	s.cfg.ModeModels["plan"] = ov
	fmt.Fprintf(os.Stderr, "  Plan mode will use model %s.\n", planModel)
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
