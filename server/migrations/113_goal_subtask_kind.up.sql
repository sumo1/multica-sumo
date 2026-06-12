-- Dynamic workflows: a goal_subtask is now typed. 'execute' nodes do the work
-- (the existing behavior); 'verify' nodes adversarially review the output of
-- the node(s) they depend on and emit a verdict (pass / reject). A reject
-- bounces the reviewed node back for a bounded retry, then re-verifies. This
-- lets the PMO design a workflow (not just a flat task list) — inserting
-- adversarial-verification nodes where quality matters.

ALTER TABLE goal_subtask
    ADD COLUMN kind TEXT NOT NULL DEFAULT 'execute'
        CHECK (kind IN ('execute', 'verify'));

-- Verdict is meaningful only on verify nodes: NULL until the verifier reports,
-- then 'pass' or 'reject'. Execute nodes leave it NULL.
ALTER TABLE goal_subtask
    ADD COLUMN verdict TEXT
        CHECK (verdict IS NULL OR verdict IN ('pass', 'reject'));
