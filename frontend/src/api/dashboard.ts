import { request } from './client';

export interface ServerStatus {
  id: number;
  name: string;
  type: string;
  status: string;
  last_check: string;
  checks: { check_type: string; status: string; message: string; value: string }[];
}

export interface RecentRun {
  id: number;
  job_name: string;
  server_name: string;
  status: string;
  started_at: string;
  finished_at: string;
  total_size_bytes: number;
}

export interface DiskUsage {
  path: string;
  total_bytes: number;
  used_bytes: number;
  free_bytes: number;
  use_percent: number;
}

export interface Alert {
  id: string;
  severity: 'critical' | 'warning' | 'info';
  title: string;
  message: string;
  server_name: string;
  timestamp: string;
}

export interface DashboardSummary {
  servers: ServerStatus[];
  recent_runs: RecentRun[];
  disk_usage: DiskUsage[];
  alerts: Alert[];
  stats: {
    total_servers: number;
    servers_online: number;
    total_jobs: number;
    last_24h_runs: number;
    last_24h_success: number;
    last_24h_failed: number;
  };
}

export const dashboardApi = {
  getSummary: () => request<DashboardSummary>('/api/dashboard/summary'),
};
