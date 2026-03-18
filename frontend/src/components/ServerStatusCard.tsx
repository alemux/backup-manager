import { useState } from 'react';
import type { ServerStatus } from '../api/dashboard';

interface Props {
  server: ServerStatus;
}

function timeAgo(isoString: string): string {
  if (!isoString) return 'never';
  const diff = Date.now() - new Date(isoString).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins} min ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function statusColor(status: string): string {
  switch (status) {
    case 'ok':
    case 'online':
      return 'bg-green-500';
    case 'warning':
      return 'bg-yellow-400';
    case 'critical':
    case 'offline':
      return 'bg-red-500';
    default:
      return 'bg-gray-400';
  }
}

function statusRing(status: string): string {
  switch (status) {
    case 'ok':
    case 'online':
      return 'ring-green-200';
    case 'warning':
      return 'ring-yellow-200';
    case 'critical':
    case 'offline':
      return 'ring-red-200';
    default:
      return 'ring-gray-200';
  }
}

function checkStatusColor(status: string): string {
  switch (status) {
    case 'ok':
      return 'text-green-600';
    case 'warning':
      return 'text-yellow-600';
    case 'critical':
      return 'text-red-600';
    default:
      return 'text-gray-500';
  }
}

export default function ServerStatusCard({ server }: Props) {
  const [showTooltip, setShowTooltip] = useState(false);

  return (
    <div
      className="relative bg-white rounded-xl border border-gray-100 shadow-sm p-4 cursor-default hover:shadow-md transition-shadow"
      onMouseEnter={() => setShowTooltip(true)}
      onMouseLeave={() => setShowTooltip(false)}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex-1 min-w-0">
          <p className="font-semibold text-gray-900 truncate">{server.name}</p>
          <div className="flex items-center gap-2 mt-1">
            <span className="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-slate-100 text-slate-600">
              {server.type}
            </span>
            <span className="text-xs text-gray-400">{timeAgo(server.last_check)}</span>
          </div>
        </div>
        <div
          className={`w-3 h-3 rounded-full shrink-0 ring-4 ${statusColor(server.status)} ${statusRing(server.status)} mt-1`}
        />
      </div>

      {/* Tooltip with check details */}
      {showTooltip && server.checks && server.checks.length > 0 && (
        <div className="absolute z-50 left-0 top-full mt-2 w-64 bg-gray-900 text-white text-xs rounded-lg shadow-xl p-3 space-y-1.5">
          <p className="font-semibold text-gray-200 mb-1">Health Checks</p>
          {server.checks.map((c) => (
            <div key={c.check_type} className="flex items-start gap-2">
              <span
                className={`w-1.5 h-1.5 rounded-full mt-1 shrink-0 ${
                  c.status === 'ok'
                    ? 'bg-green-400'
                    : c.status === 'warning'
                    ? 'bg-yellow-400'
                    : 'bg-red-400'
                }`}
              />
              <div>
                <span className={`font-medium ${checkStatusColor(c.status)}`}>
                  {c.check_type}
                </span>
                {c.value && <span className="ml-1 text-gray-400">({c.value})</span>}
                {c.message && <p className="text-gray-400 mt-0.5 leading-tight">{c.message}</p>}
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
