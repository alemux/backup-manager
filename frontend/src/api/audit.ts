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
