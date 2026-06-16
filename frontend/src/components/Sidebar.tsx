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
  SquareTerminal,
  Box,
  Shield,
  HardDrive,
} from 'lucide-react';

const navGroups = [
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
    label: 'Kubernetes Resources',
    items: [
      { to: '/configmaps', icon: FileText, label: 'ConfigMaps' },
      { to: '/secrets', icon: KeyRound, label: 'Secrets' },
      { to: '/manifests', icon: FileCode, label: 'Manifests' },
    ],
  },
  {
    label: 'Network & Access',
    items: [
      { to: '/networks', icon: Network, label: 'VPC Networks' },
      { to: '/firewalls', icon: Shield, label: 'Firewalls' },
      { to: '/cloudflare-tunnels', icon: Cloud, label: 'Tunnels' },
      { to: '/ssl-certificates', icon: ShieldCheck, label: 'SSL Certs' },
    ],
  },
  {
    label: 'Infrastructure',
    items: [
      { to: '/nodes', icon: Cpu, label: 'Nodes' },
      { to: '/storage', icon: HardDrive, label: 'Storage' },
      { to: '/backups', icon: Archive, label: 'Backups' },
    ],
  },
  {
    label: 'Operations',
    items: [
      { to: '/monitoring', icon: BarChart3, label: 'Monitoring' },
      { to: '/commands', icon: SquareTerminal, label: 'Commands' },
      { to: '/topology', icon: GitBranch, label: 'Topology' },
      { to: '/documents', icon: BookOpen, label: 'Documents' },
      { to: '/ssh-keys', icon: KeyRound, label: 'SSH Keys' },
      { to: '/users', icon: Users, label: 'Users' },
    ],
  },
];

export function Sidebar() {
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
          {navGroups.map((group) => (
            <div key={group.label}>
              <p className="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
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
