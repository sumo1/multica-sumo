package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"gopkg.in/yaml.v3"
)

// RoleSyncService reads role definitions from a project's bound local directory
// and materializes them as workspace Agents (the L1 "role" layer). This is the
// "associate a project → auto-sync its roles" capability: the repo is the SSOT,
// the platform is a projection. Sync is one-way (repo → platform); platform
// Agents are never written back to the repo.
//
// Two on-disk formats are auto-detected (a repo may use either or both):
//
//  1. .claude/agents/*.md — Claude Code standard, YAML frontmatter
//     (name/description/color) + a body. The body often dereferences a fuller
//     definition at agents/<name>/<name>.md; when it does, that file's content
//     is appended to the instructions.
//  2. roles/*.md or agents/<role>/<role>.md — harness prose (no frontmatter):
//     the filename / first H1 is the role name, the whole file is instructions.
//
// Synced agents are upserted by name: a new name creates an Agent, an existing
// one updates its instructions/description (idempotent re-sync).
type RoleSyncService struct {
	Queries *db.Queries
}

func NewRoleSyncService(q *db.Queries) *RoleSyncService {
	return &RoleSyncService{Queries: q}
}

// ParsedRole is a role definition extracted from disk, before it becomes an Agent.
type ParsedRole struct {
	Name         string
	Description  string
	Instructions string
	// Source is the relative path it was parsed from, for logging/diagnostics.
	Source string
}

// RoleSyncResult summarizes one sync pass.
type RoleSyncResult struct {
	Created []string `json:"created"`
	Updated []string `json:"updated"`
	Skipped []string `json:"skipped"`
}

// localDirRef is the resource_ref shape for a local_directory project resource.
type localDirRef struct {
	LocalPath string `json:"local_path"`
	DaemonID  string `json:"daemon_id"`
}

// SyncProjectRoles scans the project's local_directory resource for role
// definitions and upserts them as workspace Agents. Returns what changed.
//
// Scope note: the directory is read from the server's own filesystem. This is
// correct for local dev / single-host deploys where the daemon dir is reachable
// by the server. A remote-daemon deploy would route the read through the daemon
// (a future extension); here it reads directly.
func (s *RoleSyncService) SyncProjectRoles(
	ctx context.Context,
	workspaceID, projectID, creatorID pgtype.UUID,
) (RoleSyncResult, error) {
	dir, err := s.resolveProjectDir(ctx, workspaceID, projectID)
	if err != nil {
		return RoleSyncResult{}, err
	}

	roles, err := ScanRoleDir(dir)
	if err != nil {
		return RoleSyncResult{}, err
	}
	if len(roles) == 0 {
		return RoleSyncResult{}, fmt.Errorf("no role definitions found under %s (looked in .claude/agents, agents, roles)", dir)
	}

	// Pick a runtime for new agents: any usable runtime in the workspace. Agents
	// require a runtime_id; sync reuses the workspace's first available one. The
	// agent's runtime_mode MUST match the runtime's mode (local vs cloud) or
	// dispatch fails with agent_error — so carry the runtime's mode through.
	runtime, err := s.resolveRuntime(ctx, workspaceID)
	if err != nil {
		return RoleSyncResult{}, err
	}

	existing, err := s.Queries.ListAgents(ctx, workspaceID)
	if err != nil {
		return RoleSyncResult{}, fmt.Errorf("list agents: %w", err)
	}
	byName := make(map[string]db.Agent, len(existing))
	for _, a := range existing {
		byName[a.Name] = a
	}

	// Inherit custom_env from an existing runnable agent on the same runtime, so
	// provider credentials configured per-agent (e.g. CLAUDE_CODE_USE_BEDROCK for
	// Bedrock relays — stripped from the inherited process env by the daemon's
	// CLAUDE_CODE_* filter, see [[daemon-agent-env-bedrock-403]]) propagate to
	// synced roles. Without this, synced agents start with custom_env={} and fail
	// dispatch with 403 in Bedrock setups. Falls back to "{}" when no donor.
	customEnv := s.inheritCustomEnv(existing, runtime.ID)

	var result RoleSyncResult
	for _, role := range roles {
		if cur, ok := byName[role.Name]; ok {
			// Idempotent update: refresh instructions + description.
			_, uerr := s.Queries.UpdateAgent(ctx, db.UpdateAgentParams{
				ID:           cur.ID,
				Instructions: pgtype.Text{String: role.Instructions, Valid: true},
				Description:  pgtype.Text{String: role.Description, Valid: true},
			})
			if uerr != nil {
				slog.Warn("role sync: update agent failed", "name", role.Name, "err", uerr)
				result.Skipped = append(result.Skipped, role.Name)
				continue
			}
			result.Updated = append(result.Updated, role.Name)
			continue
		}

		_, cerr := s.Queries.CreateAgent(ctx, db.CreateAgentParams{
			WorkspaceID:        workspaceID,
			Name:               role.Name,
			Description:        role.Description,
			RuntimeMode:        runtime.RuntimeMode,
			RuntimeConfig:      []byte("{}"),
			RuntimeID:          runtime.ID,
			Visibility:         "workspace",
			MaxConcurrentTasks: 6,
			OwnerID:            creatorID,
			Instructions:       role.Instructions,
			CustomEnv:          customEnv,
			CustomArgs:         []byte("[]"),
			// nil → SQL NULL: no MCP servers. A bare "{}" lacks the required
			// `mcpServers` key and makes the claude CLI reject the config
			// ("Invalid MCP configuration: mcpServers ... received undefined").
			McpConfig: nil,
		})
		if cerr != nil {
			slog.Warn("role sync: create agent failed", "name", role.Name, "err", cerr)
			result.Skipped = append(result.Skipped, role.Name)
			continue
		}
		result.Created = append(result.Created, role.Name)
	}

	slog.Info("project roles synced",
		"project_id", uuidString(projectID),
		"dir", dir,
		"created", len(result.Created),
		"updated", len(result.Updated),
		"skipped", len(result.Skipped),
	)
	return result, nil
}

