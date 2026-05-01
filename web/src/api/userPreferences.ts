import { request } from './client';

// UserPreferences mirrors pkg/userprefs.Preferences on the wire. The
// notifications and hotkeys envelopes are opaque JSONB on the server —
// the SPA owns the shape and the backend round-trips bytes. Empty
// theme / language are sentinel "no preference" values: the SPA falls
// back to its localStorage / OS defaults until the user actively picks
// one.
export type ThemePreferenceValue = '' | 'dark' | 'light' | 'system';

export interface NotificationPreferences {
  // Global on/off — when false the SPA suppresses NotificationCenter
  // bubbles and the Mentions page badge regardless of channel toggles.
  enabled?: boolean;
  // Per-channel toggles. Absent keys default to true.
  mentions?: boolean;
  approvals?: boolean;
  watches?: boolean;
}

export interface HotkeyPreferences {
  // Global on/off so power users can disable the system entirely
  // without unbinding individual ids.
  enabled?: boolean;
  // Per-id key-pattern overrides keyed by HotkeyId. Empty / absent
  // entries fall back to the registry default.
  overrides?: Record<string, string>;
}

export interface UserPreferences {
  userId: string;
  theme: ThemePreferenceValue;
  language: string;
  notifications: NotificationPreferences;
  hotkeys: HotkeyPreferences;
  createdAt?: string;
  updatedAt?: string;
}

export interface UpdateUserPreferencesInput {
  theme?: ThemePreferenceValue;
  language?: string;
  notifications?: NotificationPreferences;
  hotkeys?: HotkeyPreferences;
}

export function getUserPreferences(): Promise<UserPreferences> {
  return request<UserPreferences>('GET', '/api/v2/user-preferences');
}

export function updateUserPreferences(
  input: UpdateUserPreferencesInput,
): Promise<UserPreferences> {
  return request<UserPreferences>('PUT', '/api/v2/user-preferences', input);
}
