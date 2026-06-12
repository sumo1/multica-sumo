package daemon

import (
	"strings"
	"testing"
)

// TestBuildQuickCreatePromptRules locks in the rules that govern how the
// quick-create agent is allowed to translate raw user input into the issue
// description body. Each substring corresponds to a concrete failure mode
// observed in production output:
//   - meta-instructions ("create an issue", "cc @X") leaking into the body
//   - the Context section being misused as an apology log when no external
//     references were actually fetched
//   - hard-line rules being silently dropped on prompt rewrites
func TestBuildQuickCreatePromptRules(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})

	mustContain := []string{
		// high-fidelity invariant
		"Faithfully restate what the user wants",
		"Preserve specific names, identifiers, file paths",
		// strip non-spec material: verbal routing wrappers + conversational fillers
		"verbal routing wrappers about creating the issue",
		"pure conversational fillers",
		// cc routing must survive: mention link stays in description so the
		// auto-subscribe path fires (multica issue create has no --subscriber flag)
		"CC exception",
		"auto-subscribes members",
		// context section is conditional and must not be an apology log
		"include ONLY when the input cited external resources",
		"never use it as an apology log",
		// output/reporting must be workspace-prefix agnostic. Workspaces can
		// use custom issue prefixes, so a successful issue creation should
		// not look failed merely because the identifier does not match one
		// fixed prefix.
		"multica issue create --output json",
		"JSON response",
		"identifier",
		"Do not scrape human output",
		"do not assume any workspace issue prefix",
		"Created <identifier-or-id>: <title>",
		// hard rules
		"never invent requirements",
		"never reduce multi-sentence input",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt output missing required rule: %q", s)
		}
	}
}

// TestBuildQuickCreatePromptAssigneeIncludesSquads locks in the MUL-2165
// fix: the assignee-resolution rules must tell the agent to consult the
// squad list alongside members and agents. Before this, a quick-create
// input like "assign to <SquadName>" silently fell through to
// "Unrecognized assignee" because squads were never queried.
func TestBuildQuickCreatePromptAssigneeIncludesSquads(t *testing.T) {
	out := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	mustContain := []string{
		"multica squad list",
		"Squads are first-class assignees",
		"Treat bare @-routing as an assignee directive",
		"让 @独立团 review 这个 PR",
		"pass the squad's `id` as `--assignee-id`",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt assignee block missing %q\n--- output ---\n%s", s, out)
		}
	}
}

// TestBuildQuickCreatePromptSquadDefaultsToSquad locks in the MUL-2203
// fix: when the picker was a squad, the task runs on the squad's leader
// agent, but the default assignee for issues created by this run must
// point at the SQUAD's UUID — not the leader agent's UUID. The previous
// "default to YOURSELF" instruction made squad-created issues land under
// the leader, hiding them from the squad's delegation flow.
func TestBuildQuickCreatePromptSquadDefaultsToSquad(t *testing.T) {
	const (
		squadID   = "aaaa1111-2222-3333-4444-555555555555"
		squadName = "独立团"
		leaderID  = "bbbb1111-2222-3333-4444-666666666666"
	)
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		Agent:             &AgentData{ID: leaderID, Name: "leader-agent"},
		SquadID:           squadID,
		SquadName:         squadName,
	})

	// The default-assignee instruction must point at the squad UUID.
	if !strings.Contains(out, "--assignee-id \""+squadID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must default to the squad's UUID, got:\n%s", out)
	}
	// And it must NOT tell the agent to default to itself (the leader).
	if strings.Contains(out, "--assignee-id \""+leaderID+"\"") {
		t.Errorf("buildQuickCreatePrompt with SquadID must NOT default to the leader agent's UUID, got:\n%s", out)
	}
	// The squad name should appear in the instruction so the agent has
	// human-readable context for the routing decision.
	if !strings.Contains(out, squadName) {
		t.Errorf("buildQuickCreatePrompt with SquadID should mention the squad name %q, got:\n%s", squadName, out)
	}
	// And the prompt must explicitly call out the squad-vs-leader rule
	// so the agent does not silently regress to "default to YOURSELF".
	mustContain := []string{
		"picker SQUAD",
		"running on the squad's behalf",
		"do not assign it to your own agent UUID",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with SquadID missing %q\n--- output ---\n%s", s, out)
		}
	}
}

