import { request } from './client';

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
