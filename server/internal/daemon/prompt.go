package daemon

import (
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

// BuildPrompt constructs the task prompt for an agent CLI.
// Keep this minimal — detailed instructions live in CLAUDE.md / AGENTS.md
// injected by execenv.InjectRuntimeConfig. The provider string is used by
// comment-triggered tasks: Codex's per-turn reply template needs the
// platform-aware "stdin or file" variant, every other provider gets a
// lightweight inline template (or Windows file for any provider on
// Windows).
func BuildPrompt(task Task, provider string) string {
	if task.ChatSessionID != "" {
		return buildChatPrompt(task)
	}
	if task.TriggerCommentID != "" {
		return buildCommentPrompt(task, provider)
	}
	if task.AutopilotRunID != "" {
		return buildAutopilotPrompt(task)
	}
	if task.GoalPlanningRunID != "" {
		return buildGoalPlanningPrompt(task)
	}
	if task.GoalSummaryRunID != "" {
		return buildGoalSummaryPrompt(task)
	}
	if task.GoalPersistRunID != "" {
		return buildGoalPersistPrompt(task)
	}
	if task.GoalDecisionSubtaskID != "" {
		return buildGoalDecisionPrompt(task)
	}
	if task.GoalSubtaskID != "" {
		return buildGoalSubtaskPrompt(task)
	}
	if task.QuickCreatePrompt != "" {
		return buildQuickCreatePrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then complete it.\n", task.IssueID)
	fmt.Fprintf(&b, "For comment history, follow the rule in your runtime workflow file (assignment-triggered tasks treat the read as mandatory). `multica issue comment list %s --output json` returns all comments for the issue (server caps at 2000). On long-running issues use `--recent 20 --output json` to read the 20 most recently active threads, then page older threads via the stderr `Next thread cursor: ...` line and the matching `--before` / `--before-id` until you have enough history. `--since <RFC3339>` is still available for incremental polling and may combine with `--recent`.\n", task.IssueID)
	return b.String()
}

// buildGoalPlanningPrompt constructs the prompt for the squad leader (PMO) to
// decompose a goal into a subtask DAG. The squad roster (member names, roles,
// and agent UUIDs) is injected into the agent's Instructions by the claim
// handler, so here we only state the task + the exact CLI write-back contract.
// The leader must emit a JSON plan and submit it via `multica goal plan`.
func buildGoalPlanningPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are the PMO (squad leader) for a Multica workspace. Decompose the goal below into an executable plan of subtasks, assign each to a role agent from your squad roster, and declare dependencies between subtasks.\n\n")
	if task.GoalTitle != "" {
		fmt.Fprintf(&b, "Goal title: %s\n\n", task.GoalTitle)
	}
	b.WriteString("Goal:\n")
	fmt.Fprintf(&b, "%s\n\n", task.GoalPlanningGoal)

	b.WriteString("Design a workflow, not just a flat task list. Based on the goal and the roles available to you (your Squad Roster AND the Available workspace roles pool), decide the shape:\n")
	b.WriteString("- Break the goal into the smallest set of nodes that fully covers it. Prefer 2–6 execute nodes.\n")
	b.WriteString("- Assign each node to ONE role agent by its UUID — choose from your Squad Roster OR the Available workspace roles pool (pool agents do NOT need to be squad members; you can assign work to them directly). Each roster entry carries the role's description — READ IT and match the node to the role whose described specialty fits best (don't pick by name alone; e.g. send implementation to a coder, a verify node to a reviewer/evaluator). Prefer roles flagged as this project's own. Don't pile every node onto yourself when a better-suited role exists; use yourself only when no listed role fits.\n")
	b.WriteString("- Declare dependencies with `depends_on` (the `seq` numbers that must finish first). Independent nodes run in parallel; chain nodes that build on each other.\n")
	b.WriteString("- Each node `spec` is the full instruction its role receives — write it so the role can act end to end without seeing the other nodes.\n")
	b.WriteString("- Keep workflow topology internal. In a node `spec`, do NOT refer to `seq1`, `seq2`, node numbers, dependency edges, upstream/downstream nodes, or \"previous/next node\". Use semantic input names such as \"core mechanism explanation\", \"accepted API contract\", or \"review constraints\".\n")
	b.WriteString("- If a node must consume a prior work product, declare that producer in `depends_on`. If a synthesis node needs both producer outputs and a review verdict, depend on the producers AND the verifier; do not hide required source material behind a verifier-only dependency.\n\n")

	b.WriteString("Write each node `spec` as a DUAL CONTRACT — a construction part (what to build: scope, files in/out, expected output, constraints) AND an acceptance part (how to verify it's done: concrete, machine-checkable criteria). Pair them: every construction output should have a matching acceptance check. If you cannot state how a node is verified, its construction part is underspecified — tighten it.\n")
	if task.ProjectID != "" {
		b.WriteString("Match THIS project's own contract dialect — do NOT impose a fixed template. You are running inside the project repository: first look at its existing task contracts (e.g. `docs/task/*/plan/*.md`) and reuse that project's section names, structure, and language when writing each spec. Only fall back to the generic construction/acceptance shape when the project has no existing contracts to mirror.\n")
	}
	b.WriteString("\n")

	b.WriteString("Node kinds — insert adversarial verification where quality matters:\n")
	b.WriteString("- `\"kind\":\"execute\"` (default): does the work.\n")
	b.WriteString("- `\"kind\":\"verify\"`: adversarially reviews the work products it receives through `depends_on` and returns a pass/reject verdict. Use a DIFFERENT role than the one being reviewed (independent perspective). Its `spec` should name the semantic review criteria, not node numbers. A reject bounces the reviewed node back for another attempt, then re-verifies. Add verify nodes for high-stakes or error-prone work; skip them for trivial steps to save cost.\n\n")

	b.WriteString("Submit the plan with this exact command (pass the JSON array on stdin):\n\n")
	fmt.Fprintf(&b, "  echo '<json>' | multica goal plan %s --subtasks-stdin\n\n", task.GoalPlanningRunID)
	b.WriteString("Each node: {\"seq\": <int>, \"title\": \"...\", \"spec\": \"...\", \"assignee_agent_id\": \"<uuid>\", \"depends_on\": [<seq>...], \"kind\": \"execute\"|\"verify\"}. Omit `kind` for execute.\n")
	b.WriteString("Example with adversarial verification (the final node depends on both the producer and the verifier because it needs the API contract plus review constraints):\n")
	b.WriteString("  [{\"seq\":1,\"title\":\"Backend API\",\"spec\":\"Implement POST /auth/login and report the accepted request/response contract\",\"assignee_agent_id\":\"<coder-uuid>\",\"depends_on\":[]},{\"seq\":2,\"title\":\"Security review\",\"spec\":\"Adversarially review the provided login API work product for authentication flaws and report a pass/reject verdict with any constraints\",\"assignee_agent_id\":\"<reviewer-uuid>\",\"depends_on\":[1],\"kind\":\"verify\"},{\"seq\":3,\"title\":\"Frontend\",\"spec\":\"Wire the login form using the provided accepted API contract and review constraints\",\"assignee_agent_id\":\"<coder-uuid>\",\"depends_on\":[1,2]}]\n\n")
	b.WriteString("Do not execute the nodes yourself — submitting the plan dispatches them automatically. Submit exactly one plan, then stop.\n")
	return b.String()
}

// buildGoalSummaryPrompt constructs the PMO's 收口/汇总 prompt: all subtasks are
// terminal, and the leader synthesizes their outputs into the final deliverable.
// Unlike planning, there is no CLI write-back — the agent's reply IS the result,
// streamed into the summary task's transcript (the tail of the main session ④).
func buildGoalSummaryPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are the PMO (squad leader) for a Multica workspace. Your squad has finished executing a goal you planned. Write the final deliverable for the user by synthesizing the subtask outputs below.\n\n")
	if task.GoalTitle != "" {
		fmt.Fprintf(&b, "Goal title: %s\n", task.GoalTitle)
	}
	if task.GoalSummaryGoal != "" {
		fmt.Fprintf(&b, "Original goal: %s\n", task.GoalSummaryGoal)
	}
	if task.GoalSummaryOutcome != "" {
		fmt.Fprintf(&b, "Execution outcome: %s\n", task.GoalSummaryOutcome)
	}
	b.WriteString("\nSubtask outputs:\n")
	fmt.Fprintf(&b, "%s\n\n", task.GoalSummaryDigest)

	b.WriteString("Write the final answer the user asked for — the actual deliverable, not a status report. Synthesize across the subtasks: resolve overlaps, pick the best result where they differ, and present it directly. If the outcome was partial or failed, state clearly what was achieved and what is missing.\n\n")
	b.WriteString("Do NOT run any CLI command or re-do the subtasks — they are already done. Your written reply is the final result; produce it and stop.\n")
	return b.String()
}

