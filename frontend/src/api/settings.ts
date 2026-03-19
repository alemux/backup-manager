const BASE = '';

function getCSRFToken(): string {
  const match = document.cookie
    .split('; ')
    .find((row) => row.startsWith('csrf_token='));
  return match ? match.split('=')[1] : '';
}

const CSRF_METHODS = new Set(['POST', 'PUT', 'DELETE', 'PATCH']);

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const method = (options?.method ?? 'GET').toUpperCase();

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options?.headers as Record<string, string>),
  };

  if (CSRF_METHODS.has(method)) {
    const csrfToken = getCSRFToken();
    if (csrfToken) {
      headers['X-CSRF-Token'] = csrfToken;
    }
  }

  const res = await fetch(`${BASE}${path}`, {
    credentials: 'include',
    ...options,
    headers,
  });

  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export interface Destination {
  id: number;
  name: string;
  type: 'local' | 'nas' | 'usb' | 's3';
  path: string;
  is_primary: boolean;
  retention_daily: number;
  retention_weekly: number;
  retention_monthly: number;
  enabled: boolean;
  created_at: string;
}

export interface NotificationConfig {
  id: number;
  event_type: string;
  telegram_enabled: boolean;
  email_enabled: boolean;
  telegram_chat_id: string;
  email_recipients: string;
}

export interface User {
  id: number;
  username: string;
  email: string;
  is_admin: boolean;
  created_at: string;
  updated_at: string;
}

export const settingsApi = {
  // Notification config
  getNotifications: () => request<NotificationConfig[]>('/api/notifications/config'),
  updateNotifications: (configs: Partial<NotificationConfig>[]) =>
    request<NotificationConfig[]>('/api/notifications/config', {
      method: 'PUT',
      body: JSON.stringify(configs),
    }),
  testNotification: (data: { channel: string; target: string }) =>
    request<{ status: string }>('/api/notifications/test', {
      method: 'POST',
      body: JSON.stringify(data),
    }),

  // Destinations
  getDestinations: () => request<Destination[]>('/api/destinations'),
  createDestination: (data: Partial<Destination>) =>
    request<Destination>('/api/destinations', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  updateDestination: (id: number, data: Partial<Destination>) =>
    request<Destination>(`/api/destinations/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  deleteDestination: (id: number) =>
    request<void>(`/api/destinations/${id}`, { method: 'DELETE' }),

  // General settings
  getSettings: () => request<Record<string, string>>('/api/settings'),
  updateSettings: (data: Record<string, string>) =>
    request<{ message: string }>('/api/settings', {
      method: 'PUT',
      body: JSON.stringify(data),
    }),

  // Users
  listUsers: () => request<User[]>('/api/users'),
  createUser: (data: { username: string; email: string; password: string; is_admin: boolean }) =>
    request<User>('/api/users', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  updateUser: (id: number, data: { email?: string; password?: string; is_admin?: boolean }) =>
    request<User>(`/api/users/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  deleteUser: (id: number) =>
    request<void>(`/api/users/${id}`, { method: 'DELETE' }),
};
