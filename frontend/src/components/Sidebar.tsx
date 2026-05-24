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
  GitBranch,
  LogOut,
  Database,
} from 'lucide-react';
import { useAuth } from '../context/AuthContext';

const navItems = [
  { to: '/dashboard', icon: LayoutDashboard, label: 'Dashboard' },
  { to: '/clusters', icon: Server, label: 'Clusters' },
  { to: '/database-clusters', icon: Database, label: 'Databases' },
  { to: '/networks', icon: Network, label: 'Networks' },
  { to: '/proxmox', icon: Cpu, label: 'Proxmox' },
  { to: '/topology', icon: GitBranch, label: 'Topology' },
  { to: '/users', icon: Users, label: 'Users' },
  { to: '/ssh-keys', icon: KeyRound, label: 'SSH Keys' },
  { to: '/ssl-certificates', icon: ShieldCheck, label: 'SSL Certs' },
  { to: '/secrets', icon: KeyRound, label: 'Secrets' },
  { to: '/configmaps', icon: FileText, label: 'ConfigMaps' },
  { to: '/manifests', icon: FileCode, label: 'Manifests' },
];

export function Sidebar() {
  const { user, logout } = useAuth();

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
      <nav className="flex-1 px-3 py-4 space-y-1 overflow-y-auto">
        {navItems.map(({ to, icon: Icon, label }) => (
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
      </nav>

      {/* User section */}
      <div className="px-3 py-4 border-t border-slate-700">
        <div className="flex items-center gap-3 px-3 py-2 mb-1">
          <div className="w-8 h-8 bg-indigo-500 rounded-full flex items-center justify-center text-xs font-bold text-white uppercase">
            {user?.username?.[0] ?? '?'}
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-medium text-white truncate">{user?.username}</p>
            <p className="text-xs text-slate-400 truncate">{user?.role?.replace(/_/g, ' ')}</p>
          </div>
        </div>
        <button
          onClick={logout}
          className="w-full flex items-center gap-3 px-3 py-2 rounded-lg text-sm text-slate-400 hover:bg-slate-800 hover:text-white transition-colors"
        >
          <LogOut className="h-4 w-4" />
          Sign out
        </button>
      </div>
    </aside>
  );
}
