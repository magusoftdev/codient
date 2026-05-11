package codientcli

import "testing"

func TestIsReasoningTierConfigKey(t *testing.T) {
	yes := []string{
		"low_reasoning_base_url",
		"low_reasoning_api_key",
		"low_reasoning_model",
		"high_reasoning_base_url",
		"high_reasoning_api_key",
		"high_reasoning_model",
	}
	for _, k := range yes {
		if !isReasoningTierConfigKey(k) {
			t.Errorf("%q: should be a reasoning-tier key", k)
		}
	}
	no := []string{
		"model",
		"base_url",
		"api_key",
		"plan_model",
		"build_model",
		"ask_model",
		"low_reasoning_workspace",
		"low_reasoning_",
		"_model",
		"",
	}
	for _, k := range no {
		if isReasoningTierConfigKey(k) {
			t.Errorf("%q: should NOT be a reasoning-tier key", k)
		}
	}
}
