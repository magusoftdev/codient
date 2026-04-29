package codientcli

import (
	"encoding/json"
	"testing"
)

func TestAcpStructuredPatchPreview_strReplace(t *testing.T) {
	args := json.RawMessage(`{"path":"src/a.go","old_string":"foo","new_string":"bar"}`)
	got := acpStructuredPatchPreview("str_replace", args)
	if got == nil {
		t.Fatal("nil preview")
	}
	if got["type"] != "str_replace" || got["path"] != "src/a.go" {
		t.Fatalf("preview: %#v", got)
	}
}

func TestAcpStructuredPatchPreview_patchFile(t *testing.T) {
	args := json.RawMessage(`{"path":"b.txt","diff":"--- a\n+++ b\n"}`)
	got := acpStructuredPatchPreview("patch_file", args)
	if got == nil {
		t.Fatal("nil preview")
	}
	if got["type"] != "patch_file" || got["path"] != "b.txt" {
		t.Fatalf("preview: %#v", got)
	}
}

func TestAcpStructuredPatchPreview_otherTool(t *testing.T) {
	args := json.RawMessage(`{"path":"x"}`)
	if acpStructuredPatchPreview("read_file", args) != nil {
		t.Fatal("expected nil for read_file")
	}
}