// ProjectRoleNames returns the set of role names defined in the project's repo
// (scanned from its local_directory, same source as SyncProjectRoles). Used by
// goal planning to flag which workspace agents are THIS project's own harness
// roles, so the planner prefers them. Returns an empty set (not an error) when
// the project has no local dir or no roles — callers treat "no info" as "no
// project-preferred agents", which is the right default.
func (s *RoleSyncService) ProjectRoleNames(ctx context.Context, workspaceID, projectID pgtype.UUID) map[string]bool {
	out := map[string]bool{}
	dir, err := s.resolveProjectDir(ctx, workspaceID, projectID)
	if err != nil {
		return out
	}
	roles, err := ScanRoleDir(dir)
	if err != nil {
		return out
	}
	for _, r := range roles {
		if name := strings.TrimSpace(r.Name); name != "" {
			out[name] = true
		}
	}
	return out
}

// resolveProjectDir finds the project's local_directory resource and returns its
// absolute local path.
func (s *RoleSyncService) resolveProjectDir(ctx context.Context, workspaceID, projectID pgtype.UUID) (string, error) {
	resources, err := s.Queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return "", fmt.Errorf("list project resources: %w", err)
	}
	for _, r := range resources {
		if r.WorkspaceID != workspaceID {
			continue
		}
		if r.ResourceType != "local_directory" {
			continue
		}
		var ref localDirRef
		if err := json.Unmarshal(r.ResourceRef, &ref); err != nil {
			continue
		}
		if strings.TrimSpace(ref.LocalPath) != "" {
			return ref.LocalPath, nil
		}
	}
	return "", fmt.Errorf("project has no local_directory resource to sync roles from")
}

// inheritCustomEnv returns a custom_env to seed synced agents with, copied from
// an existing non-archived agent on the same runtime (provider credentials are
// configured per-agent and must carry over — see [[daemon-agent-env-bedrock-403]]).
// Prefers a donor on the same runtime; falls back to any non-archived agent with
// a non-empty custom_env; else "{}".
func (s *RoleSyncService) inheritCustomEnv(existing []db.Agent, runtimeID pgtype.UUID) []byte {
	var fallback []byte
	for _, a := range existing {
		if a.ArchivedAt.Valid || len(a.CustomEnv) == 0 || string(a.CustomEnv) == "{}" {
			continue
		}
		if a.RuntimeID == runtimeID {
			return a.CustomEnv // best match: same runtime
		}
		if fallback == nil {
			fallback = a.CustomEnv
		}
	}
	if fallback != nil {
		return fallback
	}
	return []byte("{}")
}

// resolveRuntime returns a usable runtime in the workspace for new agents,
// preferring an online one. The caller copies its RuntimeMode onto the agent so
// the two stay consistent (a mismatch causes dispatch agent_error).
func (s *RoleSyncService) resolveRuntime(ctx context.Context, workspaceID pgtype.UUID) (db.AgentRuntime, error) {
	runtimes, err := s.Queries.ListAgentRuntimes(ctx, workspaceID)
	if err != nil {
		return db.AgentRuntime{}, fmt.Errorf("list runtimes: %w", err)
	}
	if len(runtimes) == 0 {
		return db.AgentRuntime{}, fmt.Errorf("workspace has no runtime; create one before syncing roles")
	}
	for _, rt := range runtimes {
		if rt.Status == "online" {
			return rt, nil
		}
	}
	return runtimes[0], nil
}

// ─── Pure parsing (no DB) — unit-testable on a fixture dir ──────────────────

