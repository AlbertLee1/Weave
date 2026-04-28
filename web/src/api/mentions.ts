import { request } from './client';

// MentionUser mirrors pkg/comments.MentionUser. Returned by the
// /api/v2/mentions/search autocomplete and used to render the dropdown
// rows behind each `@`-prefixed query in a comment textarea.
export interface MentionUser {
  id: string;
  email: string;
  name?: string;
}

export interface SearchMentionsResponse {
  users: MentionUser[];
}

// searchMentionUsers returns up to limit users whose email or display
// name contain q (case-insensitive). The backend caps limit at 25 to
// keep dropdown responses small.
export function searchMentionUsers(q: string, limit = 8): Promise<SearchMentionsResponse> {
  const qs = new URLSearchParams({ q, limit: String(limit) });
  return request<SearchMentionsResponse>('GET', `/api/v2/mentions/search?${qs.toString()}`);
}
