import type { ReactNode } from 'react';
import { AlertCircle, CheckCircle2, Info, AlertTriangle } from 'lucide-react';

type AlertVariant = 'info' | 'success' | 'warning' | 'error';

const config: Record<AlertVariant, { icon: React.ElementType; bg: string; text: string; border: string }> = {
  info: { icon: Info, bg: 'bg-blue-50', text: 'text-blue-800', border: 'border-blue-200' },
  success: { icon: CheckCircle2, bg: 'bg-emerald-50', text: 'text-emerald-800', border: 'border-emerald-200' },
  warning: { icon: AlertTriangle, bg: 'bg-amber-50', text: 'text-amber-800', border: 'border-amber-200' },
  error: { icon: AlertCircle, bg: 'bg-red-50', text: 'text-red-800', border: 'border-red-200' },
};

interface AlertProps {
  variant?: AlertVariant;
  title?: string;
  children: ReactNode;
  className?: string;
}

export function Alert({ variant = 'info', title, children, className = '' }: AlertProps) {
  const { icon: Icon, bg, text, border } = config[variant];
  return (
    <div className={`flex gap-3 rounded-lg border p-4 ${bg} ${border} ${className}`}>
      <Icon className={`h-5 w-5 flex-shrink-0 mt-0.5 ${text}`} />
      <div className={`text-sm ${text}`}>
        {title && <p className="font-semibold mb-1">{title}</p>}
        {children}
      </div>
    </div>
  );
}

export function ErrorMessage({ error }: { error: unknown }) {
  if (!error) return null;
  let msg = 'An unexpected error occurred.';
  if (error instanceof Error) msg = error.message;
  else if (typeof error === 'object' && error !== null && 'message' in error) {
    msg = String((error as { message: unknown }).message);
  }
  // Extract server error message if axios error
  const axiosError = error as { response?: { data?: { error?: string; message?: string } } };
  if (axiosError.response?.data?.error) msg = axiosError.response.data.error;
  else if (axiosError.response?.data?.message) msg = axiosError.response.data.message;
  return <Alert variant="error">{msg}</Alert>;
}
