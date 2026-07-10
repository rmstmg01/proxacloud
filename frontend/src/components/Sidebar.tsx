import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Server,
  Network,
  Cpu,
  Monitor,
  Users,
  KeyRound,
  Archive,
  ShieldCheck,
  FileText,
  FileCode,
  BookOpen,
  GitBranch,
  Database,
  Cloud,
  BarChart3,
  CreditCard,
  SquareTerminal,
  Box,
  Shield,
  HardDrive,
  LockKeyhole,
  MapPin,
} from 'lucide-react';
import { useAuth } from '../context/AuthContext';
import type { Role } from '../types';

type NavItem = {
  to: string;
  icon: typeof LayoutDashboard;
  label: string;
  roles?: Role[];
};

type NavGroup = {
  label: string;
  items: NavItem[];
};

const adminRoles: Role[] = ['super_user', 'super_admin'];

const navGroups: NavGroup[] = [
  {
    label: 'Main',
    items: [
      { to: '/dashboard', icon: LayoutDashboard, label: 'Dashboard' },
    ],
  },
  {
    label: 'Compute',
    items: [
      { to: '/vps', icon: Monitor, label: 'VPS' },
      { to: '/containers', icon: Box, label: 'Container' },
      { to: '/clusters', icon: Server, label: 'Clusters' },
      { to: '/database-clusters', icon: Database, label: 'Databases' },
    ],
  },
  {
    label: 'Network & Access',
    items: [
      { to: '/regions', icon: MapPin, label: 'Region', roles: adminRoles },
      { to: '/networks', icon: Network, label: 'VPC Networks', roles: adminRoles },
      { to: '/firewalls', icon: Shield, label: 'Firewalls', roles: adminRoles },
      { to: '/ssl-certificates', icon: ShieldCheck, label: 'SSL Certs', roles: adminRoles },
    ],
  },
  {
    label: 'Zero Trust',
    items: [
      { to: '/cloudflare-tunnels', icon: Cloud, label: 'Cloudflare Tunnels', roles: adminRoles },
      { to: '/proxacloud-tunnels', icon: Cloud, label: 'Proxacloud Tunnels', roles: adminRoles },
    ],
  },
  {
    label: 'Infrastructure',
    items: [
      { to: '/nodes', icon: Cpu, label: 'Nodes', roles: adminRoles },
      { to: '/storage', icon: HardDrive, label: 'Storage', roles: adminRoles },
      { to: '/backups', icon: Archive, label: 'Backups' },
    ],
  },
  {
    label: 'Operations',
    items: [
      { to: '/billing', icon: CreditCard, label: 'Billing' },
      { to: '/monitoring', icon: BarChart3, label: 'Monitoring', roles: adminRoles },
      { to: '/commands', icon: SquareTerminal, label: 'Commands', roles: adminRoles },
      { to: '/topology', icon: GitBranch, label: 'Topology', roles: adminRoles },
      { to: '/documents', icon: BookOpen, label: 'Documents' },
      { to: '/ssh-keys', icon: KeyRound, label: 'SSH Keys', roles: adminRoles },
      { to: '/users', icon: Users, label: 'Users', roles: adminRoles },
    ],
  },
  {
    label: 'Kubernetes Resources',
    items: [
      { to: '/configmaps', icon: FileText, label: 'ConfigMaps', roles: adminRoles },
      { to: '/secrets', icon: KeyRound, label: 'Secrets', roles: adminRoles },
      { to: '/image-pull-secrets', icon: LockKeyhole, label: 'Pull Secrets', roles: adminRoles },
      { to: '/manifests', icon: FileCode, label: 'Manifests', roles: adminRoles },
    ],
  },
];

export function Sidebar() {
  const { user } = useAuth();
  const visibleGroups = navGroups
    .map((group) => ({
      ...group,
      items: group.items.filter((item) => !item.roles || (user && item.roles.includes(user.role))),
    }))
    .filter((group) => group.items.length > 0);

  return (
    <aside className="w-60 flex-shrink-0 bg-slate-900 text-slate-100 flex flex-col h-screen sticky top-0">
      {/* Logo */}
      <div className="border-b border-slate-700 px-4 py-3">
        <div className="flex items-center gap-2.5">
          <div className="flex h-10 w-10 items-center justify-center rounded-md bg-indigo-500">
            <Cloud className="h-5 w-5 text-white" />
          </div>
          <div className="min-w-0">
            <p className="truncate text-sm font-semibold leading-tight text-white">ProxaCloud</p>
            <p className="truncate text-[11px] leading-tight text-slate-400">Private cloud</p>
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="scrollbar-hidden flex-1 overflow-y-auto px-3 py-3">
        <div className="space-y-4">
          {visibleGroups.map((group) => (
            <div key={group.label}>
              <p className="mb-2 rounded-md border border-slate-700/70 bg-slate-800/70 px-3 py-1.5 text-xs font-bold uppercase tracking-wide text-slate-100 shadow-sm">
                {group.label}
              </p>
              <div className="space-y-1">
                {group.items.map(({ to, icon: Icon, label }) => (
                  <NavLink
                    key={to}
                    to={to}
                    className={({ isActive }) =>
                      `flex items-center gap-2.5 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors ${
                        isActive
                          ? 'bg-indigo-600 text-white'
                          : 'text-slate-300 hover:bg-slate-800 hover:text-white'
                      }`
                    }
                  >
                    <Icon className="h-4 w-4 flex-shrink-0" />
                    {label}
                  </NavLink>
                ))}
              </div>
            </div>
          ))}
        </div>
      </nav>

    </aside>
  );
}
