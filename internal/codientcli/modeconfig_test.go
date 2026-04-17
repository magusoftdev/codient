package codientcli

import "testing"

func TestParseModeConfigKey_Valid(t *testing.T) {
	cases := []struct {
		key       string
		wantMode  string
		wantField string
	}{
		{"plan_model", "plan", "model"},
		{"plan_base_url", "plan", "base_url"},
		{"plan_api_key", "plan", "api_key"},
		{"build_model", "build", "model"},
		{"build_base_url", "build", "base_url"},
		{"build_api_key", "build", "api_key"},
		{"ask_model", "ask", "model"},
		{"ask_base_url", "ask", "base_url"},
		{"ask_api_key", "ask", "api_key"},
	}
	for _, tc := range cases {
		mode, field, ok := parseModeConfigKey(tc.key)
		if !ok {
			t.Errorf("%q: expected match", tc.key)
			continue
		}
		if mode != tc.wantMode {
			t.Errorf("%q: mode got %q want %q", tc.key, mode, tc.wantMode)
		}
		if field != tc.wantField {
			t.Errorf("%q: field got %q want %q", tc.key, field, tc.wantField)
		}
	}
}

func TestParseModeConfigKey_Invalid(t *testing.T) {
	invalid := []string{
		"model",
		"base_url",
		"api_key",
		"plan_workspace",
		"debug_model",
		"plan_",
		"_model",
		"",
		"planmodel",
		"plan model",
	}
	for _, key := range invalid {
		_, _, ok := parseModeConfigKey(key)
		if ok {
			t.Errorf("%q: should not match", key)
		}
	}
}
