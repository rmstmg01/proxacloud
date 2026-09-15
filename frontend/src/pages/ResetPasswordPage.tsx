import { useMemo, useState, type FormEvent } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Cloud } from 'lucide-react';
import { requestPasswordReset, resetPassword } from '../api/auth';
import { Input } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';

export function ResetPasswordPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const token = useMemo(() => params.get('token')?.trim() ?? '', [params]);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [message, setMessage] = useState('');
  const [error, setError] = useState<Error | null>(null);
  const [loading, setLoading] = useState(false);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    setMessage('');
    setLoading(true);
    try {
      if (!token) {
        const res = await requestPasswordReset(email);
        setMessage(res.message);
        return;
      }
      if (password !== confirmPassword) {
        throw new Error('Passwords do not match.');
      }
      const res = await resetPassword(token, password);
      setMessage(res.message);
      window.setTimeout(() => navigate('/login', { replace: true }), 1800);
    } catch (err) {
      setError(err as Error);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-900 p-4">
      <div className="w-full max-w-sm">
        <div className="mb-8 text-center">
          <div className="mb-4 inline-flex h-14 w-14 items-center justify-center rounded-2xl bg-indigo-600 shadow-lg">
            <Cloud className="h-7 w-7 text-white" />
          </div>
          <h1 className="text-2xl font-bold text-white">ProxaCloud</h1>
          <p className="mt-1 text-sm text-slate-400">{token ? 'Set a new password' : 'Reset your password'}</p>
        </div>

        <div className="rounded-2xl bg-white p-8 shadow-2xl">
          <form onSubmit={submit} className="space-y-5">
            {message && (
              <Alert variant="success">
                <span>{message}</span>
              </Alert>
            )}
            {!token ? (
              <Input
                label="Email or username"
                type="text"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                autoFocus
                autoComplete="username"
                required
                placeholder="you@example.com"
              />
            ) : (
              <>
                <Input
                  label="New password"
                  type="password"
                  value={password}
                  onChange={(event) => setPassword(event.target.value)}
                  autoFocus
                  autoComplete="new-password"
                  required
                  placeholder="New password"
                />
                <Input
                  label="Confirm password"
                  type="password"
                  value={confirmPassword}
                  onChange={(event) => setConfirmPassword(event.target.value)}
                  autoComplete="new-password"
                  required
                  placeholder="Confirm password"
                />
              </>
            )}
            {error && <ErrorMessage error={error} />}
            <Button type="submit" loading={loading} className="w-full" size="lg">
              {token ? 'Reset password' : 'Send reset link'}
            </Button>
            <Link className="block text-center text-sm font-medium text-indigo-600 hover:text-indigo-700" to="/login">
              Back to sign in
            </Link>
          </form>
        </div>
      </div>
    </div>
  );
}
