//go:build integration

package intent_test

import (
	"context"
	"os"
	"testing"
	"time"

	"codient/internal/config"
	"codient/internal/intent"
	"codient/internal/openaiclient"
)

// TestIntegration_IdentifyIntent_Categories runs the live supervisor on a small
// set of canned prompts and asserts each falls in the expected category set.
// Models classify subjectively, so each prompt accepts a small allowed list.
//
// Run:
//
//	CODIENT_INTEGRATION=1 go test -tags=integration ./internal/intent/...
func TestIntegration_IdentifyIntent_Categories(t *testing.T) {
	if os.Getenv("CODIENT_INTEGRATION") != "1" {
		t.Skip("set CODIENT_INTEGRATION=1 to run live API tests")
	}
	if testing.Short() {
		t.Skip("skipping live LLM call in -short mode")
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.RequireModel(); err != nil {
		t.Fatal(err)
	}
	client := openaiclient.NewForTier(cfg, "low")

	cases := []struct {
		name    string
		prompt  string
		allowed []intent.Category
	}{
		{
			name:    "query about codebase",
			prompt:  "How does the agent runner handle tool calls?",
			allowed: []intent.Category{intent.CategoryQuery, intent.CategoryDesign},
		},
		{
			name:    "design request",
			prompt:  "How should I structure a plugin system for this app? Don't write code yet, just architectural advice.",
			allowed: []intent.Category{intent.CategoryDesign, intent.CategoryComplexTask},
		},
		{
			name:    "simple fix",
			prompt:  "Fix the typo in the README where it says 'recieve' instead of 'receive'.",
			allowed: []intent.Category{intent.CategorySimpleFix, intent.CategoryComplexTask},
		},
		{
			name:    "complex refactor",
			prompt:  "Refactor the entire authentication module across all files to use the new session API and update every caller.",
			allowed: []intent.Category{intent.CategoryComplexTask, intent.CategoryDesign},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			id, err := intent.IdentifyIntent(ctx, client, tc.prompt, intent.Options{})
			if err != nil {
				t.Fatalf("IdentifyIntent error: %v (id=%+v)", err, id)
			}
			t.Logf("category=%s reasoning=%s fallback=%v", id.Category, id.Reasoning, id.Fallback)
			if id.Fallback {
				t.Fatalf("supervisor returned fallback for prompt %q (reason=%s)", tc.prompt, id.Reasoning)
			}
			ok := false
			for _, c := range tc.allowed {
				if id.Category == c {
					ok = true
					break
				}
			}
			if !ok {
				t.Fatalf("category %q not in allowed set %v for prompt %q", id.Category, tc.allowed, tc.prompt)
			}
		})
	}
}