// TestBuildQuickCreatePromptProjectPinning verifies that when the user
// pins a project in the quick-create modal, the prompt instructs the agent
// to pass `--project <uuid>` exactly. Without this, the agent would re-read
// the workspace default and silently drop the user's selection — the same
// "I have to retype 'in project X' every time" failure mode the modal
// addition was meant to fix.
func TestBuildQuickCreatePromptProjectPinning(t *testing.T) {
	const projectID = "11111111-2222-3333-4444-555555555555"
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ProjectID:         projectID,
		ProjectTitle:      "Web App",
	})
	mustContain := []string{
		"--project \"" + projectID + "\"",
		"Web App",
		"modal selection is authoritative",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with project missing %q\n--- output ---\n%s", s, out)
		}
	}

	// Without a project, the prompt must keep the legacy "omit" instruction
	// so the agent doesn't accidentally start passing --project on plain
	// quick-create runs.
	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if !strings.Contains(plain, "**project**: omit") {
		t.Errorf("buildQuickCreatePrompt without project must keep the omit instruction, got:\n%s", plain)
	}
	if strings.Contains(plain, "--project") {
		t.Errorf("buildQuickCreatePrompt without project must NOT mention --project, got:\n%s", plain)
	}
}

// TestBuildQuickCreatePromptParentPinning verifies that when the user
// opened quick-create from "Add sub issue" on an existing issue, the prompt
// instructs the agent to pass `--parent <uuid>` so the new issue is filed
// as a sub-issue. The frontend already seeds parent_issue_id silently
// through the manual→agent switch, so this is the last hop that has to
// hold up — without the prompt instruction the agent would create a
// standalone issue and the sub-issue relationship would be silently
// dropped.
func TestBuildQuickCreatePromptParentPinning(t *testing.T) {
	const (
		parentID         = "33333333-2222-1111-4444-555555555555"
		parentIdentifier = "MUL-2534"
	)
	out := buildQuickCreatePrompt(Task{
		QuickCreatePrompt:     "fix the login button color",
		ParentIssueID:         parentID,
		ParentIssueIdentifier: parentIdentifier,
	})
	mustContain := []string{
		"--parent \"" + parentID + "\"",
		parentIdentifier,
		"modal entry point is authoritative",
		"filed as a sub-issue",
	}
	for _, s := range mustContain {
		if !strings.Contains(out, s) {
			t.Errorf("buildQuickCreatePrompt with parent missing %q\n--- output ---\n%s", s, out)
		}
	}

	// When only the UUID is available (identifier lookup failed on claim),
	// the agent must still get the --parent instruction so the sub-issue
	// intent isn't silently dropped.
	uuidOnly := buildQuickCreatePrompt(Task{
		QuickCreatePrompt: "fix the login button color",
		ParentIssueID:     parentID,
	})
	if !strings.Contains(uuidOnly, "--parent \""+parentID+"\"") {
		t.Errorf("buildQuickCreatePrompt with parent UUID only must still pin --parent, got:\n%s", uuidOnly)
	}

	// Without a parent, the prompt must NOT mention --parent at all — a
	// plain quick-create run should not start filing sub-issues.
	plain := buildQuickCreatePrompt(Task{QuickCreatePrompt: "fix the login button color"})
	if strings.Contains(plain, "--parent") {
		t.Errorf("buildQuickCreatePrompt without parent must NOT mention --parent, got:\n%s", plain)
	}
}

