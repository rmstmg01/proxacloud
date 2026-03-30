import { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import type { User } from '../types';
import { login as apiLogin } from '../api/auth';

interface AuthContextValue {
  user: User | null;
  token: string | null;
  login: (username: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function loadSaved(): { user: User | null; token: string | null } {
  try {
    const token = sessionStorage.getItem('jwt_token');
    const raw = sessionStorage.getItem('auth_user');
    if (token && raw) return { token, user: JSON.parse(raw) as User };
  } catch {
    // ignore
  }
  return { user: null, token: null };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const saved = loadSaved();
  const [user, setUser] = useState<User | null>(saved.user);
  const [token, setToken] = useState<string | null>(saved.token);

  const login = useCallback(async (username: string, password: string) => {
    const res = await apiLogin(username, password);
    sessionStorage.setItem('jwt_token', res.token);
    sessionStorage.setItem('auth_user', JSON.stringify(res.user));
    setToken(res.token);
    setUser(res.user);
  }, []);

  const logout = useCallback(() => {
    sessionStorage.removeItem('jwt_token');
    sessionStorage.removeItem('auth_user');
    setToken(null);
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, token, login, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
