import { useState } from 'react';
import { AlertCircle, AlertTriangle, Info, X } from 'lucide-react';
import type { Alert } from '../api/dashboard';

interface Props {
  alerts: Alert[];
}

function timeAgo(isoString: string): string {
  if (!isoString) return '';
  const diff = Date.now() - new Date(isoString).getTime();
  const mins = Math.floor(diff / 60000);
  if (mins < 1) return 'just now';
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function SeverityIcon({ severity }: { severity: Alert['severity'] }) {
  switch (severity) {
    case 'critical':
      return <AlertCircle className="w-4 h-4 text-red-500 shrink-0" />;
    case 'warning':
      return <AlertTriangle className="w-4 h-4 text-yellow-500 shrink-0" />;
    default:
      return <Info className="w-4 h-4 text-blue-500 shrink-0" />;
  }
}

function severityBg(severity: Alert['severity']): string {
  switch (severity) {
    case 'critical':
      return 'bg-red-50 border-red-100';
    case 'warning':
      return 'bg-yellow-50 border-yellow-100';
    default:
      return 'bg-blue-50 border-blue-100';
  }
}

const SEVERITY_ORDER: Record<Alert['severity'], number> = { critical: 0, warning: 1, info: 2 };

export default function AlertsList({ alerts }: Props) {
  const [dismissed, setDismissed] = useState<Set<string>>(new Set());

  const visible = alerts
    .filter((a) => !dismissed.has(a.id))
    .sort((a, b) => {
      const sev = SEVERITY_ORDER[a.severity] - SEVERITY_ORDER[b.severity];
      if (sev !== 0) return sev;
      return new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime();
    });

  if (visible.length === 0) {
    return (
      <div className="text-center py-6 text-gray-400 text-sm">
        <Info className="w-6 h-6 mx-auto mb-2 text-gray-300" />
        No active alerts
      </div>
    );
  }

  return (
    <div className="space-y-2 max-h-72 overflow-y-auto pr-1">
      {visible.map((alert) => (
        <div
          key={alert.id}
          className={`flex items-start gap-3 rounded-lg border p-3 ${severityBg(alert.severity)}`}
        >
          <SeverityIcon severity={alert.severity} />
          <div className="flex-1 min-w-0">
            <div className="flex items-center justify-between gap-2">
              <p className="text-sm font-medium text-gray-800 truncate">{alert.title}</p>
              <span className="text-xs text-gray-400 shrink-0">{timeAgo(alert.timestamp)}</span>
            </div>
            <p className="text-xs text-gray-500 mt-0.5 truncate">{alert.message}</p>
            {alert.server_name && (
              <p className="text-xs text-gray-400 mt-0.5">
                Server: <span className="font-medium">{alert.server_name}</span>
              </p>
            )}
          </div>
          <button
            onClick={() => setDismissed((prev) => new Set([...prev, alert.id]))}
            className="text-gray-400 hover:text-gray-600 transition-colors shrink-0"
            title="Dismiss"
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      ))}
    </div>
  );
}