// TestBuildPromptSquadLeaderNoActionForMemberTrigger verifies that the
// squad leader no_action prohibition is injected in the per-turn prompt
// regardless of whether the triggering comment was posted by an agent or
// a member. This was the root cause of the "LGTM is a pure acknowledgment
// — no reply needed. Exiting silently." noise comment: the prohibition
// only fired for agent-triggered comments, so member-triggered ones
// (like "LGTM") bypassed it.
func TestBuildPromptSquadLeaderNoActionForMemberTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for member-triggered comments, got:\n%s", out)
	}
	if !strings.Contains(out, "DO NOT post any comment") {
		t.Errorf("buildCommentPrompt must contain DO NOT post prohibition for member-triggered squad leader, got:\n%s", out)
	}
}

// TestBuildPromptSquadLeaderNoActionForAgentTrigger verifies the rule also
// fires for agent-triggered comments (the original path that already worked).
func TestBuildPromptSquadLeaderNoActionForAgentTrigger(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "Deploy complete.",
		TriggerAuthorType:     "agent",
		TriggerAuthorName:     "deploy-boy",
		Agent: &AgentData{
			Instructions: "Some instructions\n\n## Squad Operating Protocol\n\nYou are the LEADER...",
		},
	}
	out := BuildPrompt(task, "claude")
	if !strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must inject squad leader no_action rule for agent-triggered comments, got:\n%s", out)
	}
}

func TestBuildChatPromptSlashSkills(t *testing.T) {
	t.Run("injects selected skills block", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "please [/deploy](slash://skill/abc-123) this",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "abc-123", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if !strings.Contains(out, "Explicitly selected skills:\n- deploy\n") {
			t.Fatalf("expected selected skills block, got:\n%s", out)
		}
		if !strings.Contains(out, "User message:\nplease [/deploy](slash://skill/abc-123) this") {
			t.Fatalf("expected raw user message preserved, got:\n%s", out)
		}
	})

	t.Run("ignores skills not belonging to agent", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/hacker-skill](slash://skill/evil-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "good-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block for unknown skill ID, got:\n%s", out)
		}
	})

	t.Run("validates by ID not label", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/wrong-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "real-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("matching label with wrong ID must not pass, got:\n%s", out)
		}
	})

	t.Run("uses canonical name not label", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/spoofed-name](slash://skill/real-id)",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "real-id", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if !strings.Contains(out, "- deploy\n") {
			t.Fatalf("expected canonical name 'deploy', got:\n%s", out)
		}
		if strings.Contains(out, "- spoofed-name\n") {
			t.Fatalf("selected skills block must not use spoofed label, got:\n%s", out)
		}
		if !strings.Contains(out, "User message:\n[/spoofed-name](slash://skill/real-id)") {
			t.Fatalf("expected raw user message with spoofed label preserved, got:\n%s", out)
		}
	})

	t.Run("deduplicates skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/a) and [/deploy](slash://skill/a) again",
			Agent: &AgentData{
				Skills: []SkillData{{ID: "a", Name: "deploy"}},
			},
		}
		out := buildChatPrompt(task)
		if strings.Count(out, "- deploy") != 1 {
			t.Fatalf("expected exactly 1 '- deploy', got:\n%s", out)
		}
	})

	t.Run("omits block when no valid skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "just a normal message",
			Agent:         &AgentData{Skills: []SkillData{{ID: "a", Name: "deploy"}}},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block when no slash links, got:\n%s", out)
		}
	})

	t.Run("omits block when agent has no skills", func(t *testing.T) {
		task := Task{
			ChatSessionID: "sess-1",
			ChatMessage:   "[/deploy](slash://skill/abc-123)",
			Agent:         &AgentData{},
		}
		out := buildChatPrompt(task)
		if strings.Contains(out, "Explicitly selected skills") {
			t.Fatalf("should not inject block for agent with no skills, got:\n%s", out)
		}
	})
}

