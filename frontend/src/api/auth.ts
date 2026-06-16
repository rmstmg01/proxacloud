import client from './client';
import type { LoginResponse, User } from '../types';

export const login = (username: string, password: string) =>
  client.post<LoginResponse>('/auth/login', { username, password }).then((r) => r.data);

export const getMe = () =>
  client.get<User>('/auth/me').then((r) => r.data);

export const register = (body: {
  username: string;
  full_name: string;
  email: string;
  contact_number: string;
  password: string;
}) =>
  client.post<{ user: User; message: string; verification_url?: string }>('/auth/register', body).then((r) => r.data);

export const verifyEmail = (token: string) =>
  client.post<{ user: User; message: string }>('/auth/verify-email', { token }).then((r) => r.data);
