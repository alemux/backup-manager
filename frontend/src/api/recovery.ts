import { request } from './client';

export interface Step {
  order: number;
  title: string;
  description: string;
  command?: string;
  verify?: string;
  notes?: string;
}

export interface Playbook {
  id: number;
  server_id: number | null;
  title: string;
  scenario: 'full_server' | 'single_database' | 'single_project' | 'config_only' | 'certificates';
  steps: Step[];
  created_at: string;
}

export const recoveryApi = {
  list: () => request<Playbook[]>('/api/recovery/playbooks'),

  get: (id: number) => request<Playbook>(`/api/recovery/playbooks/${id}`),

  generate: (serverId: number) =>
    request<Playbook[]>(`/api/recovery/playbooks/generate/${serverId}`, {
      method: 'POST',
    }),

  update: (id: number, playbook: Partial<Playbook>) =>
    request<Playbook>(`/api/recovery/playbooks/${id}`, {
      method: 'PUT',
      body: JSON.stringify(playbook),
    }),

  delete: (id: number) =>
    request<void>(`/api/recovery/playbooks/${id}`, { method: 'DELETE' }),
};
