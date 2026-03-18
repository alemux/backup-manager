import { NavLink } from 'react-router-dom';
import {
  LayoutDashboard,
  Server,
  Calendar,
  Archive,
  LifeBuoy,
  BookOpen,
  Bot,
  Settings,
  ScrollText,
} from 'lucide-react';

const navItems = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, exact: true },
  { to: '/servers', label: 'Servers', icon: Server },
  { to: '/jobs', label: 'Jobs', icon: Calendar },
  { to: '/snapshots', label: 'Snapshots', icon: Archive },
  { to: '/recovery', label: 'Recovery', icon: LifeBuoy },
  { to: '/docs', label: 'Docs', icon: BookOpen },
  { to: '/assistant', label: 'Assistant', icon: Bot },
  { to: '/settings', label: 'Settings', icon: Settings },
  { to: '/audit', label: 'Audit Log', icon: ScrollText },
];

export default function Sidebar() {
  return (
    <aside className="w-56 min-h-screen bg-slate-900 flex flex-col border-r border-slate-700">
      <div className="px-5 py-5 border-b border-slate-700">
        <span className="text-white font-semibold text-lg tracking-tight">
          BackupManager
        </span>
      </div>
      <nav className="flex-1 px-3 py-4 space-y-1">
        {navItems.map(({ to, label, icon: Icon, exact }) => (
          <NavLink
            key={to}
            to={to}
            end={exact}
            className={({ isActive }) =>
              [
                'flex items-center gap-3 px-3 py-2 rounded-md text-sm font-medium transition-colors',
                isActive
                  ? 'bg-slate-700 text-white'
                  : 'text-slate-400 hover:text-white hover:bg-slate-800',
              ].join(' ')
            }
          >
            <Icon size={17} strokeWidth={1.75} />
            {label}
          </NavLink>
        ))}
      </nav>
      <div className="px-5 py-4 border-t border-slate-700">
        <p className="text-slate-500 text-xs">v0.1.0</p>
      </div>
    </aside>
  );
}