// TestBuildPromptDefaultMentionsRecent pins that the catch-all fallback
// prompt (no trigger comment, no chat, no autopilot, no quick-create) also
// teaches the agent about --recent as the long-issue-friendly alternative
// to the flat dump, even though it cannot anchor a --thread without a
// trigger comment id.
func TestBuildPromptDefaultMentionsRecent(t *testing.T) {
	out := BuildPrompt(Task{IssueID: "issue-default-1"}, "claude")
	for _, s := range []string{
		"--recent 20 --output json",
		"Next thread cursor:",
		"--since",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("default BuildPrompt missing %q\n--- output ---\n%s", s, out)
		}
	}
	// And the default path must NOT inject a --thread example, because there
	// is no trigger comment id to anchor on.
	if strings.Contains(out, "--thread") {
		t.Errorf("default BuildPrompt should NOT mention --thread (no trigger comment to anchor on)\n--- output ---\n%s", out)
	}
	// The legacy "If you need comment history" soft phrasing conflicts with
	// the assignment-trigger runtime workflow, which treats reading comments
	// as mandatory. Guard against it sneaking back in.
	if strings.Contains(out, "If you need comment history") {
		t.Errorf("default BuildPrompt still carries the legacy 'If you need' soft phrasing that conflicts with the mandatory workflow\n--- output ---\n%s", out)
	}
}

// TestBuildPromptNonSquadLeaderNoRule verifies that non-squad-leader agents
// do NOT get the squad leader no_action rule injected.
func TestBuildPromptNonSquadLeaderNoRule(t *testing.T) {
	task := Task{
		IssueID:               "issue-123",
		TriggerCommentID:      "comment-456",
		TriggerCommentContent: "LGTM",
		TriggerAuthorType:     "member",
		TriggerAuthorName:     "Bohan",
		Agent: &AgentData{
			Instructions: "Some instructions without the squad marker",
		},
	}
	out := BuildPrompt(task, "claude")
	if strings.Contains(out, "Squad leader no_action rule") {
		t.Errorf("buildCommentPrompt must NOT inject squad leader no_action rule for non-squad-leader agents, got:\n%s", out)
	}
}

// TestBuildPromptNewCommentsHint pins that a comment-triggered task whose agent
// ran before on this issue (NewCommentsSince set, NewCommentCount > 0) gets the
// since-delta hint with the ISSUE-WIDE new-comment count, but is steered to read
// the triggering (parent) thread first rather than blindly pulling every new
// comment.
func TestBuildPromptNewCommentsHint(t *testing.T) {
	const (
		issueID = "issue-new-1"
		since   = "2026-05-28T11:00:00Z"
	)
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerCommentContent: "please look",
		TriggerAuthorType:     "member",
		NewCommentCount:       3,
		NewCommentsSince:      since,
	}
	out := BuildPrompt(task, "claude")

	// Issue-wide count (reverted from the thread-scoped wording).
	if !strings.Contains(out, "3 new comment(s) on this issue since your last run") {
		t.Errorf("hint must report the issue-wide new-comment count, got:\n%s", out)
	}
	// Don't-blindly-read-all guidance.
	if !strings.Contains(out, "blindly") {
		t.Errorf("hint must discourage blindly reading every new comment, got:\n%s", out)
	}
	// Parent thread first: the --thread <trigger> read is the prioritized action.
	if !strings.Contains(out, "multica issue comment list "+issueID+" --thread trigger-1 --since "+since+" --output json") {
		t.Errorf("hint must point at the triggering (parent) thread --since read first, got:\n%s", out)
	}
	if !strings.Contains(out, "--tail 30") {
		t.Errorf("hint must offer the full-thread (--tail 30) option, got:\n%s", out)
	}
	// Issue-wide catch-up is demoted to an only-if-needed fallback.
	if !strings.Contains(out, "multica issue comment list "+issueID+" --since "+since+" --output json") {
		t.Errorf("hint must keep the issue-wide --since catch-up as a fallback, got:\n%s", out)
	}
	// The old cursor-heavy paragraph must be gone.
	if strings.Contains(out, "Next reply cursor") || strings.Contains(out, "--before-id") {
		t.Errorf("the old cursor-pagination paragraph must not render, got:\n%s", out)
	}
}

