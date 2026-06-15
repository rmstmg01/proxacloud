import client from './client';
import type { User } from '../types';

export const getUsers = () =>
  client.get<User[]>('/users').then((r) => r.data);

export const createUser = (body: {
  username: string;
  full_name: string;
  email: string;
  contact_number: string;
  password: string;
  role: string;
}) =>
  client.post<User>('/users', body).then((r) => r.data);

export const updateUser = (
  id: number,
  body: {
    full_name: string;
    email: string;
    contact_number: string;
    role: string;
  },
) => client.put<User>(`/users/${id}`, body).then((r) => r.data);