// buildGoalPersistPrompt constructs the 总控's repo-persist prompt: snapshot the
// task's content into the project repo following the dev-roleplay-harness task
// structure. The agent runs INSIDE the project's local_directory (the daemon
// sets the work dir from the local_directory resource), so it authors files with
// relative paths under docs/task/{slug}/. There is no CLI write-back — the files
// ARE the deliverable. Re-running overwrites the same slug dir (snapshot).
func buildGoalPersistPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are the 总控 (squad leader / coordinator) for a Multica workspace. Persist this task's content into the current project repository, following the dev-roleplay-harness task structure. The platform database remains the source of truth; you are writing an on-demand SNAPSHOT so any tool that opens this repo can pick the task up.\n\n")
	if task.GoalTitle != "" {
		fmt.Fprintf(&b, "Goal title: %s\n", task.GoalTitle)
	}
	if task.GoalPersistGoal != "" {
		fmt.Fprintf(&b, "Goal:\n%s\n", task.GoalPersistGoal)
	}
	if task.GoalPersistOutcome != "" {
		fmt.Fprintf(&b, "Current status: %s\n", task.GoalPersistOutcome)
	}
	b.WriteString("\nSubtask content (the plan + each node's spec/result):\n")
	fmt.Fprintf(&b, "%s\n\n", task.GoalPersistDigest)

	b.WriteString("You are running inside the project repository's working directory.\n\n")
	b.WriteString("FIRST, match this project's own task-doc dialect — do NOT impose a fixed template. Look at the project's existing task docs (e.g. other `docs/task/*/` directories: their `progress.md`, their `plan/step-*.md` section names and structure). Reuse that project's conventions, section headings, and language. Only fall back to the generic shape described below when the project has no prior task docs to mirror.\n\n")
	fmt.Fprintf(&b, "Write the snapshot under `docs/task/%s/` (create parent dirs as needed; relative paths):\n\n", task.GoalPersistSlug)
	b.WriteString("1. `progress.md` — the task overview: the goal, a milestone checklist (one line per subtask, checked when completed), and a record of any non-obvious decisions. Use whatever section names this project already uses for the equivalent.\n")
	b.WriteString("2. `plan/step-*.md` — one file PER subtask. Each node's spec was ALREADY written as a dual contract (a construction part + an acceptance part) — transcribe it faithfully into the file; do NOT re-derive or invent new criteria. If a verify node reviewed the subtask, fold its findings into the acceptance part. Name the sections to match the project's existing step files.\n\n")
	b.WriteString("Generic fallback shape (only when the project has no existing task docs): `progress.md` with a goal + steps checklist + decisions; `plan/step-{NN}-{slug}.md` per subtask with a construction section (scope / files / output / constraints) paired with an acceptance section (machine-checkable criteria).\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- This is a SNAPSHOT: if the files already exist, OVERWRITE them with the current state. Do not create a second dated directory.\n")
	b.WriteString("- Write only under `docs/task/` — do NOT modify source code, run builds, or touch anything else.\n")
	b.WriteString("- Do NOT commit or push. Leave the files in the working tree; the user decides when to commit.\n")
	b.WriteString("- Never write credentials, tokens, or secrets into any file.\n")
	b.WriteString("- When done, print a one-line summary of the files you wrote, then stop.\n")
	return b.String()
}

