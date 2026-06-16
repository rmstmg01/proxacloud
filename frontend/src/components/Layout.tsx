import { useEffect, useRef, useState, type ReactNode } from 'react';
import { LogOut, Settings } from 'lucide-react';
import { Sidebar } from './Sidebar';
import { useAuth } from '../context/AuthContext';

interface LayoutProps {
  children: ReactNode;
}

export function Layout({ children }: LayoutProps) {
  const { user, logout } = useAuth();
  const [accountOpen, setAccountOpen] = useState(false);
  const accountRef = useRef<HTMLDivElement>(null);
  const userEmail = (user as { email?: string } | null)?.email?.trim();
  const roleLabel = user?.role?.replace(/_/g, ' ') ?? '';
  const avatarLetter = user?.username?.[0]?.toUpperCase() ?? '?';
  const showAccountGate = user?.role === 'normal_user' && user.status !== 'active';
  const statusMessage =
    user?.status === 'email_pending'
      ? 'Verify your email address before an admin can approve your account.'
      : user?.status === 'pending_approval'
        ? 'Your account is waiting for admin approval. Resource creation is blocked until approval.'
        : user?.status === 'suspended'
          ? 'Your account is suspended. Resource creation is blocked.'
          : user?.status === 'rejected'
            ? 'Your account registration was rejected. Contact support for help.'
            : '';

  useEffect(() => {
    if (!accountOpen) return;

    const onPointerDown = (event: PointerEvent) => {
      if (!accountRef.current?.contains(event.target as Node)) {
        setAccountOpen(false);
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setAccountOpen(false);
      }
    };

    document.addEventListener('pointerdown', onPointerDown);
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('pointerdown', onPointerDown);
      document.removeEventListener('keydown', onKeyDown);
    };
  }, [accountOpen]);

  return (
    <div className="flex min-h-screen bg-slate-50">
      <Sidebar />
      <main className="min-w-0 flex-1 overflow-x-hidden">
        <header className="sticky top-0 z-20 border-b border-slate-200 bg-white px-4 py-3 shadow-sm sm:px-6">
          <div className="flex w-full items-center justify-end">
            <div ref={accountRef} className="relative">
              <button
                type="button"
                onClick={() => setAccountOpen((open) => !open)}
                className="group flex h-10 w-10 items-center justify-center rounded-full bg-indigo-500 text-sm font-bold uppercase text-white shadow-sm ring-2 ring-white transition hover:bg-indigo-600 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2"
                aria-label="Account menu"
                aria-expanded={accountOpen}
                aria-haspopup="menu"
              >
                {avatarLetter}
                <span className="pointer-events-none absolute right-0 top-12 z-30 hidden w-64 rounded-md border border-slate-200 bg-white p-3 text-left normal-case text-slate-900 shadow-lg group-hover:block">
                  <span className="block truncate text-sm font-semibold">{user?.username}</span>
                  <span className="mt-0.5 block truncate text-xs capitalize text-slate-500">{roleLabel}</span>
                  <span className="mt-2 block truncate text-xs text-slate-500">{userEmail || 'No email on file'}</span>
                </span>
              </button>
              {accountOpen && (
                <div
                  className="absolute right-0 top-12 z-40 w-72 overflow-hidden rounded-md border border-slate-200 bg-white shadow-xl"
                  role="menu"
                  aria-label="Account menu"
                >
                  <div className="border-b border-slate-100 px-4 py-4">
                    <div className="flex items-center gap-3">
                      <div className="flex h-11 w-11 items-center justify-center rounded-full bg-indigo-500 text-base font-bold uppercase text-white">
                        {avatarLetter}
                      </div>
                      <div className="min-w-0">
                        <p className="truncate text-sm font-semibold text-slate-900">{user?.username}</p>
                        <p className="truncate text-xs capitalize text-slate-500">{roleLabel}</p>
                        <p className="mt-1 truncate text-xs text-slate-500">{userEmail || 'No email on file'}</p>
                      </div>
                    </div>
                  </div>
                  <div className="py-1">
                    <button
                      type="button"
                      className="flex w-full cursor-not-allowed items-center gap-3 px-4 py-2.5 text-left text-sm text-slate-400"
                      disabled
                      role="menuitem"
                    >
                      <Settings className="h-4 w-4" />
                      Profile settings
                    </button>
                  </div>
                  <div className="border-t border-slate-100 py-1">
                    <button
                      type="button"
                      onClick={() => {
                        setAccountOpen(false);
                        logout();
                      }}
                      className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm text-rose-600 transition-colors hover:bg-rose-50"
                      role="menuitem"
                    >
                      <LogOut className="h-4 w-4" />
                      Sign out
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </header>
        {showAccountGate && (
          <div className="border-b border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-800 sm:px-6">
            {statusMessage}
          </div>
        )}
        <div className="mx-auto w-full max-w-screen-2xl px-4 py-6 sm:px-6">{children}</div>
      </main>
    </div>
  );
}

export function PageHeader({
  title,
  description,
  actions,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-6 flex min-w-0 flex-wrap items-start justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-2xl font-bold text-slate-900">{title}</h1>
        {description && <p className="mt-1 text-sm text-slate-500">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-3">{actions}</div>}
    </div>
  );
}
