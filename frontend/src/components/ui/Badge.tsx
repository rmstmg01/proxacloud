type Variant = 'default' | 'success' | 'warning' | 'danger' | 'info' | 'purple';

const classes: Record<Variant, string> = {
  default: 'bg-slate-100 text-slate-700',
  success: 'bg-emerald-100 text-emerald-700',
  warning: 'bg-amber-100 text-amber-700',
  danger: 'bg-red-100 text-red-700',
  info: 'bg-blue-100 text-blue-700',
  purple: 'bg-purple-100 text-purple-700',
};

interface BadgeProps {
  children: React.ReactNode;
  variant?: Variant;
  className?: string;
}

export function Badge({ children, variant = 'default', className = '' }: BadgeProps) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${classes[variant]} ${className}`}>
      {children}
    </span>
  );
}

export function StatusBadge({ status }: { status: string }) {
  const s = status.toLowerCase();
  let variant: Variant = 'default';
  if (s === 'running' || s === 'active' || s === 'healthy' || s === 'online' || s === 'completed' || s === 'success')
    variant = 'success';
  else if (s === 'pending' || s === 'provisioning' || s === 'bootstrapping' || s === 'upgrading' || s === 'scaling')
    variant = 'warning';
  else if (s === 'failed' || s === 'error')
    variant = 'danger';
  else if (s === 'planned' || s === 'generated')
    variant = 'info';

  return <Badge variant={variant}>{status}</Badge>;
}

export function RoleBadge({ role }: { role: string }) {
  const map: Record<string, Variant> = {
    super_admin: 'purple',
    super_user: 'info',
    normal_user: 'default',
  };
  return <Badge variant={map[role] ?? 'default'}>{role.replace('_', ' ')}</Badge>;
}