// buildGoalDecisionPrompt constructs the 总控's 下一步判断 (next-step judgment)
// prompt: a node it planned has failed, and downstream work depends on it. The
// coordinator must inspect what the failed node produced and decide how to
// proceed — NOT redo the work. The verdict is reported via the CLI and drives
// the DAG (proceed/reshape/abort). The coordinator passes a DECISION, not data.
func buildGoalDecisionPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are the 总控 (squad leader / coordinator) for a Multica workspace. A subtask you planned has FAILED, and other subtasks depend on it. Decide the next step — do NOT redo the failed work yourself.\n\n")
	if task.GoalTitle != "" {
		fmt.Fprintf(&b, "Overall goal: %s\n\n", task.GoalTitle)
	}
	if task.GoalDecisionSubtaskTitle != "" {
		fmt.Fprintf(&b, "Failed subtask: %s\n", task.GoalDecisionSubtaskTitle)
	}
	if task.GoalDecisionSubtaskSpec != "" {
		fmt.Fprintf(&b, "Its spec:\n%s\n", task.GoalDecisionSubtaskSpec)
	}
	if task.GoalDecisionFailureReason != "" {
		fmt.Fprintf(&b, "Why it failed: %s\n", task.GoalDecisionFailureReason)
	}
	if task.GoalDecisionDownstream != "" {
		b.WriteString("\nDownstream subtasks blocked behind it:\n")
		fmt.Fprintf(&b, "%s\n", task.GoalDecisionDownstream)
	}
	b.WriteString("\nInspect the actual artifacts (files, diffs, logs) the failed subtask left behind to understand whether its failure is fatal to the downstream or not. Then choose ONE next step and report it with this exact command:\n\n")
	fmt.Fprintf(&b, "  multica goal decide %s proceed                          # the failure is non-fatal; skip it and let downstream run\n", task.GoalDecisionSubtaskID)
	fmt.Fprintf(&b, "  multica goal decide %s reshape --spec \"<new spec>\"      # the approach was wrong; rewrite the spec and retry the node\n", task.GoalDecisionSubtaskID)
	fmt.Fprintf(&b, "  multica goal decide %s abort                            # the failure blocks everything downstream; stop this branch\n\n", task.GoalDecisionSubtaskID)
	b.WriteString("Choose `proceed` only when the downstream genuinely does not need this node's output. Choose `reshape` when a different approach could succeed (the spec you pass replaces the failed one). Choose `abort` when the goal cannot meaningfully continue past this failure. Report exactly one decision, then stop.\n")
	return b.String()
}

