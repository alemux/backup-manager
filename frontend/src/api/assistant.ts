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

export interface Message {
  id: number;
  role: 'user' | 'assistant';
  content: string;
  created_at: string;
}

export const assistantApi = {
  chat: (message: string) =>
    request<Message>('/api/assistant/chat', {
      method: 'POST',
      body: JSON.stringify({ message }),
    }),

  getConversations: () => request<Message[]>('/api/assistant/conversations'),

  clearConversations: () =>
    request<void>('/api/assistant/conversations', { method: 'DELETE' }),
};
