import { useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { Cloud } from 'lucide-react';
import { register, resendVerification } from '../api/auth';
import { Input } from '../components/ui/Input';
import { Button } from '../components/ui/Button';
import { Alert, ErrorMessage } from '../components/ui/Alert';

const emptyForm = {
  username: '',
  full_name: '',
  email: '',
  contact_number: '',
  password: '',
};

export function RegisterPage() {
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const [result, setResult] = useState<{ message: string } | null>(null);
  const [resendEmail, setResendEmail] = useState('');
  const [resendMessage, setResendMessage] = useState('');
  const [resending, setResending] = useState(false);
  const [resendError, setResendError] = useState<unknown>(null);

  const handleResend = async (event: FormEvent) => {
    event.preventDefault(); setResending(true); setResendError(null); setResendMessage('');
    try { setResendMessage((await resendVerification(resendEmail)).message); }
    catch (err) { setResendError(err); }
    finally { setResending(false); }
  };

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setError(null);
    setLoading(true);
    try {
      const response = await register(form);
      setResult({ message: response.message });
      setResendEmail(form.email);
      setForm(emptyForm);
    } catch (err) {
      setError(err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-900 p-4">
      <div className="w-full max-w-md">
        <div className="mb-8 text-center">
          <div className="mb-4 inline-flex h-14 w-14 items-center justify-center rounded-2xl bg-indigo-600 shadow-lg">
            <Cloud className="h-7 w-7 text-white" />
          </div>
          <h1 className="text-2xl font-bold text-white">Create ProxaCloud Account</h1>
          <p className="mt-1 text-sm text-slate-400">Verify your email, then wait for admin approval.</p>
        </div>

        <div className="rounded-2xl bg-white p-8 shadow-2xl">
          {result ? (
            <div className="space-y-4">
              <Alert variant="success" title="Registration submitted">
                {result.message}
              </Alert>
              <Link className="block text-center text-sm font-medium text-indigo-600 hover:text-indigo-700" to="/login">
                Back to sign in
              </Link>
            </div>
          ) : (
            <form onSubmit={handleSubmit} className="space-y-4">
              <Input label="Username" value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} required autoFocus />
              <Input label="Full name" value={form.full_name} onChange={(e) => setForm({ ...form, full_name: e.target.value })} required />
              <Input label="Email" type="email" value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} required />
              <Input label="Contact number" type="tel" value={form.contact_number} onChange={(e) => setForm({ ...form, contact_number: e.target.value })} />
              <Input label="Password" type="password" value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} required minLength={8} />
              {error ? <ErrorMessage error={error} /> : null}
              <Button type="submit" loading={loading} className="w-full" size="lg">Register</Button>
              <Link className="block text-center text-sm font-medium text-indigo-600 hover:text-indigo-700" to="/login">
                Already have an account?
              </Link>
            </form>
          )}
          <form onSubmit={handleResend} className="mt-6 space-y-3 border-t pt-4">
            <p className="text-sm text-slate-600">Waiting for a verification email? Request a new link.</p>
            <Input label="Verification email" type="email" value={resendEmail} onChange={(e) => setResendEmail(e.target.value)} required />
            <Button type="submit" variant="secondary" loading={resending}>Resend verification email</Button>
            {resendMessage && <Alert variant="info">{resendMessage}</Alert>}
            {resendError ? <ErrorMessage error={resendError} /> : null}
          </form>
        </div>
      </div>
    </div>
  );
}