// buildGoalSubtaskPrompt constructs a prompt for a goal-mode subtask. The PMO
// (squad leader) decomposed a goal into this node and assigned it to a role
// agent. There is no issue to fetch — the spec IS the task. Verify nodes get a
// distinct, adversarial prompt that ends in a pass/reject verdict.
func buildGoalSubtaskPrompt(task Task) string {
	if task.GoalSubtaskKind == "verify" {
		return buildGoalVerifyPrompt(task)
	}
	var b strings.Builder
	b.WriteString("You are a role agent executing one delegated task in a Multica workspace.\n\n")
	if task.GoalTitle != "" {
		fmt.Fprintf(&b, "Overall goal: %s\n\n", task.GoalTitle)
	}
	if task.GoalSubtaskTitle != "" {
		fmt.Fprintf(&b, "Your task: %s\n\n", task.GoalSubtaskTitle)
	}
	b.WriteString("Task contract:\n")
	fmt.Fprintf(&b, "%s\n\n", task.GoalSubtaskSpec)
	// Source material: what the coordinator selected for this task. The context
	// field is still named upstream_output for wire compatibility, but the prompt
	// presents it as task inputs so the executor does not need to reason about
	// the DAG topology.
	if task.GoalUpstreamOutput != "" {
		b.WriteString("Task inputs:\n")
		if task.GoalHandoffBrief != "" {
			fmt.Fprintf(&b, "%s\n\n", task.GoalHandoffBrief)
		} else {
			b.WriteString("Use the source material below as this task's input. Build on it directly; do not redo prior work or re-derive established findings.\n\n")
		}
		b.WriteString("Source material:\n")
		fmt.Fprintf(&b, "%s\n\n", task.GoalUpstreamOutput)
	}
	if task.GoalSubtaskKind == "" || task.GoalSubtaskKind == "execute" {
		if task.ProjectID != "" {
			b.WriteString("Your spec includes acceptance criteria — treat them as the definition of done. You are running inside the project repository: follow the project's existing conventions and contracts (look at `docs/task/*/plan/*.md` and the codebase) rather than inventing your own. Make your output satisfy the acceptance part of the spec.\n")
		} else {
			b.WriteString("If your spec includes acceptance criteria, treat them as the definition of done and make your output satisfy them.\n")
		}
	}
	b.WriteString("Complete this task end to end. Stay within its scope — other assigned work is handled separately. When done, summarize what you produced.\n")
	return b.String()
}

// buildGoalVerifyPrompt constructs the prompt for a verify node: adversarially
// review the upstream work product and report a pass/reject verdict via the
// CLI. The verdict drives the workflow — reject bounces the reviewed node back.
func buildGoalVerifyPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are an adversarial reviewer in a Multica goal workflow. Your job is to critically verify another agent's work, NOT to redo it.\n\n")
	if task.GoalTitle != "" {
		fmt.Fprintf(&b, "Overall goal: %s\n\n", task.GoalTitle)
	}
	b.WriteString("Review criteria:\n")
	fmt.Fprintf(&b, "%s\n\n", task.GoalSubtaskSpec)
	if task.GoalReviewTarget != "" {
		b.WriteString("Work product to review:\n")
		fmt.Fprintf(&b, "%s\n\n", task.GoalReviewTarget)
	}
	b.WriteString("Inspect the actual artifacts (files, diffs, outputs) the reviewed work produced — don't rely only on its self-report. Be skeptical: look for missed requirements, edge cases, and quality gaps.\n\n")
	b.WriteString("Report your verdict with this exact command, then stop:\n\n")
	fmt.Fprintf(&b, "  multica goal verdict %s pass    # work meets the criteria\n", task.GoalSubtaskID)
	fmt.Fprintf(&b, "  multica goal verdict %s reject --reason \"<what is wrong>\"   # send it back for rework\n\n", task.GoalSubtaskID)
	b.WriteString("Default to reject when the work is incomplete or you are unsure it meets the criteria. A reject must include a concrete, actionable reason so the reviewed agent can fix it.\n")
	return b.String()
}

