package agent

import "errors"

// ErrMaxTurns is returned when Runner.MaxTurns is exceeded (LLM rounds in one user turn).
var ErrMaxTurns = errors.New("agent: max LLM turns exceeded")

// ErrMaxCost is returned when Runner.MaxCostUSD is exceeded (estimated session cost).
var ErrMaxCost = errors.New("agent: max estimated cost exceeded")
