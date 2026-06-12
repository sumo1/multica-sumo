-- Task-mode goals may declare a "dependency project" — the engineering repo the
-- goal works against. Roles are synced from that project's repo, and the PMO
-- reads it during planning. Nullable: a goal without a project falls back to the
-- workspace-wide agent pool.
ALTER TABLE goal_run ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE SET NULL;

CREATE INDEX idx_goal_run_project ON goal_run(project_id) WHERE project_id IS NOT NULL;