// TestBuildPromptColdStartThreadRead pins the cold-start case: no prior run means
// no since anchor (NewCommentsSince empty), so we suppress the delta hint and
// instead point the agent at the triggering CONVERSATION (--thread <trigger>
// --tail 30) rather than dumping the flat timeline.
func TestBuildPromptColdStartThreadRead(t *testing.T) {
	const issueID = "issue-cold-1"
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerCommentContent: "hi",
		TriggerAuthorType:     "member",
		NewCommentCount:       0,
		NewCommentsSince:      "",
	}
	out := BuildPrompt(task, "claude")
	if strings.Contains(out, "new comment(s) since your last run") {
		t.Errorf("no since-delta hint should render on cold start, got:\n%s", out)
	}
	if !strings.Contains(out, "multica issue comment list "+issueID+" --thread trigger-1 --tail 30 --output json") {
		t.Errorf("cold start must point at the triggering thread read, got:\n%s", out)
	}
}

// TestBuildPromptResumedNoDeltaDoesNotForceThreadRead pins the warm/no-delta
// path: when a prior provider session is actually being resumed, the triggering
// comment is already embedded in the per-turn prompt, so the agent should not
// be told to re-read the triggering thread's latest 30 replies by default.
func TestBuildPromptResumedNoDeltaDoesNotForceThreadRead(t *testing.T) {
	const issueID = "issue-resumed-1"
	task := Task{
		IssueID:               issueID,
		TriggerCommentID:      "trigger-1",
		TriggerCommentContent: "hi again",
		TriggerAuthorType:     "member",
		PriorSessionID:        "session-123",
		NewCommentCount:       0,
		NewCommentsSince:      "",
	}
	out := BuildPrompt(task, "claude")

	for _, want := range []string{
		"triggering comment is already included above",
		"No other new comments on this issue since your last run",
		"triggering comment ID / thread anchor",
		"If your reply depends on thread context",
		"do not rely only on resumed session memory",
		"multica issue comment list " + issueID + " --thread trigger-1 --tail 30 --output json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("resumed/no-delta prompt missing %q\n--- output ---\n%s", want, out)
		}
	}
	// The stale thread-scoped wording (since-delta used to be thread-scoped)
	// must not reappear.
	if strings.Contains(out, "scoped to the triggering thread") {
		t.Errorf("resumed/no-delta prompt must not claim the delta is thread-scoped, got:\n%s", out)
	}
	if strings.Contains(out, "Read the triggering conversation first") {
		t.Errorf("resumed/no-delta prompt must not use the cold-start forced-read wording, got:\n%s", out)
	}
}

// TestBuildGoalSubtaskPrompt locks in that a goal-mode subtask prompt carries
// the overall goal (for context), the subtask title + spec (the actual work),
// and instructs the role to stay in scope. There is no issue to fetch.
func TestBuildGoalSubtaskPrompt(t *testing.T) {
	out := buildGoalSubtaskPrompt(Task{
		GoalSubtaskID:    "sub-1",
		GoalTitle:        "Refactor login module",
		GoalSubtaskTitle: "Build backend API",
		GoalSubtaskSpec:  "Implement POST /auth/login returning a JWT",
	})

	mustContain := []string{
		"Refactor login module",                      // overall goal context
		"Build backend API",                          // subtask title
		"Implement POST /auth/login returning a JWT", // spec body
		"Stay within its scope",                      // scope discipline
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("goal subtask prompt missing %q\n---\n%s", want, out)
		}
	}
	// Must NOT instruct the agent to fetch an issue — there isn't one.
	if strings.Contains(out, "multica issue get") {
		t.Errorf("goal subtask prompt must not reference issue fetch:\n%s", out)
	}
}

