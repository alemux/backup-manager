import { request } from './client';

export interface SnapshotSyncStatus {
  destination_id: number;
  destination_name: string;
  status: 'pending' | 'in_progress' | 'success' | 'failed';
  retry_count: number;
  last_error?: string;
  synced_at?: string;
}

export interface Snapshot {
  id: number;
  run_id: number;
  source_id: number;
  source_name: string;
  source_type: 'web' | 'database' | 'config';
  source_path?: string;
  db_name?: string;
  server_id: number;
  server_name: string;
  snapshot_path: string;
  size_bytes: number;
  checksum_sha256?: string;
  is_encrypted: boolean;
  integrity_status?: string;
  retention_expires_at?: string;
  created_at: string;
  sync_statuses: SnapshotSyncStatus[];
}

export interface SnapshotListResponse {
  snapshots: Snapshot[];
  total: number;
  page: number;
  per_page: number;
  total_pages: number;
}

export interface CalendarDay {
  date: string;
  count: number;
  success_count: number;
  failed_count: number;
}

export interface CalendarResponse {
  year: number;
  month: number;
  days: CalendarDay[];
}

export interface SnapshotListParams {
  page?: number;
  per_page?: number;
  date_from?: string;
  date_to?: string;
  server_id?: string;
  source_type?: string;
}

export const snapshotsApi = {
  list: (params?: SnapshotListParams) => {
    const qs = params
      ? '?' + new URLSearchParams(
          Object.entries(params)
            .filter(([, v]) => v !== undefined && v !== '' && v !== 'all')
            .map(([k, v]) => [k, String(v)])
        ).toString()
      : '';
    return request<SnapshotListResponse>(`/api/snapshots${qs}`);
  },

  get: (id: number) => request<Snapshot>(`/api/snapshots/${id}`),

  download: (id: number) => {
    window.open(`/api/snapshots/${id}/download`, '_blank');
  },

  getCalendar: (month: number, year: number) =>
    request<CalendarResponse>(`/api/snapshots/calendar?month=${month}&year=${year}`),
};
