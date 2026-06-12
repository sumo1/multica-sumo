-- Human takeover: a chat_session can be the "takeover conversation" for a goal
-- subtask. When a subtask fails and the user wants to guide its agent hands-on,
-- we create a chat session bound to that subtask's assignee agent and mark it
-- here. The daemon's chat prompt then injects the subtask spec + failure
-- history so the agent enters the conversation already knowing the context.
-- Nullable: the vast majority of chat sessions are not takeover sessions.

ALTER TABLE chat_session
    ADD COLUMN goal_subtask_id UUID REFERENCES goal_subtask(id) ON DELETE SET NULL;

CREATE INDEX idx_chat_session_goal_subtask ON chat_session(goal_subtask_id);