// TestBuildGoalPlanningPrompt locks in that the planning prompt tells the
// leader to decompose, assign roles, declare dependencies, and submit via the
// exact CLI command with the goal run id — and NOT to execute the work itself.
func TestBuildGoalPlanningPrompt(t *testing.T) {
	out := buildGoalPlanningPrompt(Task{
		GoalPlanningRunID: "goal-run-42",
		GoalTitle:         "Refactor login",
		GoalPlanningGoal:  "Refactor the whole login module",
	})

	mustContain := []string{
		"Refactor the whole login module",          // the goal body
		"Squad Roster",                             // points at the injected roster
		"depends_on",                               // dependency declaration
		"Keep workflow topology internal",          // worker specs must not expose graph shape
		"do NOT refer to `seq1`",                   // no seq leakage in role-facing specs
		"depend on the producers AND the verifier", // synthesis nodes receive all needed inputs
		"multica goal plan goal-run-42",            // exact submit command w/ run id
		"--subtasks-stdin",                         // stdin contract
		"Do not execute the nodes yourself",        // planner must not do the work
		"\"kind\":\"verify\"",                      // workflow design offers verify nodes
		"adversarially review",                     // adversarial-verification guidance
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("goal planning prompt missing %q\n---\n%s", want, out)
		}
	}
}

// TestBuildGoalPlanningPromptDialectAlignment locks the contract-dialect guidance:
// every planning prompt asks for dual-contract specs, and WHEN the goal is bound
// to a project the planner is told to match THAT project's existing contract
// dialect (not a fixed template). Without a project, no dialect line appears.
func TestBuildGoalPlanningPromptDialectAlignment(t *testing.T) {
	withProject := buildGoalPlanningPrompt(Task{
		GoalPlanningRunID: "gr-1",
		GoalPlanningGoal:  "ship it",
		ProjectID:         "proj-1",
	})
	for _, want := range []string{
		"DUAL CONTRACT", // dual-contract spec requirement (always)
		"acceptance",    // acceptance part
		"Match THIS project's own contract dialect", // dialect alignment (project-bound)
		"do NOT impose a fixed template",
		"docs/task/*/plan", // points at the project's existing contracts
	} {
		if !strings.Contains(withProject, want) {
			t.Errorf("project-bound planning prompt missing %q", want)
		}
	}

	noProject := buildGoalPlanningPrompt(Task{
		GoalPlanningRunID: "gr-2",
		GoalPlanningGoal:  "ship it",
	})
	if !strings.Contains(noProject, "DUAL CONTRACT") {
		t.Error("planning prompt should always ask for dual-contract specs")
	}
	if strings.Contains(noProject, "Match THIS project's own contract dialect") {
		t.Error("without a project, the planning prompt must not reference project dialect")
	}
}

// TestBuildGoalPersistPrompt locks in the repo-persist (持久化到工程) prompt: it
// routes via GoalPersistRunID, writes the snapshot under the slug dir, transcribes
// the already-dual-contract specs (does not re-derive), and — crucially for a
// GENERIC tool — tells the agent to MATCH THE PROJECT'S OWN dialect rather than
// impose a fixed template. Hard-bounds it to docs/task (no source edits/commit).
func TestBuildGoalPersistPrompt(t *testing.T) {
	out := BuildPrompt(Task{
		GoalPersistRunID:   "goal-run-99",
		GoalTitle:          "贪吃蛇 Evolution",
		GoalPersistGoal:    "Build a playable snake game",
		GoalPersistSlug:    "260611-tan-chi-she-evolution",
		GoalPersistOutcome: "completed",
		GoalPersistDigest:  "### [completed] Engine\nResult: core loop done\n",
	}, "claude")

	mustContain := []string{
		"docs/task/260611-tan-chi-she-evolution/", // snapshot dir at the slug
		"progress.md",              // overview file
		"plan/step-",               // per-subtask files
		"match this project's own", // dialect-alignment, not a fixed template
		"do NOT impose a fixed template",
		"transcribe it faithfully", // specs are already dual contracts
		"OVERWRITE",                // snapshot semantics (repeatable)
		"Do NOT commit or push",    // leave it in the working tree
		"core loop done",           // the digest is embedded
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("goal persist prompt missing %q\n---\n%s", want, out)
		}
	}

	// A generic tool must NOT hardcode one project's contract section names.
	for _, banned := range []string{"施工契约", "验收契约"} {
		if strings.Contains(out, banned) {
			t.Errorf("goal persist prompt must not hardcode a project-specific contract heading %q", banned)
		}
	}
}

