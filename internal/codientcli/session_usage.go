package codientcli

import (
	"fmt"
	"os"

	"codient/internal/modelprice"
	"codient/internal/tokentracker"
)

func (s *session) printTurnTokenSummary() {
	if s.tokenTracker == nil {
		return
	}
	u := s.tokenTracker.TurnSinceMark()
	if !u.HasAny() {
		return
	}
	est, hasPrice := s.estimateCostForUsage(u)
	if line := tokentracker.FormatTurnSummary(s.turn, u, est, hasPrice); line != "" {
		fmt.Fprintln(os.Stderr, line)
	}
}

func (s *session) estimateCostForUsage(u tokentracker.Usage) (float64, bool) {
	if s.cfg.CostPerMTok != nil {
		return modelprice.EstimateCost(u, s.cfg.CostPerMTok.Input, s.cfg.CostPerMTok.Output), true
	}
	c, _, _, ok := modelprice.EstimateForModel(s.cfg.Model, u)
	return c, ok
}

func (s *session) formatCostStatusLine() string {
	if s.tokenTracker == nil {
		return ""
	}
	u := s.tokenTracker.Session()
	if !u.HasAny() {
		return ""
	}
	line := fmt.Sprintf("  tokens:   %d in / %d out (session total)\n", u.PromptTokens, u.CompletionTokens)
	est, has := s.estimateCostForUsage(u)
	if has {
		line += fmt.Sprintf("  est cost: %s\n", tokentracker.FormatUSD(est))
	}
	return line
}

func (s *session) printCostCommand() {
	if s.tokenTracker == nil {
		fmt.Fprintf(os.Stderr, "codient: token tracking unavailable\n")
		return
	}
	u := s.tokenTracker.Session()
	var in, out float64
	fromTable := true
	if s.cfg.CostPerMTok != nil {
		in, out = s.cfg.CostPerMTok.Input, s.cfg.CostPerMTok.Output
		fromTable = false
	} else {
		var ok bool
		in, out, ok = modelprice.Lookup(s.cfg.Model)
		if !ok {
			in, out = 0, 0
		}
	}
	fmt.Fprint(os.Stderr, tokentracker.FormatFullBlock(u, s.cfg.Model, in, out, fromTable))
}
