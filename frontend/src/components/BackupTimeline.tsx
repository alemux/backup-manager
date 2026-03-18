import type { RecentRun } from '../api/dashboard';

interface Props {
  runs: RecentRun[];
}

function runColor(status: string): string {
  switch (status) {
    case 'success':
      return 'bg-green-500';
    case 'failed':
      return 'bg-red-500';
    case 'timeout':
      return 'bg-orange-500';
    case 'running':
      return 'bg-blue-500 animate-pulse';
    default:
      return 'bg-gray-300';
  }
}

function runBorderColor(status: string): string {
  switch (status) {
    case 'success':
      return 'border-green-600';
    case 'failed':
      return 'border-red-600';
    case 'timeout':
      return 'border-orange-600';
    case 'running':
      return 'border-blue-600';
    default:
      return 'border-gray-400';
  }
}

function formatTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}

function formatSize(bytes: number): string {
  if (!bytes) return '—';
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(1)} TB`;
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`;
  return `${(bytes / 1e3).toFixed(1)} KB`;
}

export default function BackupTimeline({ runs }: Props) {
  if (runs.length === 0) {
    return (
      <div className="text-center py-8 text-gray-400 text-sm">
        No backup runs in the last 24 hours.
      </div>
    );
  }

  // Build 24h scale
  const now = Date.now();
  const windowMs = 24 * 60 * 60 * 1000;
  const start = now - windowMs;

  // Hour marks
  const hourMarks: { label: string; pct: number }[] = [];
  for (let h = 1; h <= 24; h += 3) {
    const t = start + h * 3600000;
    hourMarks.push({
      label: new Date(t).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
      pct: (h / 24) * 100,
    });
  }

  return (
    <div className="space-y-3">
      {/* Hour scale */}
      <div className="relative h-5 mb-1">
        {hourMarks.map((m) => (
          <span
            key={m.pct}
            className="absolute text-xs text-gray-400 -translate-x-1/2"
            style={{ left: `${m.pct}%` }}
          >
            {m.label}
          </span>
        ))}
      </div>

      {/* Timeline ruler */}
      <div className="relative h-1 bg-gray-100 rounded mb-2">
        {hourMarks.map((m) => (
          <div
            key={m.pct}
            className="absolute top-0 w-px h-2 bg-gray-300 -translate-y-0.5"
            style={{ left: `${m.pct}%` }}
          />
        ))}
      </div>

      {/* Run bars */}
      <div className="space-y-2">
        {runs.map((run) => {
          const runStart = run.started_at ? new Date(run.started_at).getTime() : now;
          const runEnd = run.finished_at ? new Date(run.finished_at).getTime() : now;

          const leftPct = Math.max(0, ((runStart - start) / windowMs) * 100);
          const widthPct = Math.max(0.5, ((runEnd - runStart) / windowMs) * 100);
          const clampedWidth = Math.min(widthPct, 100 - leftPct);

          return (
            <div key={run.id} className="relative group">
              {/* Job label */}
              <div className="flex items-center justify-between text-xs text-gray-500 mb-0.5 px-0.5">
                <span className="font-medium text-gray-700 truncate max-w-[45%]">
                  {run.job_name}
                </span>
                <span className="text-gray-400 truncate max-w-[45%] text-right">
                  {run.server_name}
                </span>
              </div>

              {/* Timeline track */}
              <div className="relative h-5 bg-gray-50 rounded border border-gray-100">
                <div
                  className={`absolute top-0 h-full rounded border ${runColor(run.status)} ${runBorderColor(run.status)} opacity-85`}
                  style={{
                    left: `${leftPct}%`,
                    width: `${clampedWidth}%`,
                    minWidth: '3px',
                  }}
                />

                {/* Tooltip on hover */}
                <div className="absolute z-50 hidden group-hover:flex flex-col bg-gray-900 text-white text-xs rounded-lg shadow-xl p-2.5 gap-0.5 min-w-[160px] -top-24 left-0">
                  <span className="font-semibold">{run.job_name}</span>
                  <span className="text-gray-400">{run.server_name}</span>
                  <span className="capitalize">{run.status}</span>
                  <span className="text-gray-400">
                    {formatTime(run.started_at)} – {run.finished_at ? formatTime(run.finished_at) : 'running'}
                  </span>
                  <span className="text-gray-400">{formatSize(run.total_size_bytes)}</span>
                </div>
              </div>
            </div>
          );
        })}
      </div>

      {/* Legend */}
      <div className="flex items-center gap-4 pt-1">
        {[
          { label: 'Success', color: 'bg-green-500' },
          { label: 'Failed', color: 'bg-red-500' },
          { label: 'Running', color: 'bg-blue-500' },
          { label: 'Timeout', color: 'bg-orange-500' },
        ].map((l) => (
          <div key={l.label} className="flex items-center gap-1.5 text-xs text-gray-500">
            <div className={`w-2.5 h-2.5 rounded-sm ${l.color}`} />
            {l.label}
          </div>
        ))}
      </div>
    </div>
  );
}
