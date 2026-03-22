import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { Play, CheckCircle, XCircle, Loader2, Clock, Pause, Search, X, HardDrive, FileText } from 'lucide-react';
import { jobsApi, type Job, type AnalysisResult } from '../api/jobs';

interface JobCardProps {
  job: Job;
  onRunNow?: () => void;
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

const sourceTypeIcons: Record<string, React.ReactNode> = {
  web: <HardDrive size={14} className="text-blue-500" />,
  config: <FileText size={14} className="text-amber-500" />,
  database: <HardDrive size={14} className="text-purple-500" />,
};

export default function JobCard({ job, onRunNow }: JobCardProps) {
  const queryClient = useQueryClient();
  const [analysisResult, setAnalysisResult] = useState<AnalysisResult | null>(null);
  const [showAnalysis, setShowAnalysis] = useState(false);

  const triggerMutation = useMutation({
    mutationFn: () => jobsApi.trigger(job.id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] });
      queryClient.invalidateQueries({ queryKey: ['runs'] });
      setShowAnalysis(false);
      setAnalysisResult(null);
      onRunNow?.();
    },
  });

  const analyzeMutation = useMutation({
    mutationFn: () => jobsApi.analyze(job.id),
    onSuccess: (data) => {
      setAnalysisResult(data);
      setShowAnalysis(true);
    },
  });

  return (
    <>
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

          <div className="flex items-center gap-1.5 shrink-0">
            <button
              onClick={() => analyzeMutation.mutate()}
              disabled={analyzeMutation.isPending || !job.enabled}
              title="Analyze transfer size"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold text-amber-600 border border-amber-200 rounded-lg hover:bg-amber-50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {analyzeMutation.isPending ? (
                <Loader2 size={12} className="animate-spin" />
              ) : (
                <Search size={12} />
              )}
              {analyzeMutation.isPending ? 'Analyzing...' : 'Analyze'}
            </button>

            <button
              onClick={() => triggerMutation.mutate()}
              disabled={triggerMutation.isPending || !job.enabled}
              title="Run now"
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold text-blue-600 border border-blue-200 rounded-lg hover:bg-blue-50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {triggerMutation.isPending ? (
                <Loader2 size={12} className="animate-spin" />
              ) : (
                <Play size={12} />
              )}
              Run Now
            </button>
          </div>
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

      {/* Analysis Results Modal */}
      {showAnalysis && analysisResult && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={() => setShowAnalysis(false)}>
          <div className="bg-white rounded-2xl shadow-2xl w-full max-w-lg mx-4 overflow-hidden" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100">
              <h2 className="text-lg font-semibold text-gray-900">Analysis: {job.name}</h2>
              <button onClick={() => setShowAnalysis(false)} className="p-1 rounded-lg hover:bg-gray-100 transition-colors">
                <X size={18} className="text-gray-400" />
              </button>
            </div>

            <div className="px-6 py-4 space-y-3 max-h-80 overflow-y-auto">
              {analysisResult.sources.map((src) => (
                <div key={src.source_id} className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
                  <div className="flex items-center gap-2 min-w-0">
                    {sourceTypeIcons[src.source_type] ?? <HardDrive size={14} className="text-gray-400" />}
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-gray-800 truncate">{src.source_name}</p>
                      <p className="text-xs text-gray-400">{src.source_type}</p>
                    </div>
                  </div>
                  <div className="text-right shrink-0 ml-3">
                    {src.error ? (
                      <p className="text-xs text-red-500">{src.error}</p>
                    ) : (
                      <>
                        <p className="text-sm font-semibold text-gray-800">{src.human_size}</p>
                        <p className="text-xs text-gray-400">
                          {src.files_to_transfer} file{src.files_to_transfer !== 1 ? 's' : ''} to transfer
                        </p>
                        <p className="text-xs text-gray-400">{src.human_total} total on server</p>
                      </>
                    )}
                  </div>
                </div>
              ))}
            </div>

            <div className="px-6 py-4 bg-gray-50 border-t border-gray-100">
              <div className="flex items-center justify-between mb-3">
                <span className="text-sm text-gray-600">Total to transfer</span>
                <span className="text-sm font-bold text-gray-900">
                  {analysisResult.total_files_to_transfer} file{analysisResult.total_files_to_transfer !== 1 ? 's' : ''}, {analysisResult.human_total_transfer}
                </span>
              </div>
              <div className="flex items-center justify-between mb-4">
                <span className="text-sm text-gray-600">Total on server</span>
                <span className="text-sm text-gray-500">{analysisResult.human_total_all}</span>
              </div>
              <button
                onClick={() => {
                  triggerMutation.mutate();
                }}
                disabled={triggerMutation.isPending}
                className="w-full inline-flex items-center justify-center gap-2 px-4 py-2.5 text-sm font-semibold text-white bg-blue-600 rounded-lg hover:bg-blue-700 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                {triggerMutation.isPending ? (
                  <Loader2 size={14} className="animate-spin" />
                ) : (
                  <Play size={14} />
                )}
                Run Backup Now
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
