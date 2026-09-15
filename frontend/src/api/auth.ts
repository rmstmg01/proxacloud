import client from './client';
import type { LoginResponse, LoginResult, TwoFactorStatus, TwoFactorTOTPSetup, User } from '../types';

export const login = (username: string, password: string) =>
  client.post<LoginResult>('/auth/login', { username, password }).then((r) => r.data);

export const verifyLogin2FA = (challenge_token: string, code: string) =>
  client.post<LoginResponse>('/auth/login/2fa/verify', { challenge_token, code }).then((r) => r.data);

export const resendLogin2FAEmailCode = (challenge_token: string) =>
  client.post<{ message: string }>('/auth/login/2fa/send-email-code', { challenge_token }).then((r) => r.data);

export const getMe = () =>
  client.get<User>('/auth/me').then((r) => r.data);

export const updateProfile = (body: {
  username: string;
  first_name: string;
  last_name: string;
  contact_number: string;
  current_password?: string;
}) =>
  client.put<LoginResponse>('/profile', body).then((r) => r.data);

export const changePassword = (body: {
  current_password: string;
  new_password: string;
}) =>
  client.post<{ message: string }>('/profile/password', body).then((r) => r.data);

export const register = (body: {
  username: string;
  full_name: string;
  email: string;
  contact_number: string;
  password: string;
}) =>
  client.post<{ user: User; message: string }>('/auth/register', body).then((r) => r.data);

export const resendVerification = (email: string) =>
  client.post<{ message: string }>('/auth/verify-email/resend', { email }).then((r) => r.data);

export const verifyEmail = (token: string) =>
  client.post<{ user: User; message: string }>('/auth/verify-email', { token }).then((r) => r.data);

export const requestPasswordReset = (email: string) =>
  client.post<{ message: string }>('/auth/password-reset/request', { email }).then((r) => r.data);

export const resetPassword = (token: string, new_password: string) =>
  client.post<{ message: string }>('/auth/password-reset/confirm', { token, new_password }).then((r) => r.data);

export const getTwoFactorStatus = () =>
  client.get<TwoFactorStatus>('/profile/2fa/status').then((r) => r.data);

export const requestEmailTwoFactor = (current_password: string) =>
  client.post<{ challenge_token: string; message: string }>('/profile/2fa/email/enable-request', { current_password }).then((r) => r.data);

export const confirmEmailTwoFactor = (challenge_token: string, code: string) =>
  client.post<{ message: string }>('/profile/2fa/email/enable-confirm', { challenge_token, code }).then((r) => r.data);

export const setupTOTP = (current_password: string) =>
  client.post<TwoFactorTOTPSetup>('/profile/2fa/totp/setup', { current_password }).then((r) => r.data);

export const confirmTOTP = (code: string) =>
  client.post<{ message: string }>('/profile/2fa/totp/confirm', { code }).then((r) => r.data);

export const disableTwoFactor = (current_password: string, code?: string) =>
  client.post<{ message: string }>('/profile/2fa/disable', { current_password, code }).then((r) => r.data);
