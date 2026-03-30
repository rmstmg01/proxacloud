import client from './client';
import type { User } from '../types';

export const getUsers = () =>
  client.get<User[]>('/users').then((r) => r.data);

export const createUser = (body: { username: string; password: string; role: string }) =>
  client.post<User>('/users', body).then((r) => r.data);