// TestBuildGoalSubtaskPromptUpstreamOutput locks the handoff: an execute node
// with upstream deps must receive the upstream OUTPUT in its prompt, framed as
// "build on it, do NOT redo / re-derive". A root node (no upstream) must not.
func TestBuildGoalSubtaskPromptUpstreamOutput(t *testing.T) {
	withUpstream := buildGoalSubtaskPrompt(Task{
		GoalSubtaskTitle:   "Generate names",
		GoalSubtaskSpec:    "produce candidates",
		GoalSubtaskKind:    "execute",
		GoalUpstreamOutput: "### Analyze essence\nResult: a cross-project multi-agent tool\n",
		GoalHandoffBrief:   "Task objective: Generate names\nThe coordinator has embedded the source material this task needs. Use it directly; do not read or create an intermediate handoff file.",
	})
	for _, want := range []string{
		"Task inputs",                      // framed as semantic task input
		"Task objective: Generate names",   // current objective is injected
		"Source material",                  // material is an explicit prompt section
		"intermediate handoff file",        // no file handoff dependency
		"a cross-project multi-agent tool", // the actual upstream result is embedded
		"Analyze essence",                  // upstream node title
	} {
		if !strings.Contains(withUpstream, want) {
			t.Errorf("upstream prompt missing %q\n---\n%s", want, withUpstream)
		}
	}
	for _, banned := range []string{
		"Runtime handoff",
		"Upstream input",
		"upstream",
	} {
		if strings.Contains(withUpstream, banned) {
			t.Errorf("delegated task prompt should hide workflow-topology wording %q\n---\n%s", banned, withUpstream)
		}
	}

	root := buildGoalSubtaskPrompt(Task{
		GoalSubtaskTitle: "Analyze essence",
		GoalSubtaskSpec:  "distill",
		GoalSubtaskKind:  "execute",
	})
	if strings.Contains(root, "Source material") || strings.Contains(root, "Task inputs") {
		t.Errorf("root node prompt must not reference upstream output:\n%s", root)
	}
}

// TestBuildGoalDecisionPrompt locks in the 下一步判断 prompt: it routes via
// GoalDecisionSubtaskID, frames the coordinator as a judge (not a re-doer), and
// ends in the proceed/reshape/abort CLI contract bound to the failed node's id.
func TestBuildGoalDecisionPrompt(t *testing.T) {
	out := BuildPrompt(Task{
		GoalDecisionSubtaskID:     "failed-node-7",
		GoalTitle:                 "Ship onboarding",
		GoalDecisionSubtaskTitle:  "Backend API",
		GoalDecisionSubtaskSpec:   "implement POST /auth/login",
		GoalDecisionFailureReason: "compile error in handler",
		GoalDecisionDownstream:    "- Frontend\n- Security review",
	}, "claude")

	mustContain := []string{
		"compile error in handler",                  // the failure reason
		"do NOT redo the failed work",               // judge, not re-doer
		"multica goal decide failed-node-7 proceed", // proceed contract
		"multica goal decide failed-node-7 reshape", // reshape contract
		"multica goal decide failed-node-7 abort",   // abort contract
		"Frontend", // downstream blast radius
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("goal decision prompt missing %q\n---\n%s", want, out)
		}
	}
}

