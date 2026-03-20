import { request } from './client';
import type { Server, BackupSource } from '../types';

export const serversApi = {
  list: () => request<Server[]>('/api/servers'),
  create: (data: Partial<Server>) =>
    request<Server>('/api/servers', {
      method: 'POST',
      body: JSON.stringify(data),
    }),
  get: (id: number) => request<Server>(`/api/servers/${id}`),
  update: (id: number, data: Partial<Server>) =>
    request<Server>(`/api/servers/${id}`, {
      method: 'PUT',
      body: JSON.stringify(data),
    }),
  delete: (id: number) =>
    request<void>(`/api/servers/${id}`, { method: 'DELETE' }),
  testConnection: (data: unknown) =>
    request<{ success: boolean; message: string }>(
      '/api/servers/test-connection',
      { method: 'POST', body: JSON.stringify(data) }
    ),
  discover: (id: number) =>
    request<unknown>(`/api/servers/${id}/discover`, { method: 'POST' }),
  listSources: (serverId: number) =>
    request<BackupSource[]>(`/api/servers/${serverId}/sources`),
  createSource: (serverId: number, data: unknown) =>
    request<BackupSource>(`/api/servers/${serverId}/sources`, {
      method: 'POST',
      body: JSON.stringify(data),
    }),
};
