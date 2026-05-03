// Package modelprice maps model names to approximate $/1M input and output token prices.
package modelprice

import (
	"sort"
	"strings"

	"codient/internal/tokentracker"
)

// entry is one row in the built-in pricing table (USD per 1M tokens).
type entry struct {
	prefix                string // lowercase substring match
	inPerMTok, outPerMTok float64
}

// Built-in approximate list prices (subject to provider changes; for estimates only).
var table = []entry{
	{prefix: "gpt-4o-mini", inPerMTok: 0.15, outPerMTok: 0.60},
	{prefix: "gpt-4o", inPerMTok: 2.50, outPerMTok: 10.00},
	{prefix: "gpt-4-turbo", inPerMTok: 10.00, outPerMTok: 30.00},
	{prefix: "gpt-4", inPerMTok: 30.00, outPerMTok: 60.00},
	{prefix: "gpt-3.5-turbo", inPerMTok: 0.50, outPerMTok: 1.50},
	{prefix: "o1-mini", inPerMTok: 3.00, outPerMTok: 12.00},
	{prefix: "o1-preview", inPerMTok: 15.00, outPerMTok: 60.00},
	{prefix: "o1", inPerMTok: 15.00, outPerMTok: 60.00},
	{prefix: "claude-3-5-haiku", inPerMTok: 1.00, outPerMTok: 5.00},
	{prefix: "claude-3-5-sonnet", inPerMTok: 3.00, outPerMTok: 15.00},
	{prefix: "claude-3-opus", inPerMTok: 15.00, outPerMTok: 75.00},
	{prefix: "claude-sonnet-4", inPerMTok: 3.00, outPerMTok: 15.00},
	{prefix: "claude-opus-4", inPerMTok: 15.00, outPerMTok: 75.00},
	{prefix: "claude-haiku-4", inPerMTok: 1.00, outPerMTok: 5.00},
	{prefix: "gemini-2.0-flash", inPerMTok: 0.10, outPerMTok: 0.40},
	{prefix: "gemini-2.5-pro", inPerMTok: 1.25, outPerMTok: 10.00},
	{prefix: "gemini-1.5-pro", inPerMTok: 1.25, outPerMTok: 5.00},
}

func init() {
	sort.Slice(table, func(i, j int) bool {
		return len(table[i].prefix) > len(table[j].prefix)
	})
}

// Lookup returns $/1M input and output for the model, and whether a table row matched.
func Lookup(model string) (inPerMTok, outPerMTok float64, ok bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	if m == "" {
		return 0, 0, false
	}
	for _, e := range table {
		if strings.Contains(m, e.prefix) {
			return e.inPerMTok, e.outPerMTok, true
		}
	}
	return 0, 0, false
}

// EstimateCost computes estimated USD from usage and per-million token rates.
func EstimateCost(u tokentracker.Usage, inPerMTok, outPerMTok float64) float64 {
	return (float64(u.PromptTokens)/1e6)*inPerMTok + (float64(u.CompletionTokens)/1e6)*outPerMTok
}

// EstimateForModel uses the built-in table when no override is set.
func EstimateForModel(model string, u tokentracker.Usage) (cost float64, inPerMTok, outPerMTok float64, matched bool) {
	in, out, ok := Lookup(model)
	if !ok {
		return 0, 0, 0, false
	}
	return EstimateCost(u, in, out), in, out, true
}
