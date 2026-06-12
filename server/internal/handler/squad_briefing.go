package handler

import (
	"context"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// squadOperatingProtocol is the hard-coded system-level briefing prepended to
// every squad-leader claim. It explains the leader's coordinator role, the
// @mention dispatch mechanism, and the stop-after-dispatch contract.
//
// Keep this text English-only (matches existing agent-harness conventions)
// and keep the mention syntax exactly aligned with util.MentionRe — the
// "Squad Roster" block below renders concrete examples that round-trip
// through util.ParseMentions, and the protocol text refers to that format.
const squadOperatingProtocol = `## Squad Operating Protocol

You are the LEADER of a squad. Your job is to **coordinate**, not to execute
the work yourself.

Your responsibilities, in order:

1. **Read the issue** (title, description, latest comments, acceptance
   criteria) and decide which squad member is best suited to do the work.
2. **Delegate by @mention.** Post a single comment on this issue that
   @mentions the chosen member(s) and tells them what to do.
   - **Be terse.** Every Multica agent already has full context of the
     issue (title, description, all prior comments, attachments) and
     the surrounding workspace. Do NOT restate or summarise the
     issue body, prior discussion, or known facts in your delegation
     comment — they read it themselves.
   - Say only what cannot be inferred from the issue: who you're
     picking, why them (one short clause), and any *additional*
     constraints, hints, or sequencing you want them to follow.
     Two or three sentences is usually plenty.
   - Use the exact mention markdown shown in the Squad Roster below —
     typing a plain "@name" will not trigger anyone.
3. **Record your evaluation.** After every trigger — whether you delegated,
   decided no action is needed, or encountered an error — record it:
   ` + "`" + `multica squad activity <issue-id> <outcome> --reason "<short reason>"` + "`" + `
   Outcome values: ` + "`" + `action` + "`" + ` (you delegated or acted),
   ` + "`" + `no_action` + "`" + ` (you evaluated and decided nothing is needed),
   ` + "`" + `failed` + "`" + ` (you hit an error).
   This is mandatory on every turn — it records your decision in the
   issue timeline so humans can see you evaluated the trigger.
4. **Stop after dispatching.** Once your delegation comment is posted
   and evaluation recorded, end your turn. Do not continue working,
   do not write code, do not open files. You will be re-triggered
   automatically when:
   - a delegated member posts an update or asks you a question;
   - a delegated member finishes and the issue moves forward;
   - someone @mentions you again on this issue.
5. **Re-evaluate on each trigger.** When you wake up again, read the new
   activity and decide whether to delegate the next step, escalate to
   the human reporter, or close the loop. If no action is needed
   (e.g. a member posted a progress update that requires no response),
   record ` + "`" + `no_action` + "`" + ` and exit silently.

Hard rules:
- EVERY delegation MUST use the full mention markdown syntax
  ` + "`" + `[@Name](mention://<type>/<UUID>)` + "`" + ` exactly as shown in the Squad
  Roster. A plain "@name" or bare name does NOT trigger the agent —
  if you skip the mention link, the task is never delivered and the
  issue stalls. This is non-negotiable: no mention link = no delegation.
- Do NOT restate the issue body or prior comments in your delegation —
  the assignee already has them. Repeating context is noise that
  buries the actual instruction.
- Do NOT do the implementation work yourself unless the squad has no
  other suitable members. The squad exists so work is split — bypassing
  it defeats the point.
- Do NOT @mention members who don't appear in the Squad Roster below;
  they are not part of this squad.
- One delegation comment per turn is enough. Avoid spamming multiple
  near-identical comments.
- If the squad has no member capable of the task, post a comment
  explaining the gap (and @mention the issue's reporter if possible)
  rather than silently doing the work.
- ALWAYS call ` + "`" + `multica squad activity` + "`" + ` before ending your turn —
  even when the outcome is no_action.
- A child issue you create with ` + "`" + `--status todo` + "`" + ` and an agent assignee
  already fires that agent automatically — the assignment IS the trigger.
  If you also @mention the same agent on this parent issue for the same
  work, the agent runs twice in parallel (once from the mention, once
  from the assignment). Pick exactly one path: either delegate by
  @mention on this issue, or create a ` + "`" + `todo` + "`" + ` child issue assigned to
  them. Never both for the same work.`

// buildSquadLeaderBriefing composes the full system briefing appended to a
// squad leader's Instructions when it claims a task on a squad-assigned
// issue. The returned string contains three sections:
//
//  1. Squad Operating Protocol (constant, system-level rules).
//  2. Squad Roster (data — leader self-row + members with literal
//     `[@Name](mention://<type>/<UUID>)` strings ready to paste).
//  3. Squad Instructions (user-defined `squad.instructions`, omitted when
//     empty so we don't leave a dangling heading).
//
// Archived agent members are skipped — there's no point asking the leader
// to delegate to a retired agent. Members whose underlying record can't be
// loaded (deleted user/agent races, FK weirdness) are also skipped silently.
func buildSquadLeaderBriefing(ctx context.Context, q *db.Queries, squad db.Squad) string {
	var sb strings.Builder
	sb.WriteString(squadOperatingProtocol)
	sb.WriteString("\n\n")
	sb.WriteString(buildSquadRoster(ctx, q, squad))

	if trimmed := strings.TrimSpace(squad.Instructions); trimmed != "" {
		sb.WriteString("\n\n## Squad Instructions (")
		sb.WriteString(squad.Name)
		sb.WriteString(")\n\n")
		sb.WriteString(trimmed)
	}
	return sb.String()
}

// buildSquadRoster renders the "## Squad Roster" section: a leader self-row
// plus one row per non-archived member, with literal mention markdown.
func buildSquadRoster(ctx context.Context, q *db.Queries, squad db.Squad) string {
	var sb strings.Builder
	sb.WriteString("## Squad Roster\n\n")

	// Leader self-row. Leaders are always agents (FK enforced in schema).
	leaderName := "Leader"
	if leader, err := q.GetAgent(ctx, squad.LeaderID); err == nil {
		leaderName = leader.Name
	}
	sb.WriteString("Leader (you):\n")
	sb.WriteString("- ")
	sb.WriteString(leaderName)
	sb.WriteString(" — agent — `")
	sb.WriteString(formatMention(leaderName, "agent", util.UUIDToString(squad.LeaderID)))
	sb.WriteString("`\n")

	members, err := q.ListSquadMembers(ctx, squad.ID)
	if err != nil {
		members = nil
	}

	rows := make([]string, 0, len(members))
	for _, m := range members {
		// Skip the leader if they happen to also be in the member list —
		// they're already shown above and we don't want self-delegation.
		if m.MemberType == "agent" && util.UUIDToString(m.MemberID) == util.UUIDToString(squad.LeaderID) {
			continue
		}
		row := renderMemberRow(ctx, q, m)
		if row != "" {
			rows = append(rows, row)
		}
	}

	if len(rows) == 0 {
		sb.WriteString("\nMembers: (none — you are the only member of this squad)\n")
		return sb.String()
	}

	sb.WriteString("\nMembers:\n")
	for _, r := range rows {
		sb.WriteString(r)
	}
	return sb.String()
}

// buildPlanningRoster is the roster shown to a goal PLANNER. It is the squad
// roster PLUS an "available workspace roles" pool — every non-archived workspace
// agent with a runtime that isn't already the leader/a squad member. This is the
// fix for "all subtasks went to the same agent": a task's dynamic squad often
// holds only the leader, but the planner must be able to assign nodes to the
// workspace's role agents (coder / reviewer / evaluator …). dispatchSubtask
// admits any non-archived workspace agent with a runtime, so pool agents are
// dispatchable without squad membership.
//
// When projectID is set, agents whose name matches one of that project's repo
// roles (`.claude/agents/`) are flagged "(this project's role)" so the planner
// prefers the project's own harness roles.
func (h *Handler) buildPlanningRoster(ctx context.Context, squad db.Squad, projectID string) string {
	base := buildSquadRoster(ctx, h.Queries, squad)

	agents, err := h.Queries.ListAgents(ctx, squad.WorkspaceID)
	if err != nil {
		return base
	}

	// Exclude the leader + existing squad members (already shown in `base`).
	inSquad := map[string]bool{util.UUIDToString(squad.LeaderID): true}
	if members, err := h.Queries.ListSquadMembers(ctx, squad.ID); err == nil {
		for _, m := range members {
			if m.MemberType == "agent" {
				inSquad[util.UUIDToString(m.MemberID)] = true
			}
		}
	}

	// Project's own harness roles (by name), to flag/prefer them.
	projectRoles := map[string]bool{}
	if projectID != "" && h.RoleSyncService != nil {
		if pUUID, err := util.ParseUUID(projectID); err == nil {
			projectRoles = h.RoleSyncService.ProjectRoleNames(ctx, squad.WorkspaceID, pUUID)
		}
	}

	var pool []string
	for _, ag := range agents {
		if ag.ArchivedAt.Valid || !ag.RuntimeID.Valid {
			continue
		}
		if inSquad[util.UUIDToString(ag.ID)] {
			continue
		}
		row := formatRosterRowWithDesc(ag.Name, "agent", "", formatMention(ag.Name, "agent", util.UUIDToString(ag.ID)), ag.Description)
		// Prefer the project's own roles: flag them so the planner picks them
		// first, and front-load them in the list.
		if projectRoles[ag.Name] {
			row = strings.TrimRight(row, "\n") + " — (this project's role)\n"
			pool = append([]string{row}, pool...)
		} else {
			pool = append(pool, row)
		}
	}

	if len(pool) == 0 {
		return base
	}

	var sb strings.Builder
	sb.WriteString(base)
	sb.WriteString("\n\nAvailable workspace roles (assign nodes to these by their @mention — they need NOT be squad members):\n")
	for _, r := range pool {
		sb.WriteString(r)
	}
	return sb.String()
}

// renderMemberRow renders a single roster row, returning "" if the member
// can't be resolved or should be skipped (e.g. archived agent).
func renderMemberRow(ctx context.Context, q *db.Queries, m db.SquadMember) string {
	id := util.UUIDToString(m.MemberID)
	role := strings.TrimSpace(m.Role)
	switch m.MemberType {
	case "agent":
		ag, err := q.GetAgent(ctx, m.MemberID)
		if err != nil {
			return ""
		}
		if ag.ArchivedAt.Valid {
			return ""
		}
		return formatRosterRowWithDesc(ag.Name, "agent", role, formatMention(ag.Name, "agent", id), ag.Description)
	case "member":
		user, err := q.GetUser(ctx, m.MemberID)
		if err != nil {
			return ""
		}
		// Mention syntax for humans uses the user_id (matches the rest of
		// the product — see util.MentionRe and frontend mention payloads).
		userID := util.UUIDToString(m.MemberID)
		return formatRosterRow(user.Name, "member (human)", role, formatMention(user.Name, "member", userID))
	default:
		return ""
	}
}

func formatRosterRow(name, kind, role, mention string) string {
	return formatRosterRowWithDesc(name, kind, role, mention, "")
}

// formatRosterRowWithDesc is formatRosterRow plus the agent's description (its
// specialty), so a planner can match a node to a role by what the role actually
// does — not just guess from its name. The description is truncated to its first
// paragraph / ~200 runes to keep the roster compact.
func formatRosterRowWithDesc(name, kind, role, mention, desc string) string {
	var sb strings.Builder
	sb.WriteString("- ")
	sb.WriteString(name)
	sb.WriteString(" — ")
	sb.WriteString(kind)
	if role != "" {
		sb.WriteString(`, role: "`)
		sb.WriteString(role)
		sb.WriteString(`"`)
	}
	sb.WriteString(" — `")
	sb.WriteString(mention)
	sb.WriteString("`")
	if d := truncateRoleDesc(desc); d != "" {
		sb.WriteString(" — ")
		sb.WriteString(d)
	}
	sb.WriteString("\n")
	return sb.String()
}

// truncateRoleDesc reduces a role description to a single compact line for the
// roster: collapse whitespace, take the first paragraph, cap at ~200 runes. An
// empty/blank description returns "" (the row then omits the description trailer).
func truncateRoleDesc(desc string) string {
	d := strings.TrimSpace(desc)
	if d == "" {
		return ""
	}
	// First paragraph only (descriptions can be a whole Soul block).
	if i := strings.Index(d, "\n\n"); i > 0 {
		d = d[:i]
	}
	d = strings.Join(strings.Fields(d), " ") // collapse newlines/runs of spaces
	const max = 200
	if r := []rune(d); len(r) > max {
		d = strings.TrimSpace(string(r[:max])) + "…"
	}
	return d
}

// formatMention emits a mention markdown string that round-trips through
// util.ParseMentions. The label is the human display name; the link target
// uses the mention:// scheme with the entity type and UUID.
func formatMention(name, mentionType, id string) string {
	return "[@" + name + "](mention://" + mentionType + "/" + id + ")"
}
