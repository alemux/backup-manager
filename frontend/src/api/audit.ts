import { request } from './client';

export interface AuditEntry {
  id: number;
  user_id: number | null;
  username?: string;
  action: string;
  target: string | null;
  ip_address: string | null;
  details: string | null;
  created_at: string;
}

export interface AuditListResponse {
  data: AuditEntry[];
  total: number;
  page: number;
  per_page: number;
}

export const auditApi = {
  list: (params?: Record<string, string>) => {
    const qs = params ? '?' + new URLSearchParams(params).toString() : '';
    return request<AuditListResponse>(`/api/audit${qs}`);
  },
  exportCSV: () => {
    window.open('/api/audit/export', '_blank');
  },
};