// TestBuildGoalVerifyPrompt locks in that a verify node's prompt is adversarial,
// carries the review target, and ends in the pass/reject verdict CLI contract.
func TestBuildGoalVerifyPrompt(t *testing.T) {
	out := buildGoalSubtaskPrompt(Task{
		GoalSubtaskID:    "verify-7",
		GoalSubtaskKind:  "verify",
		GoalTitle:        "Ship login",
		GoalSubtaskSpec:  "Check the API for auth flaws",
		GoalReviewTarget: "### Backend API\nResult: implemented POST /auth/login",
	})

	mustContain := []string{
		"adversarial",                          // adversarial framing
		"Check the API for auth flaws",         // the review criteria
		"implemented POST /auth/login",         // the work product to review
		"multica goal verdict verify-7 pass",   // pass contract
		"multica goal verdict verify-7 reject", // reject contract
		"Default to reject",                    // skeptical default
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("goal verify prompt missing %q\n---\n%s", want, out)
		}
	}
	// A verify prompt must NOT tell the agent to just "complete the subtask".
	if strings.Contains(out, "Complete this subtask end to end") {
		t.Errorf("verify prompt should not use the execute-node body:\n%s", out)
	}
}

// TestBuildChatPromptTakeoverContext locks in that a takeover chat (a chat
// session bound to a failed goal subtask) injects the subtask title, original
// spec, and failure reason so the agent enters the conversation oriented.
func TestBuildChatPromptTakeoverContext(t *testing.T) {
	out := buildChatPrompt(Task{
		ChatSessionID:         "cs-1",
		ChatMessage:           "let's fix the login bug together",
		TakeoverSubtaskTitle:  "Backend API",
		TakeoverSubtaskSpec:   "Implement POST /auth/login",
		TakeoverFailureReason: "lint error: unused import",
	})

	mustContain := []string{
		"Takeover context",                 // the orienting header
		"Backend API",                      // subtask title
		"Implement POST /auth/login",       // original spec
		"lint error: unused import",        // why it failed
		"let's fix the login bug together", // the user's actual message still present
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("takeover chat prompt missing %q\n---\n%s", want, out)
		}
	}

	// A normal chat (no takeover fields) must NOT show the takeover header.
	plain := buildChatPrompt(Task{ChatSessionID: "cs-2", ChatMessage: "hi"})
	if strings.Contains(plain, "Takeover context") {
		t.Errorf("plain chat prompt should not carry takeover context:\n%s", plain)
	}
}

// TestBuildChatPromptDiscussionFacilitation locks in that a task-mode discussion
// chat frames the agent as the 总控 facilitator — guide the user to clarify the
// goal into a confirmable task card, ask clarifying questions, and explicitly NOT
// start the work or self-trigger planning. A non-discussion chat stays plain.
func TestBuildChatPromptDiscussionFacilitation(t *testing.T) {
	out := buildChatPrompt(Task{
		ChatSessionID:        "cs-1",
		ChatMessage:          "I want to add dark mode",
		GoalDiscussionActive: true,
		GoalDiscussionTitle:  "Dark mode",
		GoalDiscussionGoal:   "Add a dark theme toggle",
	})

	mustContain := []string{
		"总控",                          // framed as coordinator
		"discussion phase",            // it's the discussion, not execution
		"NOT to start doing the work", // don't jump to execution
		"clarifying questions",        // facilitate
		"task card",                   // converge to a confirmable goal
		"Confirm & execute",           // confirmation is the user's action
		"Dark mode",                   // working title surfaced
		"Add a dark theme toggle",     // goal-so-far surfaced
		"I want to add dark mode",     // user's message still present
	}
	for _, want := range mustContain {
		if !strings.Contains(out, want) {
			t.Errorf("discussion chat prompt missing %q\n---\n%s", want, out)
		}
	}

	// A normal chat must NOT carry the discussion facilitation framing.
	plain := buildChatPrompt(Task{ChatSessionID: "cs-2", ChatMessage: "hi"})
	if strings.Contains(plain, "总控 (coordinator) in a task discussion") {
		t.Errorf("plain chat prompt should not carry discussion facilitation:\n%s", plain)
	}
}
