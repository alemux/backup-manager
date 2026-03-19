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
  return res.json();
}

export interface DocEntry {
  slug: string;
  title: string;
  category: string;
}

export interface DocContent {
  content: string;
}

export const docsApi = {
  list: () => request<DocEntry[]>('/api/docs'),

  get: (slug: string) => request<DocContent>(`/api/docs/${slug}`),

  search: (q: string) =>
    request<DocEntry[]>(`/api/docs/search?q=${encodeURIComponent(q)}`),
};
