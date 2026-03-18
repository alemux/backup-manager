import { useState, useCallback } from 'react';
import { api } from '../api/client';
import type { User } from '../types';

export function useAuth() {
  const [user, setUser] = useState<User | null>(() => {
    const saved = localStorage.getItem('bm_user');
    return saved ? JSON.parse(saved) : null;
  });

  const login = useCallback(async (username: string, password: string) => {
    const data = await api.login(username, password) as User;
    setUser(data);
    localStorage.setItem('bm_user', JSON.stringify(data));
    return data;
  }, []);

  const logout = useCallback(async () => {
    await api.logout();
    setUser(null);
    localStorage.removeItem('bm_user');
  }, []);

  const handleUnauthorized = useCallback(() => {
    setUser(null);
    localStorage.removeItem('bm_user');
  }, []);

  return { user, login, logout, handleUnauthorized, isAuthenticated: !!user };
}
