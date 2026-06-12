import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Server,
  Network,
  Cpu,
  Users,
  KeyRound,
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
      { to: '/clusters', icon: Server, label: 'Clusters' },
      { to: '/vps', icon: Box, label: 'VPS' },
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
      { to: '/networks', icon: Network, label: 'Networks' },
      { to: '/cloudflare-tunnels', icon: Cloud, label: 'Tunnels' },
      { to: '/ssl-certificates', icon: ShieldCheck, label: 'SSL Certs' },
    ],
  },
  {
    label: 'Platform Settings',
    items: [
      { to: '/proxmox', icon: Cpu, label: 'Proxmox' },
      { to: '/ssh-keys', icon: KeyRound, label: 'SSH Keys' },
      { to: '/users', icon: Users, label: 'Users' },
    ],
  },
  {
    label: 'Operations',
    items: [
      { to: '/monitoring', icon: BarChart3, label: 'Monitoring' },
      { to: '/commands', icon: SquareTerminal, label: 'Commands' },
      { to: '/topology', icon: GitBranch, label: 'Topology' },
      { to: '/documents', icon: BookOpen, label: 'Documents' },
    ],
  },
];

export function Sidebar() {
  return (
    <aside className="w-64 flex-shrink-0 bg-slate-900 text-slate-100 flex flex-col h-screen sticky top-0">
      {/* Logo */}
      <div className="px-6 py-5 border-b border-slate-700">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 bg-indigo-500 rounded-lg flex items-center justify-center">
            <Server className="h-4 w-4 text-white" />
          </div>
          <div>
            <p className="text-sm font-bold text-white leading-tight">K8s Proxmox</p>
            <p className="text-xs text-slate-400">Manager</p>
          </div>
        </div>
      </div>

      {/* Navigation */}
      <nav className="flex-1 px-3 py-4 overflow-y-auto">
        <div className="space-y-5">
          {navGroups.map((group) => (
            <div key={group.label}>
              <p className="px-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-slate-500">
                {group.label}
              </p>
              <div className="space-y-1">
                {group.items.map(({ to, icon: Icon, label }) => (
                  <NavLink
                    key={to}
                    to={to}
                    className={({ isActive }) =>
                      `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors ${
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
