package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestSyncProjectRolesEndToEnd verifies the full DB-backed sync: a project with
// a local_directory resource pointing at a role fixture → Agents created in the
// workspace, and a re-sync updates (not duplicates) them.
func TestSyncProjectRolesEndToEnd(t *testing.T) {
	ctx := context.Background()
	queries := db.New(testPool)
	svc := service.NewRoleSyncService(queries)

	wsUUID := parseUUID(testWorkspaceID)
	userUUID := parseUUID(testUserID)

	// Absolute path to the parser fixture committed under internal/service.
	abs, err := filepath.Abs("../../internal/service/testdata/roleproj")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}

	// Create a project + local_directory resource pointing at the fixture.
	proj, err := queries.CreateProject(ctx, db.CreateProjectParams{
		WorkspaceID: wsUUID,
		Title:       "Role Sync Test Project",
		Status:      "planned",
		Priority:    "none",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project WHERE id=$1`, proj.ID)
	})

	ref, _ := json.Marshal(map[string]string{"local_path": abs, "daemon_id": "test"})
	if _, err := queries.CreateProjectResource(ctx, db.CreateProjectResourceParams{
		ProjectID:    proj.ID,
		WorkspaceID:  wsUUID,
		ResourceType: "local_directory",
		ResourceRef:  ref,
		Position:     0,
		CreatedBy:    userUUID,
	}); err != nil {
		t.Fatalf("CreateProjectResource: %v", err)
	}

	// Clean up any agents the sync creates (coder, lonewolf).
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM agent WHERE workspace_id=$1 AND name IN ('coder','lonewolf')`, testWorkspaceID)
	})

	// First sync → creates the roles.
	res, err := svc.SyncProjectRoles(ctx, wsUUID, proj.ID, userUUID)
	if err != nil {
		t.Fatalf("SyncProjectRoles: %v", err)
	}
	if len(res.Created) < 2 {
		t.Fatalf("expected >=2 roles created, got %v", res.Created)
	}

	// The coder agent exists with the dereferenced full body in instructions,
	// a runtime_mode matching its runtime, and NULL mcp_config (a bare "{}"
	// makes the claude CLI reject the config with "Invalid MCP configuration").
	var instr, runtimeMode string
	var mcpConfig []byte
	if err := testPool.QueryRow(ctx,
		`SELECT instructions, runtime_mode, mcp_config FROM agent WHERE workspace_id=$1 AND name='coder'`,
		testWorkspaceID,
	).Scan(&instr, &runtimeMode, &mcpConfig); err != nil {
		t.Fatalf("load synced coder agent: %v", err)
	}
	if !strings.Contains(instr, "DEREF_MARKER_FULL_BODY") {
		t.Fatalf("synced coder instructions should include the dereferenced body")
	}
	if runtimeMode == "" {
		t.Fatalf("synced agent runtime_mode should match its runtime, got empty")
	}
	if mcpConfig != nil {
		t.Fatalf("synced agent mcp_config must be NULL (not %q) so the claude CLI accepts it", string(mcpConfig))
	}

	// Re-sync → updates, does not duplicate.
	res2, err := svc.SyncProjectRoles(ctx, wsUUID, proj.ID, userUUID)
	if err != nil {
		t.Fatalf("re-sync: %v", err)
	}
	if len(res2.Created) != 0 {
		t.Fatalf("re-sync should create nothing, got %v", res2.Created)
	}
	if len(res2.Updated) < 2 {
		t.Fatalf("re-sync should update existing roles, got %v", res2.Updated)
	}

	var cnt int
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM agent WHERE workspace_id=$1 AND name='coder'`, testWorkspaceID,
	).Scan(&cnt); err != nil {
		t.Fatalf("count coder: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("coder must exist exactly once after re-sync, got %d", cnt)
	}
}
