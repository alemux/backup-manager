import { request } from './client';

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

export interface AnalysisSourceResult {
  source_id: number;
  source_name: string;
  source_type: string;
  total_files: number;
  files_to_transfer: number;
  bytes_to_transfer: number;
  bytes_total: number;
  human_size: string;
  human_total: string;
  error?: string;
}

export interface AnalysisResult {
  sources: AnalysisSourceResult[];
  total_bytes_to_transfer: number;
  total_bytes_all: number;
  total_files_to_transfer: number;
  human_total_transfer: string;
  human_total_all: string;
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
  analyze: (id: number) =>
    request<AnalysisResult>(`/api/jobs/${id}/analyze`, { method: 'POST' }),
  listRuns: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<RunsResponse>(`/api/runs${qs}`);
  },
  getRunLogs: (id: number) => request<unknown>(`/api/runs/${id}/logs`),
};
