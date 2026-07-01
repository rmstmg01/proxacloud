import { createContext, useContext, useState, useCallback, type ReactNode } from 'react';
import type { LoginResponse, LoginResult, User } from '../types';
import { login as apiLogin, verifyLogin2FA as apiVerifyLogin2FA } from '../api/auth';

interface AuthContextValue {
  user: User | null;
  token: string | null;
  login: (username: string, password: string) => Promise<LoginResult>;
  verifyLogin2FA: (challengeToken: string, code: string) => Promise<void>;
  saveSession: (res: LoginResponse) => void;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

const TOKEN_KEY = 'jwt_token';
const USER_KEY = 'auth_user';

function loadSaved(): { user: User | null; token: string | null } {
 try {
    const token = localStorage.getItem(TOKEN_KEY) || sessionStorage.getItem(TOKEN_KEY);
    const raw = localStorage.getItem(USER_KEY) || sessionStorage.getItem(USER_KEY);
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

  const saveSession = useCallback((res: LoginResponse) => {
    localStorage.setItem(TOKEN_KEY, res.token);
    localStorage.setItem(USER_KEY, JSON.stringify(res.user));
    sessionStorage.removeItem(TOKEN_KEY);
    sessionStorage.removeItem(USER_KEY);
    setToken(res.token);
    setUser(res.user);
  }, []);

  const login = useCallback(async (username: string, password: string) => {
    const res = await apiLogin(username, password);
    if ('token' in res) {
      saveSession(res);
    }
    return res;
  }, [saveSession]);

  const verifyLogin2FA = useCallback(async (challengeToken: string, code: string) => {
    const res = await apiVerifyLogin2FA(challengeToken, code);
    saveSession(res);
  }, [saveSession]);

  const logout = useCallback(() => {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
    sessionStorage.removeItem(TOKEN_KEY);
    sessionStorage.removeItem(USER_KEY);
    setToken(null);
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, token, login, verifyLogin2FA, saveSession, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
