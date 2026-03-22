import { useState } from 'react';
import { RefreshCw, Square, Download } from 'lucide-react';

export interface BackupProgress {
  job_id: number;
  job_name: string;
  server_name: string;
  bytes_done: number;
  bytes_total: number;
  percent: number;
  eta_seconds: number;
  current_file: string;
}

interface RunningBackupProps {
  progress: BackupProgress | null;
  onStop: (jobId: number) => void;
  stopping: boolean;
}

function humanizeBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val % 1 === 0 ? val : val.toFixed(1)} ${units[i]}`;
}

function formatETA(seconds: number): string {
  if (seconds <= 0) return 'calculating...';
  if (seconds < 60) return '< 1 min';
  if (seconds < 3600) return `~${Math.ceil(seconds / 60)} min`;
  const hours = Math.floor(seconds / 3600);
  const mins = Math.ceil((seconds % 3600) / 60);
  return `~${hours}h ${mins}m`;
}

export default function RunningBackup({ progress, onStop, stopping }: RunningBackupProps) {
  const [confirmStop, setConfirmStop] = useState(false);

  if (!progress) return null;

  const { job_id, job_name, server_name, bytes_done, bytes_total, percent, eta_seconds, current_file } = progress;
  const clampedPercent = Math.min(100, Math.max(0, percent));

  const handleStopClick = () => {
    if (stopping) return;
    setConfirmStop(true);
  };

  const handleConfirmStop = () => {
    setConfirmStop(false);
    onStop(job_id);
  };

  const handleCancelStop = () => {
    setConfirmStop(false);
  };

  return (
    <>
      <div
        className="rounded-xl p-px mb-6"
        style={{
          background: 'linear-gradient(135deg, #3b82f6 0%, #6366f1 50%, #3b82f6 100%)',
        }}
      >
        <div className="bg-white rounded-[11px] p-5">
          {/* Header */}
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              <RefreshCw size={15} className="text-blue-500 animate-spin" style={{ animationDuration: '2s' }} />
              <span className="text-sm font-semibold text-gray-800">Running Backup</span>
            </div>
            <button
              onClick={handleStopClick}
              disabled={stopping}
              className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold text-red-600 border border-red-200 rounded-lg hover:bg-red-50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
            >
              <Square size={11} />
              {stopping ? 'Stopping...' : 'Stop'}
            </button>
          </div>

          {/* Job name & server */}
          <p className="text-sm font-medium text-gray-800 mb-3">
            {job_name}
            <span className="font-normal text-gray-400 mx-1.5">—</span>
            <span className="text-gray-500">{server_name}</span>
          </p>

          {/* Progress bar */}
          <div className="mb-2">
            <div className="flex items-center justify-between mb-1">
              <span className="text-xs text-gray-500">
                {humanizeBytes(bytes_done)} / {humanizeBytes(bytes_total)}
              </span>
              <span className="text-xs font-bold text-gray-800">{clampedPercent}%</span>
            </div>
            <div className="h-2.5 w-full bg-gray-100 rounded-full overflow-hidden">
              <div
                className="h-full rounded-full transition-all duration-500"
                style={{
                  width: `${clampedPercent}%`,
                  background: 'linear-gradient(90deg, #22c55e 0%, #16a34a 100%)',
                  animation: 'pulse 2s cubic-bezier(0.4, 0, 0.6, 1) infinite',
                }}
              />
            </div>
          </div>

          {/* ETA & current file */}
          <div className="flex items-center justify-between mt-2">
            <div className="flex items-center gap-1.5 min-w-0 flex-1 mr-3">
              <Download size={12} className="text-gray-400 shrink-0" />
              <span
                className="text-xs text-gray-500 font-mono truncate"
                title={current_file}
              >
                {current_file || 'Waiting for file...'}
              </span>
            </div>
            <span className="text-xs text-gray-400 shrink-0">
              {formatETA(eta_seconds)} remaining
            </span>
          </div>
        </div>
      </div>

      {/* Stop confirmation dialog */}
      {confirmStop && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
          onClick={handleCancelStop}
        >
          <div
            className="bg-white rounded-2xl shadow-2xl w-full max-w-sm mx-4 p-6"
            onClick={(e) => e.stopPropagation()}
          >
            <h3 className="text-base font-semibold text-gray-900 mb-2">Stop this backup?</h3>
            <p className="text-sm text-gray-500 mb-5">
              Files already transferred will be kept. The backup will be marked as stopped.
            </p>
            <div className="flex gap-3 justify-end">
              <button
                onClick={handleCancelStop}
                className="px-4 py-2 text-sm font-medium text-gray-600 border border-gray-200 rounded-lg hover:bg-gray-50 transition-colors"
              >
                Cancel
              </button>
              <button
                onClick={handleConfirmStop}
                className="px-4 py-2 text-sm font-semibold text-white bg-red-600 rounded-lg hover:bg-red-700 transition-colors"
              >
                Stop Backup
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  );
}
