package agent

import (
	"os"
	"strings"
	"testing"
)

func TestMuninnIdentityMentionsOfficialMCPTools(t *testing.T) {
	workspace := setupWorkspace(t, map[string]string{})
	defer func() { _ = os.RemoveAll(workspace) }()

	cb := NewContextBuilderWithMemoryMode(workspace, NewNoopMemoryStore(), true).WithMuninnVault("picoclaw")
	identity := cb.getIdentity()

	if !strings.Contains(identity, "official Muninn MCP") {
		t.Fatalf("identity does not mention official Muninn MCP tools: %s", identity)
	}
	if strings.Contains(identity, "Use `memory_store`") || strings.Contains(identity, "Use `memory_recall`") {
		t.Fatalf("identity should not instruct local memory_store/memory_recall in Muninn mode: %s", identity)
	}
	if !strings.Contains(identity, "exact contact mapping such as chat_id") {
		t.Fatalf("identity should instruct recalling exact contact mappings before third-party sends: %s", identity)
	}
	if !strings.Contains(identity, `"picoclaw"`) {
		t.Fatalf("identity should mention configured muninn vault: %s", identity)
	}
}
