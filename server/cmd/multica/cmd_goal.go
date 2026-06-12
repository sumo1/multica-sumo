package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var goalCmd = &cobra.Command{
	Use:   "goal",
	Short: "Work with goals (PMO orchestration)",
}

// ── Plan submission ──────────────────────────────────────────────────────────
//
// Used by a squad leader (PMO) during a planning task: it decomposes the goal
// into subtasks and submits them here. The server persists the DAG, flips the
// goal to executing, and dispatches the root subtasks to the assigned roles.

var goalPlanCmd = &cobra.Command{
	Use:   "plan <goal-id>",
	Short: "Submit a goal decomposition (subtask plan)",
	Long: "Submit the subtask plan for a goal. Pass the JSON array of subtasks " +
		"via --subtasks '<json>' or pipe it on stdin with --subtasks-stdin.\n\n" +
		"Each subtask: {\"seq\": <int>, \"title\": \"...\", \"spec\": \"...\", " +
		"\"assignee_agent_id\": \"<uuid>\", \"depends_on\": [<seq>, ...]}",
	Args: cobra.ExactArgs(1),
	RunE: runGoalPlan,
}

func runGoalPlan(cmd *cobra.Command, args []string) error {
	goalID := strings.TrimSpace(args[0])
	if goalID == "" {
		return fmt.Errorf("goal id is required")
	}

	raw, err := resolveSubtasksInput(cmd)
	if err != nil {
		return err
	}

	// Validate the JSON is a subtask array before sending, so the agent gets a
	// clear local error instead of a generic 400.
	var subtasks []map[string]any
	if err := json.Unmarshal([]byte(raw), &subtasks); err != nil {
		return fmt.Errorf("--subtasks must be a JSON array of subtasks: %w", err)
	}
	if len(subtasks) == 0 {
		return fmt.Errorf("plan must contain at least one subtask")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"subtasks": subtasks}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/goals/"+goalID+"/plan", body, &result); err != nil {
		return fmt.Errorf("submit plan: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// resolveSubtasksInput reads the subtask JSON from --subtasks or stdin
// (--subtasks-stdin), mirroring the --content / --content-stdin convention.
func resolveSubtasksInput(cmd *cobra.Command) (string, error) {
	useStdin, _ := cmd.Flags().GetBool("subtasks-stdin")
	inline, _ := cmd.Flags().GetString("subtasks")

	if useStdin && inline != "" {
		return "", fmt.Errorf("--subtasks and --subtasks-stdin are mutually exclusive")
	}
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin for --subtasks-stdin: %w", err)
		}
		body := strings.TrimSpace(string(data))
		if body == "" {
			return "", fmt.Errorf("stdin content for --subtasks-stdin is empty")
		}
		return body, nil
	}
	if strings.TrimSpace(inline) == "" {
		return "", fmt.Errorf("provide the plan via --subtasks '<json>' or --subtasks-stdin")
	}
	return inline, nil
}

// ── Verdict (verify nodes) ───────────────────────────────────────────────────
//
// Used by a verify node's agent to report its adversarial-review verdict. A
// 'reject' bounces the reviewed node back for another attempt; a 'pass' lets
// the workflow proceed.

var goalVerdictCmd = &cobra.Command{
	Use:   "verdict <subtask-id> <pass|reject>",
	Short: "Report a verify node's pass/reject verdict",
	Args:  cobra.ExactArgs(2),
	RunE:  runGoalVerdict,
}

func runGoalVerdict(cmd *cobra.Command, args []string) error {
	subtaskID := strings.TrimSpace(args[0])
	verdict := strings.TrimSpace(args[1])
	if subtaskID == "" {
		return fmt.Errorf("subtask id is required")
	}
	if verdict != "pass" && verdict != "reject" {
		return fmt.Errorf("verdict must be 'pass' or 'reject'")
	}
	reason, _ := cmd.Flags().GetString("reason")
	if verdict == "reject" && strings.TrimSpace(reason) == "" {
		return fmt.Errorf("--reason is required when rejecting")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"verdict": verdict, "reason": reason}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/goals/subtasks/"+subtaskID+"/verdict", body, &result); err != nil {
		return fmt.Errorf("submit verdict: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

// ── Decision (下一步判断) ──────────────────────────────────────────────────────
//
// Used by the 总控 (coordinator) during a next-step judgment task: a planned node
// failed and downstream work depends on it. The coordinator inspects the failure
// and reports how to proceed. 'reshape' rewrites the node's spec then retries it.

var goalDecideCmd = &cobra.Command{
	Use:   "decide <subtask-id> <proceed|reshape|abort>",
	Short: "Report a coordinator's next-step judgment on a failed subtask",
	Long: "Report how to proceed past a failed subtask. 'proceed' skips it and " +
		"unblocks downstream; 'reshape' rewrites its spec (via --spec) and retries " +
		"it; 'abort' blocks the downstream branch.",
	Args: cobra.ExactArgs(2),
	RunE: runGoalDecide,
}

func runGoalDecide(cmd *cobra.Command, args []string) error {
	subtaskID := strings.TrimSpace(args[0])
	decision := strings.TrimSpace(args[1])
	if subtaskID == "" {
		return fmt.Errorf("subtask id is required")
	}
	switch decision {
	case "proceed", "reshape", "abort":
	default:
		return fmt.Errorf("decision must be 'proceed', 'reshape', or 'abort'")
	}
	spec, _ := cmd.Flags().GetString("spec")

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	body := map[string]any{"decision": decision, "spec": spec}
	var result map[string]any
	if err := client.PostJSON(ctx, "/api/goals/subtasks/"+subtaskID+"/decide", body, &result); err != nil {
		return fmt.Errorf("submit decision: %w", err)
	}
	return cli.PrintJSON(os.Stdout, result)
}

func init() {
	goalPlanCmd.Flags().String("subtasks", "", "JSON array of subtasks")
	goalPlanCmd.Flags().Bool("subtasks-stdin", false, "read the subtasks JSON array from stdin")
	goalCmd.AddCommand(goalPlanCmd)

	goalVerdictCmd.Flags().String("reason", "", "reason for the verdict (required for reject)")
	goalCmd.AddCommand(goalVerdictCmd)

	goalDecideCmd.Flags().String("spec", "", "replacement spec for the node (used with 'reshape')")
	goalCmd.AddCommand(goalDecideCmd)
}
