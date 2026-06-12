package main

import (
	"context"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerGoalListeners hooks task lifecycle events so a goal subtask's
// agent_task_queue task completing/failing drives the goal_subtask state
// machine: update the subtask, unlock downstream on success, block it on
// failure, and roll the goal_run aggregate status up. Mirrors
// registerAutopilotListeners — same EventTaskCompleted/Failed/Cancelled feed,
// gated on task.GoalSubtaskID instead of task.AutopilotRunID.
func registerGoalListeners(bus *events.Bus, svc *service.GoalService) {
	ctx := context.Background()

	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		syncGoalFromTaskEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventTaskFailed, func(e events.Event) {
		syncGoalFromTaskEvent(ctx, svc, e)
	})
	bus.Subscribe(protocol.EventTaskCancelled, func(e events.Event) {
		syncGoalFromTaskEvent(ctx, svc, e)
	})
}

// syncGoalFromTaskEvent routes a terminal task event to the right goal handler:
// subtask tasks (goal_subtask_id set) drive the DAG; planning tasks (no
// goal_subtask_id, goal_planning context) finalize the planning phase. A task
// that is neither is ignored.
func syncGoalFromTaskEvent(ctx context.Context, svc *service.GoalService, e events.Event) {
	payload, ok := e.Payload.(map[string]any)
	if !ok {
		return
	}
	taskID, ok := payload["task_id"].(string)
	if !ok || taskID == "" {
		return
	}
	task, err := svc.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		return
	}
	if task.GoalSubtaskID.Valid {
		svc.SyncSubtaskFromTask(ctx, task)
		return
	}
	// Planning, summary AND decision tasks carry no goal_subtask_id; each service
	// method filters by its own context type and no-ops on the others.
	svc.SyncSummaryFromTask(ctx, task)
	svc.SyncPlanningFromTask(ctx, task)
	svc.SyncDecisionFromTask(ctx, task)
}
