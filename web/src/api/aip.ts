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

// AIPMessage mirrors pkg/aip.Message on the wire.
export interface AIPMessage {
  id: number;
  threadId: string;
  role: 'system' | 'user' | 'assistant';
  content: string;
  tokenCount?: number;
  createdAt: string;
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

export interface SendMessageResponse {
  userMessage: AIPMessage;
  assistantMessage: AIPMessage;
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
