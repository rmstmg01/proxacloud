import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { Cloud, KeyRound, Mail } from 'lucide-react';
import { useAuth } from '../context/AuthContext';
import { resendLogin2FAEmailCode } from '../api/auth';
import { Input } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';

export function LoginPage() {
  const { login, verifyLogin2FA } = useAuth();
  const navigate = useNavigate();
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [twoFactorCode, setTwoFactorCode] = useState('');
  const [challengeToken, setChallengeToken] = useState('');
  const [twoFactorMethod, setTwoFactorMethod] = useState<'email' | 'totp'>('email');
  const [mode, setMode] = useState<'password' | '2fa'>('password');
  const [message, setMessage] = useState('');
  const [loading, setLoading] = useState(false);
  const [resending, setResending] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    try {
      if (mode === '2fa') {
        await verifyLogin2FA(challengeToken, twoFactorCode);
        navigate('/dashboard', { replace: true });
        return;
      }

      const res = await login(username, password);
      if ('requires_2fa' in res && res.requires_2fa) {
        setChallengeToken(res.challenge_token);
        setTwoFactorMethod(res.method);
        setTwoFactorCode('');
        setMode('2fa');
        setMessage(res.method === 'totp' ? 'Enter the code from your authenticator app.' : 'A verification code has been sent to your email.');
        return;
      }

      navigate('/dashboard', { replace: true });
    } catch (err) {
      setError(err as Error);
    } finally {
      setLoading(false);
    }
  };

  const resendCode = async () => {
    setError(null);
    setResending(true);
    try {
      const res = await resendLogin2FAEmailCode(challengeToken);
      setMessage(res.message || 'Verification code sent.');
    } catch (err) {
      setError(err as Error);
    } finally {
      setResending(false);
    }
  };

  return (
    <div className="min-h-screen bg-slate-900 flex items-center justify-center p-4">
      <div className="w-full max-w-sm">
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-14 h-14 bg-indigo-600 rounded-2xl mb-4 shadow-lg">
            <Cloud className="h-7 w-7 text-white" />
          </div>
          <h1 className="text-2xl font-bold text-white">ProxaCloud</h1>
          <p className="text-slate-400 text-sm mt-1">Private cloud control plane</p>
        </div>

        <div className="bg-white rounded-2xl shadow-2xl p-8">
          <form onSubmit={handleSubmit} className="space-y-5">
            {mode === 'password' ? (
              <>
                <Input
                  label="Username or email"
                  type="text"
                  value={username}
                  onChange={(e) => setUsername(e.target.value)}
                  autoFocus
                  autoComplete="username"
                  required
                  placeholder="Username or email address"
                />
                <Input
                  label="Password"
                  type="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="current-password"
                  required
                  placeholder="Password"
                />
                <Link className="block text-right text-sm font-medium text-indigo-600 hover:text-indigo-700" to="/reset-password">
                  Forgot password?
                </Link>
              </>
            ) : (
              <>
                <Alert variant="info">
                  <span className="inline-flex items-center gap-2">
                    {twoFactorMethod === 'totp' ? <KeyRound className="h-4 w-4" /> : <Mail className="h-4 w-4" />}
                    {message}
                  </span>
                </Alert>
                <Input
                  label={twoFactorMethod === 'totp' ? 'Authenticator code' : 'Email verification code'}
                  type="text"
                  inputMode="numeric"
                  value={twoFactorCode}
                  onChange={(e) => setTwoFactorCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
                  autoFocus
                  autoComplete="one-time-code"
                  required
                  placeholder="123456"
                />
                {twoFactorMethod === 'email' && (
                  <Button type="button" variant="ghost" className="w-full" onClick={resendCode} loading={resending}>
                    Resend code
                  </Button>
                )}
              </>
            )}
            {error && <ErrorMessage error={error} />}
            <Button type="submit" loading={loading} className="w-full" size="lg">
              {mode === '2fa' ? 'Verify & sign in' : 'Sign in'}
            </Button>
            {mode === '2fa' ? (
              <button
                type="button"
                className="block w-full text-center text-sm font-medium text-indigo-600 hover:text-indigo-700"
                onClick={() => {
                  setMode('password');
                  setChallengeToken('');
                  setTwoFactorCode('');
                  setError(null);
                }}
              >
                Back to password
              </button>
            ) : (
              <Link className="block text-center text-sm font-medium text-indigo-600 hover:text-indigo-700" to="/register">
                Create a client account
              </Link>
            )}
          </form>
        </div>

      </div>
    </div>
  );
}
