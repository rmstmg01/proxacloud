import { useEffect, useState } from 'react';
import { KeyRound, Mail, QrCode, ShieldCheck, ShieldOff } from 'lucide-react';
import { useAuth } from '../context/AuthContext';
import {
  changePassword,
  confirmEmailTwoFactor,
  confirmTOTP,
  disableTwoFactor,
  getTwoFactorStatus,
  requestEmailTwoFactor,
  setupTOTP,
  updateProfile,
} from '../api/auth';
import type { TwoFactorStatus, TwoFactorTOTPSetup } from '../types';
import { Layout, PageHeader } from '../components/Layout';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { Badge } from '../components/ui/Badge';
import { Button } from '../components/ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../components/ui/Card';
import { Input } from '../components/ui/Input';

export function ProfileSettingsPage() {
  const { user, saveSession } = useAuth();
  const [status, setStatus] = useState<TwoFactorStatus>({ enabled: false, method: 'none' });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  const [success, setSuccess] = useState('');

  const [emailPassword, setEmailPassword] = useState('');
  const [emailCode, setEmailCode] = useState('');
  const [emailChallenge, setEmailChallenge] = useState('');

  const [totpPassword, setTotpPassword] = useState('');
  const [totpCode, setTotpCode] = useState('');
  const [totpSetup, setTotpSetup] = useState<TwoFactorTOTPSetup | null>(null);

  const [disablePassword, setDisablePassword] = useState('');
  const [disableCode, setDisableCode] = useState('');

  const [profileForm, setProfileForm] = useState({
    username: '',
    first_name: '',
    last_name: '',
    contact_number: '',
    current_password: '',
  });
  const [passwordForm, setPasswordForm] = useState({
    current_password: '',
    new_password: '',
    confirm_password: '',
  });

  const loadStatus = async () => {
    setLoading(true);
    try {
      setStatus(await getTwoFactorStatus());
    } catch (err) {
      setError(err as Error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadStatus();
  }, []);

  useEffect(() => {
    if (!user) return;
    const names = splitName(user.full_name);
    setProfileForm({
      username: user.username || '',
      first_name: user.first_name || names.firstName,
      last_name: user.last_name || names.lastName,
      contact_number: user.contact_number || '',
      current_password: '',
    });
  }, [user]);

  const resetMessages = () => {
    setError(null);
    setSuccess('');
  };

  const beginEmailSetup = async () => {
    resetMessages();
    setSaving(true);
    try {
      const res = await requestEmailTwoFactor(emailPassword);
      setEmailChallenge(res.challenge_token);
      setSuccess(res.message);
    } catch (err) {
      setError(err as Error);
    } finally {
      setSaving(false);
    }
  };

  const finishEmailSetup = async () => {
    resetMessages();
    setSaving(true);
    try {
      const res = await confirmEmailTwoFactor(emailChallenge, emailCode);
      setSuccess(res.message);
      setEmailPassword('');
      setEmailCode('');
      setEmailChallenge('');
      await loadStatus();
    } catch (err) {
      setError(err as Error);
    } finally {
      setSaving(false);
    }
  };

  const beginTOTPSetup = async () => {
    resetMessages();
    setSaving(true);
    try {
      const res = await setupTOTP(totpPassword);
      setTotpSetup(res);
      setSuccess('Scan the QR code and enter your authenticator code to confirm.');
    } catch (err) {
      setError(err as Error);
    } finally {
      setSaving(false);
    }
  };

  const finishTOTPSetup = async () => {
    resetMessages();
    setSaving(true);
    try {
      const res = await confirmTOTP(totpCode);
      setSuccess(res.message);
      setTotpPassword('');
      setTotpCode('');
      setTotpSetup(null);
      await loadStatus();
    } catch (err) {
      setError(err as Error);
    } finally {
      setSaving(false);
    }
  };

  const disable = async () => {
    resetMessages();
    setSaving(true);
    try {
      const res = await disableTwoFactor(disablePassword, disableCode);
      setSuccess(res.message);
      setDisablePassword('');
      setDisableCode('');
      await loadStatus();
    } catch (err) {
      setError(err as Error);
    } finally {
      setSaving(false);
    }
  };

  const saveProfile = async () => {
    resetMessages();
    setSaving(true);
    try {
      const res = await updateProfile(profileForm);
      saveSession(res);
      setProfileForm((prev) => ({ ...prev, current_password: '' }));
      setSuccess('Profile updated successfully');
    } catch (err) {
      setError(err as Error);
    } finally {
      setSaving(false);
    }
  };

  const savePassword = async () => {
    resetMessages();
    if (passwordForm.new_password !== passwordForm.confirm_password) {
      setError(new Error('New password and confirmation do not match'));
      return;
    }
    setSaving(true);
    try {
      const res = await changePassword({
        current_password: passwordForm.current_password,
        new_password: passwordForm.new_password,
      });
      setSuccess(res.message);
      setPasswordForm({ current_password: '', new_password: '', confirm_password: '' });
    } catch (err) {
      setError(err as Error);
    } finally {
      setSaving(false);
    }
  };

  const methodLabel = status.method === 'totp' ? 'Authenticator app' : status.method === 'email' ? 'Email code' : 'None';

  return (
    <Layout>
      <PageHeader title="Profile Settings" description="Manage your account security settings." />

      <div className="space-y-6">
        {error && <ErrorMessage error={error} />}
        {success && <Alert variant="success">{success}</Alert>}

        <Card>
          <CardHeader>
            <CardTitle>Account details</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-4 lg:grid-cols-2">
              <Input
                label="Username"
                value={profileForm.username}
                onChange={(e) => setProfileForm({ ...profileForm, username: e.target.value })}
                autoComplete="username"
              />
              <Input label="Email" type="email" value={user?.email || ''} readOnly helperText="Email cannot be changed here." />
              <Input
                label="First name"
                value={profileForm.first_name}
                onChange={(e) => setProfileForm({ ...profileForm, first_name: e.target.value })}
                autoComplete="given-name"
              />
              <Input
                label="Last name"
                value={profileForm.last_name}
                onChange={(e) => setProfileForm({ ...profileForm, last_name: e.target.value })}
                autoComplete="family-name"
              />
              <Input
                label="Contact number"
                type="tel"
                value={profileForm.contact_number}
                onChange={(e) => setProfileForm({ ...profileForm, contact_number: e.target.value })}
                autoComplete="tel"
              />
              <Input
                label="Current password"
                type="password"
                value={profileForm.current_password}
                onChange={(e) => setProfileForm({ ...profileForm, current_password: e.target.value })}
                autoComplete="current-password"
                helperText="Required only when changing username."
              />
            </div>
            <Button type="button" onClick={saveProfile} loading={saving} disabled={!profileForm.username.trim()}>
              Save profile
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Password</CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-end">
            <Input
              label="Current password"
              type="password"
              value={passwordForm.current_password}
              onChange={(e) => setPasswordForm({ ...passwordForm, current_password: e.target.value })}
              autoComplete="current-password"
            />
            <Input
              label="New password"
              type="password"
              value={passwordForm.new_password}
              onChange={(e) => setPasswordForm({ ...passwordForm, new_password: e.target.value })}
              autoComplete="new-password"
              minLength={8}
            />
            <Input
              label="Confirm password"
              type="password"
              value={passwordForm.confirm_password}
              onChange={(e) => setPasswordForm({ ...passwordForm, confirm_password: e.target.value })}
              autoComplete="new-password"
              minLength={8}
            />
            <Button
              type="button"
              variant="secondary"
              onClick={savePassword}
              loading={saving}
              disabled={!passwordForm.current_password || passwordForm.new_password.length < 8 || !passwordForm.confirm_password}
            >
              Change password
            </Button>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <div className="flex flex-wrap items-center justify-between gap-3">
              <CardTitle>Two-factor authentication</CardTitle>
              <Badge variant={status.enabled ? 'success' : 'default'}>{loading ? 'Loading' : status.enabled ? 'Enabled' : 'Disabled'}</Badge>
            </div>
          </CardHeader>
          <CardContent>
            <div className="grid gap-4 text-sm text-slate-600 sm:grid-cols-3">
              <div>
                <p className="font-medium text-slate-900">Username</p>
                <p className="mt-1">{user?.username}</p>
              </div>
              <div>
                <p className="font-medium text-slate-900">Email</p>
                <p className="mt-1">{user?.email || 'No email on file'}</p>
              </div>
              <div>
                <p className="font-medium text-slate-900">Method</p>
                <p className="mt-1">{methodLabel}</p>
              </div>
            </div>
          </CardContent>
        </Card>

        <div className="grid gap-6 xl:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Mail className="h-4 w-4 text-indigo-600" />
                Email code
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <Input
                label="Current password"
                type="password"
                value={emailPassword}
                onChange={(e) => setEmailPassword(e.target.value)}
                autoComplete="current-password"
              />
              <Button type="button" onClick={beginEmailSetup} loading={saving} disabled={!emailPassword}>
                <ShieldCheck className="h-4 w-4" />
                Send setup code
              </Button>
              {emailChallenge && (
                <div className="space-y-4 border-t border-slate-200 pt-4">
                  <Input
                    label="Email verification code"
                    type="text"
                    inputMode="numeric"
                    value={emailCode}
                    onChange={(e) => setEmailCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    autoComplete="one-time-code"
                    placeholder="123456"
                  />
                  <Button type="button" onClick={finishEmailSetup} loading={saving} disabled={emailCode.length !== 6}>
                    Confirm email 2FA
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <QrCode className="h-4 w-4 text-indigo-600" />
                Authenticator app
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-4">
              <Input
                label="Current password"
                type="password"
                value={totpPassword}
                onChange={(e) => setTotpPassword(e.target.value)}
                autoComplete="current-password"
              />
              <Button type="button" variant="secondary" onClick={beginTOTPSetup} loading={saving} disabled={!totpPassword}>
                <QrCode className="h-4 w-4" />
                Generate QR setup
              </Button>
              {totpSetup && (
                <div className="space-y-4 border-t border-slate-200 pt-4">
                  <img src={totpSetup.qr_code} alt="2FA QR code" className="h-[180px] w-[180px] rounded-md border border-slate-200" />
                  <Input label="Manual key" value={totpSetup.secret} readOnly />
                  <Input
                    label="Authenticator code"
                    type="text"
                    inputMode="numeric"
                    value={totpCode}
                    onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                    autoComplete="one-time-code"
                    placeholder="123456"
                  />
                  <Button type="button" onClick={finishTOTPSetup} loading={saving} disabled={totpCode.length !== 6}>
                    <KeyRound className="h-4 w-4" />
                    Confirm authenticator 2FA
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <ShieldOff className="h-4 w-4 text-red-600" />
              Disable 2FA
            </CardTitle>
          </CardHeader>
          <CardContent className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] lg:items-end">
            <Input
              label="Current password"
              type="password"
              value={disablePassword}
              onChange={(e) => setDisablePassword(e.target.value)}
              autoComplete="current-password"
            />
            <Input
              label="Authenticator code"
              type="text"
              inputMode="numeric"
              value={disableCode}
              onChange={(e) => setDisableCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              placeholder={status.method === 'totp' ? '123456' : 'Optional'}
              disabled={status.method !== 'totp'}
            />
            <Button type="button" variant="danger" onClick={disable} loading={saving} disabled={!status.enabled || !disablePassword}>
              Disable
            </Button>
          </CardContent>
        </Card>
      </div>
    </Layout>
  );
}

function splitName(fullName: string | undefined): { firstName: string; lastName: string } {
  const parts = (fullName || '').trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return { firstName: '', lastName: '' };
  return { firstName: parts[0], lastName: parts.slice(1).join(' ') };
}
