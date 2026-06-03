import { request } from './client';

// AIPThread mirrors pkg/aip.Thread on the wire.
export interface AIPThread {
  id: string;
  title?: string;
  provider: string;
  model?: string;
  systemPrompt?: string;
  createdBy?: string;
  createdAt: string;
  updatedAt: string;
}

// AIPToolCall mirrors pkg/aip.ToolCall on the wire. Present on
// assistant messages that requested function invocations (US-284).
export interface AIPToolCall {
  id: string;
  name: string;
  arguments?: unknown;
}

// AIPMessage mirrors pkg/aip.Message on the wire. Tool-call fields
// (toolCalls / toolCallId / toolName) are populated on rows produced
// by the function-calling chain (US-284) and absent on regular
// system / user / assistant text turns. parentMessageId / branchId
// arrived with US-374 to expose tree shape; legacy linear callers may
// omit both.
export interface AIPMessage {
  id: number;
  threadId: string;
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  tokenCount?: number;
  toolCalls?: AIPToolCall[];
  toolCallId?: string;
  toolName?: string;
  parentMessageId?: number | null;
  branchId?: string;
  createdAt: string;
}

// AIPMessageTreeNode mirrors pkg/aip.MessageTreeNode on the wire. The
// node carries every Message field flat (Go embeds *Message into the
// struct) plus a children slice ordered by message id asc.
export interface AIPMessageTreeNode extends AIPMessage {
  children?: AIPMessageTreeNode[];
}

export interface ThreadTreeResponse {
  threadId: string;
  roots: AIPMessageTreeNode[];
}

export interface ListThreadsResponse {
  threads: AIPThread[];
}

export interface ListMessagesResponse {
  messages: AIPMessage[];
}

export interface CreateThreadRequest {
  title?: string;
  provider: string;
  model?: string;
  systemPrompt?: string;
}

export interface UpdateThreadRequest {
  title?: string;
  model?: string;
  systemPrompt?: string;
}

export interface SendMessageRequest {
  content: string;
  temperature?: number;
  maxTokens?: number;
}

// ForkThreadRequest mirrors pkg/aip.forkThreadRequest. messageId names
// the pivot message in the source thread; the new thread copies every
// ancestor from the root through (and including) the pivot. The optional
// overrides (title / model / systemPrompt) relabel the fresh branch at
// fork time; provider is always inherited from the source thread.
export interface ForkThreadRequest {
  messageId: number;
  newThreadId?: string;
  title?: string;
  model?: string;
  systemPrompt?: string;
}

// ForkThreadResponse mirrors pkg/aip.forkThreadResponse. The new thread
// plus its copied ancestor chain are echoed inline so the SPA can render
// the fresh branch without a second ListMessages round-trip.
export interface ForkThreadResponse {
  thread: AIPThread;
  messages: AIPMessage[];
}

export interface SendMessageResponse {
  userMessage: AIPMessage;
  assistantMessage: AIPMessage;
  // toolMessages carries every RoleTool result the function-calling
  // chain produced (US-284). Absent / empty on plain chat turns.
  toolMessages?: AIPMessage[];
  // iterations is the number of Provider.Complete cycles the
  // SendMessage loop performed (1 for plain chat).
  iterations?: number;
}

export function listThreads(): Promise<ListThreadsResponse> {
  return request<ListThreadsResponse>('GET', '/api/v2/aip/threads');
}

export function getThread(threadId: string): Promise<AIPThread> {
  return request<AIPThread>('GET', `/api/v2/aip/threads/${encodeURIComponent(threadId)}`);
}

export function createThread(body: CreateThreadRequest): Promise<AIPThread> {
  return request<AIPThread>('POST', '/api/v2/aip/threads', body);
}

export function updateThread(
  threadId: string,
  body: UpdateThreadRequest,
): Promise<AIPThread> {
  return request<AIPThread>(
    'PUT',
    `/api/v2/aip/threads/${encodeURIComponent(threadId)}`,
    body,
  );
}

export function deleteThread(threadId: string): Promise<void> {
  return request<void>(
    'DELETE',
    `/api/v2/aip/threads/${encodeURIComponent(threadId)}`,
  );
}

export function getThreadTree(threadId: string): Promise<ThreadTreeResponse> {
  return request<ThreadTreeResponse>(
    'GET',
    `/api/v2/aip/threads/${encodeURIComponent(threadId)}/tree`,
  );
}

export function listMessages(threadId: string): Promise<ListMessagesResponse> {
  return request<ListMessagesResponse>(
    'GET',
    `/api/v2/aip/threads/${encodeURIComponent(threadId)}/messages`,
  );
}

export function sendMessage(
  threadId: string,
  body: SendMessageRequest,
): Promise<SendMessageResponse> {
  return request<SendMessageResponse>(
    'POST',
    `/api/v2/aip/threads/${encodeURIComponent(threadId)}/messages`,
    body,
  );
}

export function forkThread(
  threadId: string,
  body: ForkThreadRequest,
): Promise<ForkThreadResponse> {
  return request<ForkThreadResponse>(
    'POST',
    `/api/v2/aip/threads/${encodeURIComponent(threadId)}/fork`,
    body,
  );
}
