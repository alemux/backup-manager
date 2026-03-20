import { request } from './client';

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
  testNotification: (data: Record<string, unknown>) =>
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
