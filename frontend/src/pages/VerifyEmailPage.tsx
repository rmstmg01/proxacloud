import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { Cloud } from 'lucide-react';
import { verifyEmail } from '../api/auth';
import { Alert, ErrorMessage } from '../components/ui/Alert';
import { PageSpinner } from '../components/ui/Spinner';

export function VerifyEmailPage() {
  const [params] = useSearchParams();
  const [state, setState] = useState<'loading' | 'success' | 'error'>('loading');
  const [message, setMessage] = useState('');
  const [error, setError] = useState<unknown>(null);

  useEffect(() => {
    const token = params.get('token') ?? '';
    if (!token) {
      setError(new Error('Verification token is missing.'));
      setState('error');
      return;
    }
    verifyEmail(token)
      .then((result) => {
        setMessage(result.message);
        setState('success');
      })
      .catch((err) => {
        setError(err);
        setState('error');
      });
  }, [params]);

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-900 p-4">
      <div className="w-full max-w-md">
        <div className="mb-8 text-center">
          <div className="mb-4 inline-flex h-14 w-14 items-center justify-center rounded-2xl bg-indigo-600 shadow-lg">
            <Cloud className="h-7 w-7 text-white" />
          </div>
          <h1 className="text-2xl font-bold text-white">Email Verification</h1>
        </div>
        <div className="rounded-2xl bg-white p-8 shadow-2xl">
          {state === 'loading' && <PageSpinner />}
          {state === 'success' && <Alert variant="success" title="Email verified">{message}</Alert>}
          {state === 'error' && <ErrorMessage error={error} />}
          <Link className="mt-5 block text-center text-sm font-medium text-indigo-600 hover:text-indigo-700" to="/login">
            Continue to sign in
          </Link>
        </div>
      </div>
    </div>
  );
}
