package service

import (
	"strings"
	"testing"
)

// TestScanRoleDir verifies the dual-format auto-detection: frontmatter roles in
// .claude/agents/*.md (with dereference to agents/<name>/<name>.md) plus
// prose-only roles discovered via the nested agents/<role>/<role>.md fallback,
// deduped by name.
func TestScanRoleDir(t *testing.T) {
	roles, err := ScanRoleDir("testdata/roleproj")
	if err != nil {
		t.Fatalf("ScanRoleDir: %v", err)
	}

	byName := map[string]ParsedRole{}
	for _, r := range roles {
		byName[r.Name] = r
	}

	// coder: parsed from frontmatter, name + description from YAML.
	coder, ok := byName["coder"]
	if !ok {
		t.Fatalf("expected a 'coder' role, got %v", keys(byName))
	}
	if !strings.Contains(coder.Description, "Subtask implementer") {
		t.Fatalf("coder description should come from frontmatter, got %q", coder.Description)
	}
	// Dereference: the body points at agents/coder/coder.md, whose marker must
	// be appended to the instructions.
	if !strings.Contains(coder.Instructions, "DEREF_MARKER_FULL_BODY") {
		t.Fatalf("coder instructions should include the dereferenced full body")
	}
	if !strings.Contains(coder.Instructions, "You are the coder") {
		t.Fatalf("coder instructions should also keep the frontmatter body")
	}

	// lonewolf: prose-only, discovered via the agents/<role>/<role>.md fallback.
	lone, ok := byName["lonewolf"]
	if !ok {
		t.Fatalf("expected a 'lonewolf' prose-only role, got %v", keys(byName))
	}
	if !strings.Contains(lone.Instructions, "prose-only role") {
		t.Fatalf("lonewolf instructions should be its full file content")
	}

	// No duplicate of coder (frontmatter wins; the nested file must not re-add it).
	count := 0
	for _, r := range roles {
		if r.Name == "coder" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("coder must appear exactly once (dedup), got %d", count)
	}
}

func keys(m map[string]ParsedRole) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