// buildQuickCreatePrompt constructs a prompt for quick-create tasks. The
// user typed a single natural-language sentence in the create-issue modal;
// the agent's job is to translate it into one `multica issue create` CLI
// invocation, using its judgment to decide whether fetching referenced URLs
// would produce a better issue. No issue exists yet, so the agent must NOT
// call `multica issue get` or attempt to comment — there's nothing to read
// or reply to.
func buildQuickCreatePrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a quick-create assistant for a Multica workspace.\n\n")
	b.WriteString("A user captured the following input via the quick-create modal. There is NO existing issue. Your job is to create a well-formed issue from this input with a single `multica issue create` command.\n\n")
	fmt.Fprintf(&b, "User input:\n> %s\n\n", task.QuickCreatePrompt)

	b.WriteString("Field rules:\n\n")

	// title
	b.WriteString("- **title**: required. A concise but semantically rich summary. If the input references external resources (PRs, issues, URLs), use your judgment on whether fetching the resource would produce a meaningfully better title — e.g. \"review PR #123\" → \"Review PR #123: Refactor auth module to OAuth2\". Strip filler words but preserve key semantic information.\n\n")

	// description — the core optimization
	b.WriteString("- **description**: The description is the executing agent's primary context. Aim for high fidelity — they should grasp the user's intent as if they had read the raw input themselves. Use a two-section structure:\n\n")
	b.WriteString("  1. **User request** — Faithfully restate what the user wants in their own words. Preserve specific names, identifiers, file paths, code snippets, and technical terms verbatim. Strip non-spec material before writing it (this is removal, not paraphrasing): verbal routing wrappers about creating the issue or routing it (e.g. \"create an issue\", \"分配给 X\", \"让 @X 处理\") and pure conversational fillers (e.g. \"对吧？\"). When in doubt, keep it.\n\n")
	b.WriteString("     CC exception: `multica issue create` has no `--subscriber` flag, and the platform auto-subscribes members whose `[@Name](mention://member/<uuid>)` link appears in the description. When the user wrote \"cc @Y\", strip the verbal \"cc\" wrapper from the User request body and append a final `CC: <mention link(s)>` line to the description so the cc routing still fires.\n\n")
	b.WriteString("  2. **Context** — include ONLY when the input cited external resources AND you successfully fetched them AND they produced verifiable facts worth recording. Summarize facts only (e.g. \"PR #45 changes auth to JWT\"), not interpretation or unsolicited reference implementations. If you have nothing factual to add, omit the section entirely — never use it as an apology log for resources you could not fetch.\n\n")
	b.WriteString("  Hard rules: never invent requirements, implementation details, or acceptance criteria the user did not express; never reduce multi-sentence input to a single vague sentence; never echo the title.\n\n")

	// priority
	b.WriteString("- **priority**: one of `urgent`, `high`, `medium`, `low`, or omit. Map P0/P1 → urgent/high; \"asap\" → urgent. If unspecified, omit.\n\n")

	// assignee
	b.WriteString("- **assignee**:\n")
	b.WriteString("    - When the user names someone (\"assign to X\" / \"@X\"), call `multica workspace member list --output json`, `multica agent list --output json`, and `multica squad list --output json` and find the matching entity by display name. Squads are first-class assignees too — a squad name (e.g. \"Super Human\") routes work to the squad leader, who then delegates. On a clean unambiguous match, prefer `--assignee-id <uuid>` using the `user_id` (member) or `id` (agent or squad) from that JSON — UUID matching is exact and robust to name collisions in workspaces with overlapping names. `--assignee <name>` (fuzzy) is acceptable as a fallback when names are unambiguous. On no match or ambiguous match, do NOT pass either flag — instead append a final line to the description: `Unrecognized assignee: X`.\n")
	b.WriteString("    - Treat bare @-routing as an assignee directive even when the user did not write the English word \"assign\". This includes Chinese imperatives like `让 @独立团 review 这个 PR`, `给 @X 处理`, or `交给 @X`; strip the leading `@`/`＠` before matching display names. Do not keep that routing wrapper or `@Name` in the description unless it is a true CC-style notification rather than ownership. If the matched entity is a squad, pass the squad's `id` as `--assignee-id`, not the leader agent's id.\n")
	agentID := ""
	agentName := ""
	if task.Agent != nil {
		agentID = task.Agent.ID
		agentName = task.Agent.Name
	}
	switch {
	case task.SquadID != "":
		// The user opened quick-create with a SQUAD selected. The task
		// runs on the squad's leader agent, but the squad is the expected
		// owner — assigning to the leader would mask the squad's
		// delegation flow. Always point the default at the squad UUID.
		if task.SquadName != "" {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD %q: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadName, task.SquadID)
		} else {
			fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to the picker SQUAD: pass `--assignee-id %q` (the squad's UUID). The user opened quick-create with the squad selected; you (the leader agent) are running on the squad's behalf, so the squad — not you — is the expected owner. Never leave the issue unassigned, and do not assign it to your own agent UUID.\n\n", task.SquadID)
		}
	case agentID != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee-id %q` (your agent UUID). The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned. Use the UUID flag, not `--assignee <name>`, so the assignment is unambiguous even when other agents share part of your name.\n\n", agentID)
	case agentName != "":
		fmt.Fprintf(&b, "    - When the user did NOT name an assignee, default to YOURSELF: pass `--assignee %q`. The picker agent is the expected owner because the user opened quick-create with you selected — never leave the issue unassigned.\n\n", agentName)
	default:
		b.WriteString("    - When the user did NOT name an assignee, default to YOURSELF (the picker agent): pass `--assignee-id <your agent UUID>` (preferred) or `--assignee <your agent name>`. Never leave the issue unassigned.\n\n")
	}

	// project — pinned by the modal when the user picked one, otherwise
	// omitted so the platform routes to the workspace default. Always pass
	// the UUID (never a name) so the issue lands in the right project even
	// when several share a title.
	if task.ProjectID != "" {
		if task.ProjectTitle != "" {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in project %q (the user picked it in the quick-create modal). Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID, task.ProjectTitle)
		} else {
			fmt.Fprintf(&b, "- **project**: required for this run. Pass `--project %q` so the new issue lands in the project the user picked in the quick-create modal. Do not infer a different project from the prompt text — the modal selection is authoritative.\n", task.ProjectID)
		}
	} else {
		b.WriteString("- **project**: omit. The platform will route the issue to the workspace default.\n")
	}
	// parent — pinned by the modal when the user opened it from "Add sub
	// issue" on an existing issue. Pass the UUID (never the identifier) so
	// the create lands the sub-issue under the right parent even when the
	// workspace prefix changes; the identifier is included in the prose
	// purely as human-readable context for the agent.
	if task.ParentIssueID != "" {
		if task.ParentIssueIdentifier != "" {
			fmt.Fprintf(&b, "- **parent**: required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of %s (the user opened quick-create from that issue's \"Add sub issue\" entry). Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID, task.ParentIssueIdentifier)
		} else {
			fmt.Fprintf(&b, "- **parent**: required for this run. Pass `--parent %q` so the new issue is filed as a sub-issue of the parent the user picked in the quick-create modal. Do not infer a different parent from the prompt text — the modal entry point is authoritative.\n", task.ParentIssueID)
		}
	}
	b.WriteString("- **status**: omit (defaults to `todo`).\n")
	b.WriteString("- **attachments**: do NOT pass `--attachment`. The flag only accepts LOCAL file paths. Any image URL in the user input is already markdown — keep it inline in `--description` instead.\n\n")

	// output format
	b.WriteString("Output format:\n")
	b.WriteString("- Run exactly one `multica issue create --output json` invocation. Do not retry for any reason — even on non-zero exit. The issue may already exist; another attempt would create a duplicate.\n")
	b.WriteString("- Parse the JSON response to read the created issue's `identifier` (preferred) or `id` (fallback). Do not scrape human output and do not assume any workspace issue prefix such as `MUL-`; workspaces can use custom prefixes.\n")
	b.WriteString("- After success, print exactly one line: `Created <identifier-or-id>: <title>` and exit. No commentary, no follow-up tool calls.\n")
	b.WriteString("- Do NOT call `multica issue get` or `multica issue comment add` — there is no issue to query or comment on.\n")
	b.WriteString("- On CLI error or JSON parse error, exit with the error as the only output. The platform writes a failure notification automatically.\n")
	return b.String()
}

// buildCommentPrompt constructs a prompt for comment-triggered tasks.
// The triggering comment content is embedded directly so the agent cannot
// miss it, even when stale output files exist in a reused workdir.
// The reply instructions (including the current TriggerCommentID as --parent)
// are re-emitted on every turn so resumed sessions cannot carry forward a
// previous turn's --parent UUID.
func buildCommentPrompt(task Task, provider string) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	fmt.Fprintf(&b, "Your assigned issue ID is: %s\n\n", task.IssueID)
	if task.TriggerCommentContent != "" {
		authorLabel := "A user"
		if task.TriggerAuthorType == "agent" {
			name := task.TriggerAuthorName
			if name == "" {
				name = "another agent"
			}
			authorLabel = fmt.Sprintf("Another agent (%s)", name)
		}
		fmt.Fprintf(&b, "[NEW COMMENT] %s just left a new comment. Focus on THIS comment — do not confuse it with previous ones:\n\n", authorLabel)
		fmt.Fprintf(&b, "> %s\n\n", task.TriggerCommentContent)
		if task.TriggerAuthorType == "agent" {
			b.WriteString("⚠️ The triggering comment was posted by another agent. Decide whether a reply is warranted. If you produced actual work this turn (investigated, fixed something, answered a real question), post the result as a normal reply — that is NOT a noise comment, and the standard rule that final results must be delivered via comment still applies. If the triggering comment was a pure acknowledgment, thanks, or sign-off AND you produced no work this turn, do NOT reply — and do NOT post a comment saying 'No reply needed' or similar. Simply exit with no output. Silence is the preferred way to end agent-to-agent threads. If you do reply, do not @mention the other agent as a sign-off (that re-triggers them and starts a loop).\n\n")
		}
		if task.Agent != nil && strings.Contains(task.Agent.Instructions, "## Squad Operating Protocol") {
			fmt.Fprintf(&b, "⚠️ **Squad leader no_action rule:** If you decide no action is needed, call `multica squad activity %s no_action --reason \"...\"` and EXIT. DO NOT post any comment — not even one that says \"no action needed\" or \"exiting silently\". The squad activity call records your decision; a comment is redundant noise.\n\n", task.IssueID)
		}
	}
	fmt.Fprintf(&b, "Start by running `multica issue get %s --output json` to understand your task, then decide how to proceed.\n\n", task.IssueID)
	// Comment-reading pointer. Warm path with new comments: issue-wide
	// since-delta count, but steer the agent to read the triggering thread
	// first. Warm resumed path with no new comments: the trigger is already
	// injected, so don't force a duplicate thread read. Cold path: read the
	// triggering thread, not the flat timeline. Final fallback (no trigger id,
	// shouldn't happen here): plain read.
	if hint := execenv.BuildNewCommentsHint(task.IssueID, task.TriggerCommentID, task.NewCommentsSince, task.NewCommentCount); hint != "" {
		b.WriteString(hint)
	} else if task.PriorSessionID != "" {
		b.WriteString(execenv.BuildResumedCommentsHint(task.IssueID, task.TriggerCommentID))
	} else if cold := execenv.BuildColdCommentsHint(task.IssueID, task.TriggerCommentID); cold != "" {
		b.WriteString(cold)
	} else {
		fmt.Fprintf(&b, "Read the discussion: `multica issue comment list %s --output json` (long issue? use `--recent 20`).\n\n", task.IssueID)
	}
	b.WriteString(execenv.BuildCommentReplyInstructions(provider, task.IssueID, task.TriggerCommentID))
	return b.String()
}

