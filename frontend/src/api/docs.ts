import { request } from './client';

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
