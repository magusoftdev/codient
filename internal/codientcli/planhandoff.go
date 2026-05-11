package codientcli

import (
	"codient/internal/designstore"
	"codient/internal/planstore"
)

// planHandoffApplies returns true when the given plan / last assistant reply justify
// injecting an implementation directive on a plan->build transition.
func planHandoffApplies(plan *planstore.Plan, lastAssistantReply string) bool {
	if plan != nil && len(plan.Steps) > 0 {
		return true
	}
	return designstore.LooksLikeReadyToImplement(lastAssistantReply)
}