// buildChatPrompt constructs a prompt for interactive chat tasks.
func buildChatPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a chat assistant for a Multica workspace.\n")
	b.WriteString("A user is chatting with you directly. Respond to their message.\n\n")
	// Takeover context: this chat is the human stepping in on a goal subtask you
	// were assigned that failed. Orient the agent so it doesn't start cold.
	if task.TakeoverSubtaskSpec != "" || task.TakeoverSubtaskTitle != "" {
		b.WriteString("## Takeover context\n")
		b.WriteString("This conversation is a hands-on takeover of a goal subtask you were assigned, which did not complete. The user is here to guide you through redoing it.\n")
		if task.TakeoverSubtaskTitle != "" {
			fmt.Fprintf(&b, "Subtask: %s\n", task.TakeoverSubtaskTitle)
		}
		if task.TakeoverSubtaskSpec != "" {
			fmt.Fprintf(&b, "Original spec:\n%s\n", task.TakeoverSubtaskSpec)
		}
		if task.TakeoverFailureReason != "" {
			fmt.Fprintf(&b, "Why it failed last time: %s\n", task.TakeoverFailureReason)
		}
		b.WriteString("Use the user's guidance below to get it right this time.\n\n")
	}
	// Discussion facilitation: this chat drives a task-mode goal still in the
	// 'discussion' phase. The agent is the 总控 (coordinator) and its job here is
	// NOT to start doing the work — it is to help the user shape a fuzzy ask into
	// a clear, confirmable goal (the "task card"). Once the user is happy they
	// click Confirm, which dispatches planning; the agent never self-triggers that.
	if task.GoalDiscussionActive {
		b.WriteString("## You are the 总控 (coordinator) in a task discussion\n")
		b.WriteString("This conversation is the discussion phase of a task. Your job is to help the user turn their request into a clear, executable goal — NOT to start doing the work yet, and NOT to plan or assign subtasks.\n")
		if task.GoalDiscussionTitle != "" {
			fmt.Fprintf(&b, "Working title: %s\n", task.GoalDiscussionTitle)
		}
		if strings.TrimSpace(task.GoalDiscussionGoal) != "" {
			fmt.Fprintf(&b, "Goal so far:\n%s\n", task.GoalDiscussionGoal)
		}
		b.WriteString("Facilitate actively:\n")
		b.WriteString("- Ask focused clarifying questions when the goal is ambiguous, underspecified, or has unstated assumptions (scope, success criteria, constraints, what's explicitly out of scope).\n")
		b.WriteString("- Reflect your current understanding back as a concise goal statement (a \"task card\") so the user can correct it. Converge — don't interrogate endlessly; once it's clear enough to execute, say so.\n")
		b.WriteString("- If the goal would benefit from roles not yet on the squad, suggest which to add (the user manages members from the task page).\n")
		b.WriteString("- Do NOT call any CLI to create issues, plan, or dispatch work. When the goal is clear, tell the user to click \"Confirm & execute\" to start planning — confirmation is the user's action, not yours.\n\n")
	}
	if task.Agent != nil && len(task.Agent.Skills) > 0 {
		refs := ExtractSlashSkills(task.ChatMessage)
		if len(refs) > 0 {
			agentSkills := make(map[string]string, len(task.Agent.Skills))
			for _, s := range task.Agent.Skills {
				agentSkills[s.ID] = s.Name
			}

			selected := make([]string, 0, len(refs))
			seen := make(map[string]struct{}, len(refs))
			for _, ref := range refs {
				name, ok := agentSkills[ref.ID]
				if !ok {
					continue
				}
				if _, ok := seen[ref.ID]; ok {
					continue
				}
				seen[ref.ID] = struct{}{}
				selected = append(selected, name)
			}

			if len(selected) > 0 {
				b.WriteString("Explicitly selected skills:\n")
				for _, name := range selected {
					fmt.Fprintf(&b, "- %s\n", name)
				}
				b.WriteString("\n")
			}
		}
	}
	fmt.Fprintf(&b, "User message:\n%s\n", task.ChatMessage)
	// List attachments by id + filename so the agent can fetch them via
	// the CLI. We deliberately do NOT inline the URL: chat attachments
	// live behind a signed CDN with a short TTL, so by the time the agent
	// has finished thinking the URL embedded in the markdown body may
	// have expired. `multica attachment download <id>` re-signs at click
	// time and is the only reliable path.
	if len(task.ChatMessageAttachments) > 0 {
		b.WriteString("\nAttachments on this message:\n")
		for _, a := range task.ChatMessageAttachments {
			if a.ContentType != "" {
				fmt.Fprintf(&b, "- id=%s filename=%q content_type=%s\n", a.ID, a.Filename, a.ContentType)
			} else {
				fmt.Fprintf(&b, "- id=%s filename=%q\n", a.ID, a.Filename)
			}
		}
		b.WriteString("Use `multica attachment download <id>` to fetch each file locally before referring to it.\n")
	}
	return b.String()
}

