const BASE = '';

function getCSRFToken(): string {
  const match = document.cookie
    .split('; ')
    .find((row) => row.startsWith('csrf_token='));
  return match ? match.split('=')[1] : '';
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const method = (options?.method ?? 'GET').toUpperCase();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options?.headers as Record<string, string>),
  };
  if (['POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) {
    const token = getCSRFToken();
    if (token) headers['X-CSRF-Token'] = token;
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
