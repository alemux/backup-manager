const BASE = '';
const headers = { 'Content-Type': 'application/json' };

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    credentials: 'include',
    headers,
    ...options,
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(err.error || res.statusText);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

export interface Job {
  id: number;
  name: string;
  server_id: number;
  server_name: string;
  schedule: string;
  retention_daily: number;
  retention_weekly: number;
  retention_monthly: number;
  bandwidth_limit_mbps: number | null;
  timeout_minutes: number;
  enabled: boolean;
  source_ids: number[];
  last_run: {
    id: number;
    status: string;
    started_at: string;
    finished_at: string;
    total_size_bytes: number;
  } | null;
  created_at: string;
}

export interface BackupRun {
  id: number;
  job_id: number;
  status: string;
  started_at: string;
  finished_at: string;
  total_size_bytes: number;
  files_copied: number;
  error_message: string;
}

export interface RunsResponse {
  runs: BackupRun[];
  total: number;
  page: number;
  per_page: number;
}

export const jobsApi = {
  list: () => request<Job[]>('/api/jobs'),
  create: (data: unknown) =>
    request<Job>('/api/jobs', { method: 'POST', body: JSON.stringify(data) }),
  get: (id: number) => request<Job>(`/api/jobs/${id}`),
  update: (id: number, data: unknown) =>
    request<Job>(`/api/jobs/${id}`, { method: 'PUT', body: JSON.stringify(data) }),
  delete: (id: number) =>
    request<void>(`/api/jobs/${id}`, { method: 'DELETE' }),
  trigger: (id: number) =>
    request<{ run_id: number }>(`/api/jobs/${id}/trigger`, { method: 'POST' }),
  listRuns: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<RunsResponse>(`/api/runs${qs}`);
  },
  getRunLogs: (id: number) => request<unknown>(`/api/runs/${id}/logs`),
};
