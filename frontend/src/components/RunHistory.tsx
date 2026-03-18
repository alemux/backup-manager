import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  CheckCircle,
  XCircle,
  Loader2,
  Clock,
  ChevronLeft,
  ChevronRight,
  AlertTriangle,
  FileText,
} from 'lucide-react';
import { jobsApi, type BackupRun, type RunsResponse } from '../api/jobs';

interface RunHistoryProps {
  jobId?: number;
}

function formatDuration(started: string, finished: string): string {
  if (!started || !finished) return '—';
  const ms = new Date(finished).getTime() - new Date(started).getTime();
  if (ms < 0) return '—';
  const secs = Math.floor(ms / 1000);
  if (secs < 60) return `${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ${secs % 60}s`;
  return `${Math.floor(mins / 60)}h ${mins % 60}m`;
}

function formatSize(bytes: number): string {
  if (!bytes) return '—';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 ** 2) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 ** 3) return `${(bytes / 1024 ** 2).toFixed(1)} MB`;
  return `${(bytes / 1024 ** 3).toFixed(2)} GB`;
}

function formatDate(dateStr: string): string {
  if (!dateStr) return '—';
  return new Date(dateStr).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case 'success':
      return <CheckCircle size={16} className="text-green-500" />;
    case 'failed':
      return <XCircle size={16} className="text-red-500" />;
    case 'running':
      return <Loader2 size={16} className="text-yellow-500 animate-spin" />;
    case 'pending':
      return <Clock size={16} className="text-gray-400" />;
    case 'timeout':
      return <AlertTriangle size={16} className="text-purple-500" />;
    default:
      return <Clock size={16} className="text-gray-400" />;
  }
}

function StatusLabel({ status }: { status: string }) {
  const colors: Record<string, string> = {
    success: 'text-green-700',
    failed: 'text-red-700',
    running: 'text-yellow-700',
    pending: 'text-gray-500',
    timeout: 'text-purple-700',
  };
  return (
    <span className={`text-xs font-medium capitalize ${colors[status] ?? 'text-gray-500'}`}>
      {status}
    </span>
  );
}

interface LogsModalProps {
  run: BackupRun;
  onClose: () => void;
}

function LogsModal({ run, onClose }: LogsModalProps) {
  const { data, isLoading } = useQuery({
    queryKey: ['run-logs', run.id],
    queryFn: () => jobsApi.getRunLogs(run.id),
  });

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl shadow-2xl w-full max-w-3xl max-h-[80vh] flex flex-col">
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100">
          <h3 className="font-bold text-gray-900">Run #{run.id} Logs</h3>
          <button onClick={onClose} className="text-gray-400 hover:text-gray-600 transition-colors text-xl leading-none">×</button>
        </div>
        <div className="flex-1 overflow-y-auto p-4">
          {isLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 size={24} className="animate-spin text-blue-600" />
            </div>
          ) : data ? (
            <pre className="text-xs text-gray-700 font-mono whitespace-pre-wrap bg-gray-50 rounded-lg p-4">
              {typeof data === 'string' ? data : JSON.stringify(data, null, 2)}
            </pre>
          ) : (
            <p className="text-sm text-gray-400 text-center py-12">No logs available.</p>
          )}
        </div>
      </div>
    </div>
  );
}

export default function RunHistory({ jobId }: RunHistoryProps) {
  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState('');
  const [selectedRun, setSelectedRun] = useState<BackupRun | null>(null);

  const perPage = 10;

  const params: Record<string, string> = { page: page.toString(), per_page: perPage.toString() };
  if (jobId) params.job_id = jobId.toString();
  if (statusFilter) params.status = statusFilter;

  const { data, isLoading, isError } = useQuery<RunsResponse>({
    queryKey: ['runs', params],
    queryFn: () => jobsApi.listRuns(params),
    refetchInterval: 15000,
  });

  const runs = data?.runs ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / perPage));

  return (
    <div>
      <div className="flex items-center justify-between mb-4">
        <h2 className="text-base font-semibold text-gray-800 flex items-center gap-2">
          <FileText size={16} />
          Run History
          {total > 0 && <span className="text-xs font-normal text-gray-400">({total} total)</span>}
        </h2>
        <select
          value={statusFilter}
          onChange={(e) => { setStatusFilter(e.target.value); setPage(1); }}
          className="border border-gray-300 rounded-lg px-3 py-1.5 text-xs focus:outline-none focus:ring-2 focus:ring-blue-500"
        >
          <option value="">All statuses</option>
          <option value="success">Success</option>
          <option value="failed">Failed</option>
          <option value="running">Running</option>
          <option value="pending">Pending</option>
          <option value="timeout">Timeout</option>
        </select>
      </div>

      {isLoading && (
        <div className="flex items-center justify-center py-10">
          <Loader2 size={20} className="animate-spin text-blue-600" />
        </div>
      )}

      {isError && (
        <div className="bg-red-50 border border-red-200 text-red-700 rounded-xl px-4 py-3 text-sm">
          Failed to load run history.
        </div>
      )}

      {!isLoading && !isError && runs.length === 0 && (
        <div className="text-center py-10 text-gray-400 text-sm">No runs found.</div>
      )}

      {!isLoading && !isError && runs.length > 0 && (
        <div className="border border-gray-200 rounded-xl overflow-hidden">
          <table className="w-full text-sm">
            <thead>
              <tr className="bg-gray-50 border-b border-gray-200">
                <th className="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Status</th>
                <th className="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Started</th>
                <th className="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Duration</th>
                <th className="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Size</th>
                <th className="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Files</th>
                <th className="px-4 py-3 text-left text-xs font-semibold text-gray-500 uppercase tracking-wide">Error</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100">
              {runs.map((run) => (
                <tr
                  key={run.id}
                  onClick={() => setSelectedRun(run)}
                  className="hover:bg-gray-50 cursor-pointer transition-colors"
                >
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-1.5">
                      <StatusIcon status={run.status} />
                      <StatusLabel status={run.status} />
                    </div>
                  </td>
                  <td className="px-4 py-3 text-gray-600">{formatDate(run.started_at)}</td>
                  <td className="px-4 py-3 text-gray-600">{formatDuration(run.started_at, run.finished_at)}</td>
                  <td className="px-4 py-3 text-gray-600">{formatSize(run.total_size_bytes)}</td>
                  <td className="px-4 py-3 text-gray-600">{run.files_copied ?? '—'}</td>
                  <td className="px-4 py-3 text-red-500 text-xs truncate max-w-[200px]">
                    {run.error_message || '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Pagination */}
      {totalPages > 1 && (
        <div className="flex items-center justify-between mt-4">
          <span className="text-xs text-gray-400">
            Page {page} of {totalPages}
          </span>
          <div className="flex items-center gap-2">
            <button
              onClick={() => setPage((p) => Math.max(1, p - 1))}
              disabled={page <= 1}
              className="inline-flex items-center gap-1 px-3 py-1.5 text-xs font-medium text-gray-600 border border-gray-200 rounded-lg hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              <ChevronLeft size={14} /> Prev
            </button>
            <button
              onClick={() => setPage((p) => Math.min(totalPages, p + 1))}
              disabled={page >= totalPages}
              className="inline-flex items-center gap-1 px-3 py-1.5 text-xs font-medium text-gray-600 border border-gray-200 rounded-lg hover:bg-gray-50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
            >
              Next <ChevronRight size={14} />
            </button>
          </div>
        </div>
      )}

      {selectedRun && (
        <LogsModal run={selectedRun} onClose={() => setSelectedRun(null)} />
      )}
    </div>
  );
}
