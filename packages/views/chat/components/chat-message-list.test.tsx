// @vitest-environment jsdom

import { describe, it, expect, afterEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, cleanup } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { ChatMessage } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";
import { ChatMessageList } from "./chat-message-list";

const TEST_RESOURCES = { en: { chat: enChat } };

function msg(id: string, role: "user" | "assistant", content: string, ts: string): ChatMessage {
  return {
    id,
    chat_session_id: "chat-1",
    role,
    content,
    task_id: null,
    created_at: ts,
  };
}

function renderList(props: Parameters<typeof ChatMessageList>[0]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ChatMessageList {...props} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

afterEach(cleanup);

describe("ChatMessageList timelineInsert", () => {
  // The Task page bug: planning/summary streams were pinned to the bottom, so
  // a chat message sent AFTER the task completed rendered above them. The
  // insert must sit at the confirm gate (afterTs), keeping the thread in
  // time order: pre-confirm chat → streams → post-confirm chat.
  it("places the insert between pre- and post-confirm messages by timestamp", () => {
    const messages: ChatMessage[] = [
      msg("m1", "user", "describe the goal", "2026-06-10T20:30:00Z"),
      msg("m2", "assistant", "ok planning", "2026-06-10T20:32:00Z"),
      msg("m3", "user", "follow-up after completion", "2026-06-10T20:47:00Z"),
    ];

    renderList({
      messages,
      pendingTask: null,
      availability: undefined,
      timelineInsert: {
        afterTs: "2026-06-10T20:33:00Z", // confirm gate, between m2 and m3
        content: <div data-testid="streams">PLANNING + SUMMARY</div>,
      },
    });

    // Document position: the insert must come after m2 (pre-confirm) and
    // before m3 (post-confirm completion chat).
    const insert = screen.getByTestId("streams");
    const preText = screen.getByText("ok planning");
    const postText = screen.getByText("follow-up after completion");

    expect(preText.compareDocumentPosition(insert) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(insert.compareDocumentPosition(postText) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("renders the insert at the end when no anchor is given (plain footer)", () => {
    const messages: ChatMessage[] = [
      msg("m1", "user", "only message", "2026-06-10T20:30:00Z"),
    ];

    renderList({
      messages,
      pendingTask: null,
      availability: undefined,
      timelineInsert: { afterTs: "", content: <div data-testid="footer">FOOTER</div> },
    });

    const footer = screen.getByTestId("footer");
    const only = screen.getByText("only message");
    // Empty anchor → insert after all messages.
    expect(only.compareDocumentPosition(footer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