// buildAutopilotPrompt constructs a prompt for run_only autopilot tasks.
func buildAutopilotPrompt(task Task) string {
	var b strings.Builder
	b.WriteString("You are running as a local coding agent for a Multica workspace.\n\n")
	b.WriteString("This task was triggered by an Autopilot in run-only mode. There is no assigned Multica issue for this run.\n\n")
	fmt.Fprintf(&b, "Autopilot run ID: %s\n", task.AutopilotRunID)
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Autopilot ID: %s\n", task.AutopilotID)
	}
	if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "Autopilot title: %s\n", task.AutopilotTitle)
	}
	if task.AutopilotSource != "" {
		fmt.Fprintf(&b, "Trigger source: %s\n", task.AutopilotSource)
	}
	if strings.TrimSpace(string(task.AutopilotTriggerPayload)) != "" {
		fmt.Fprintf(&b, "Trigger payload:\n%s\n", strings.TrimSpace(string(task.AutopilotTriggerPayload)))
	}
	b.WriteString("\nAutopilot instructions:\n")
	if strings.TrimSpace(task.AutopilotDescription) != "" {
		b.WriteString(task.AutopilotDescription)
		b.WriteString("\n\n")
	} else if task.AutopilotTitle != "" {
		fmt.Fprintf(&b, "%s\n\n", task.AutopilotTitle)
	} else {
		b.WriteString("No additional autopilot instructions were provided. Inspect the autopilot configuration before proceeding.\n\n")
	}
	if task.AutopilotID != "" {
		fmt.Fprintf(&b, "Start by running `multica autopilot get %s --output json` if you need the full autopilot configuration, then complete the instructions above.\n", task.AutopilotID)
	} else {
		b.WriteString("Complete the instructions above.\n")
	}
	b.WriteString("Do not run `multica issue get`; this run does not have an issue ID.\n")
	return b.String()
}