var frontmatterRe = regexp.MustCompile(`(?s)^\s*---\s*\n(.*?)\n---\s*\n(.*)$`)
var derefRe = regexp.MustCompile("(?:agents|roles)/[\\w./-]+\\.md")
var h1Re = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// ScanRoleDir scans a project directory for role definitions in priority order:
//  1. .claude/agents/*.md (frontmatter) — dereferences agents/<name>/<name>.md
//  2. roles/*.md (prose)
//  3. agents/<role>/<role>.md (prose) — only roles not already found
//
// Pure function over the filesystem so it can be unit-tested against a fixture.
func ScanRoleDir(dir string) ([]ParsedRole, error) {
	seen := map[string]bool{}
	var roles []ParsedRole

	// 1. .claude/agents/*.md (frontmatter, highest priority)
	claudeDir := filepath.Join(dir, ".claude", "agents")
	if entries, err := os.ReadDir(claudeDir); err == nil {
		for _, e := range sortedMD(entries) {
			path := filepath.Join(claudeDir, e)
			role, ok := parseFrontmatterRole(dir, path)
			if !ok || seen[role.Name] {
				continue
			}
			seen[role.Name] = true
			roles = append(roles, role)
		}
	}

	// 2. roles/*.md (prose)
	for _, sub := range []string{"roles"} {
		proseDir := filepath.Join(dir, sub)
		if entries, err := os.ReadDir(proseDir); err == nil {
			for _, e := range sortedMD(entries) {
				path := filepath.Join(proseDir, e)
				role, ok := parseProseRole(path)
				if !ok || seen[role.Name] {
					continue
				}
				seen[role.Name] = true
				roles = append(roles, role)
			}
		}
	}

	// 3. agents/<role>/<role>.md (prose, nested) — fills in any not yet seen
	agentsDir := filepath.Join(dir, "agents")
	if subdirs, err := os.ReadDir(agentsDir); err == nil {
		for _, sd := range subdirs {
			if !sd.IsDir() {
				continue
			}
			roleDir := filepath.Join(agentsDir, sd.Name())
			files, ferr := os.ReadDir(roleDir)
			if ferr != nil {
				continue
			}
			for _, f := range sortedMD(files) {
				path := filepath.Join(roleDir, f)
				role, ok := parseProseRole(path)
				if !ok {
					continue
				}
				// Prefer the directory name as the role name for nested defs.
				role.Name = sd.Name()
				if seen[role.Name] {
					continue
				}
				seen[role.Name] = true
				roles = append(roles, role)
			}
		}
	}

	return roles, nil
}

// sortedMD returns the .md filenames in an entry list, sorted for determinism.
func sortedMD(entries []os.DirEntry) []string {
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// parseFrontmatterRole parses a .claude/agents/*.md file: YAML frontmatter for
// name/description, body for instructions, and dereferences any agents/<x>.md
// path mentioned in the body so the full definition is captured.
func parseFrontmatterRole(projectDir, path string) (ParsedRole, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ParsedRole{}, false
	}
	m := frontmatterRe.FindSubmatch(raw)
	if m == nil {
		return ParsedRole{}, false
	}
	var fm frontmatter
	if err := yaml.Unmarshal(m[1], &fm); err != nil {
		return ParsedRole{}, false
	}
	name := strings.TrimSpace(fm.Name)
	if name == "" {
		// Fall back to the filename stem.
		name = strings.TrimSuffix(filepath.Base(path), ".md")
	}
	body := strings.TrimSpace(string(m[2]))

	// Dereference: if the body points at agents/<x>/<y>.md, append its content.
	instructions := body
	for _, rel := range derefRe.FindAllString(body, -1) {
		full := filepath.Join(projectDir, rel)
		if extra, rerr := os.ReadFile(full); rerr == nil {
			instructions += "\n\n" + strings.TrimSpace(string(extra))
		}
	}

	return ParsedRole{
		Name:         name,
		Description:  strings.TrimSpace(fm.Description),
		Instructions: instructions,
		Source:       path,
	}, true
}

// parseProseRole parses a prose role file (no frontmatter): the first H1 (or the
// filename stem) is the name, the whole file is the instructions.
func parseProseRole(path string) (ParsedRole, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ParsedRole{}, false
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return ParsedRole{}, false
	}
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	if m := h1Re.FindStringSubmatch(content); m != nil {
		// Use the H1 text up to the first separator (e.g. "coder — 实现流程").
		h := strings.TrimSpace(m[1])
		for _, sep := range []string{" — ", " - ", "—", ":"} {
			if i := strings.Index(h, sep); i > 0 {
				h = strings.TrimSpace(h[:i])
				break
			}
		}
		if h != "" {
			name = h
		}
	}

	// First non-heading paragraph as a short description.
	desc := ""
	for _, line := range strings.Split(content, "\n") {
		l := strings.TrimSpace(line)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		desc = l
		break
	}

	return ParsedRole{
		Name:         name,
		Description:  desc,
		Instructions: content,
		Source:       path,
	}, true
}

// uuidString is a tiny local helper to avoid importing util just for logging.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	b := u.Bytes
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
