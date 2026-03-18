import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Play, CheckCircle, XCircle, Loader2, Clock, Pause } from 'lucide-react';
import { jobsApi, type Job } from '../api/jobs';

interface JobCardProps {
  job: Job;
}

function formatTimeAgo(dateStr: string): string {
  if (!dateStr) return 'Never';
  const diff = Date.now() - new Date(dateStr).getTime();
  const minutes = Math.floor(diff / 60000);
  if (minutes < 1) return 'Just now';
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function formatSchedule(cron: string): string {
  if (!cron) return cron;
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return cron;
  const [min, hr, dom, , dow] = parts;
  const time = `${hr.padStart(2, '0')}:${min.padStart(2, '0')}`;

  if (dom === '*' && dow === '*') return `Every day at ${time}`;
  if (dom === '*' && dow !== '*') {
    const days = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
    return `Every ${days[parseInt(dow, 10)] ?? dow} at ${time}`;
  }
  if (dom !== '*' && dow === '*') return `Monthly on day ${dom} at ${time}`;
  return cron;
}

function StatusBadge({ status }: { status: string }) {
  const configs: Record<string, { color: string; icon: React.ReactNode; label: string }> = {
    success: {
      color: 'bg-green-100 text-green-700',
      icon: <CheckCircle size={12} />,
      label: 'Success',
    },
    failed: {
      color: 'bg-red-100 text-red-700',
      icon: <XCircle size={12} />,
      label: 'Failed',
    },
    running: {
      color: 'bg-yellow-100 text-yellow-700',
      icon: <Loader2 size={12} className="animate-spin" />,
      label: 'Running',
    },
    pending: {
      color: 'bg-gray-100 text-gray-500',
      icon: <Clock size={12} />,
      label: 'Pending',
    },
    timeout: {
      color: 'bg-purple-100 text-purple-700',
      icon: <Clock size={12} />,
      label: 'Timeout',
    },
  };

  const cfg = configs[status] ?? configs['pending'];
  return (
    <span className={`inline-flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full ${cfg.color}`}>
      {cfg.icon}
      {cfg.label}
    </span>
  );
}

export default function JobCard({ job }: JobCardProps) {
  const queryClient = useQueryClient();

  const triggerMutation = useMutation({
    mutationFn: () => jobsApi.trigger(job.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] });
      queryClient.invalidateQueries({ queryKey: ['runs'] });
    },
  });

  return (
    <div className="bg-white border border-gray-200 rounded-xl p-5 transition-all hover:shadow-md hover:border-gray-300">
      <div className="flex items-start justify-between mb-3">
        <div className="min-w-0 flex-1 pr-3">
          <div className="flex items-center gap-2 mb-0.5">
            <h3 className="font-semibold text-gray-900 text-base leading-tight truncate">{job.name}</h3>
            {!job.enabled && (
              <span className="inline-flex items-center gap-1 text-xs font-medium bg-gray-100 text-gray-500 px-2 py-0.5 rounded-full shrink-0">
                <Pause size={10} />
                Disabled
              </span>
            )}
          </div>
          <p className="text-sm text-gray-400">{job.server_name}</p>
        </div>

        <button
          onClick={() => triggerMutation.mutate()}
          disabled={triggerMutation.isPending || !job.enabled}
          title="Run now"
          className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold text-blue-600 border border-blue-200 rounded-lg hover:bg-blue-50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
        >
          {triggerMutation.isPending ? (
            <Loader2 size={12} className="animate-spin" />
          ) : (
            <Play size={12} />
          )}
          Run Now
        </button>
      </div>

      <div className="flex items-center gap-1.5 text-sm text-gray-500 mb-4">
        <Clock size={13} className="text-gray-400" />
        {formatSchedule(job.schedule)}
      </div>

      <div className="flex items-center justify-between pt-3 border-t border-gray-100">
        <div className="flex items-center gap-2">
          {job.last_run ? (
            <>
              <StatusBadge status={job.last_run.status} />
              <span className="text-xs text-gray-400">{formatTimeAgo(job.last_run.started_at)}</span>
            </>
          ) : (
            <span className="text-xs text-gray-400">Never run</span>
          )}
        </div>

        <div className="flex items-center gap-1">
          <span className={`w-2 h-2 rounded-full ${job.enabled ? 'bg-green-400' : 'bg-gray-300'}`} />
          <span className="text-xs text-gray-400">{job.enabled ? 'Active' : 'Inactive'}</span>
        </div>
      </div>
    </div>
  );
}
