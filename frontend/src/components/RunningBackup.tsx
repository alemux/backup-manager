import { useEffect, useState } from 'react';
import { RefreshCw, Square, CheckCircle, AlertCircle, XCircle } from 'lucide-react';

export interface BackupProgress {
  job_id: number;
  job_name: string;
  server_name: string;
  bytes_done?: number;
  bytes_total?: number;
  percent?: number;
  eta_seconds?: number;
  current_file?: string;
  status?: string;
  message?: string;
}

interface RunningBackupProps {
  progress: BackupProgress | null;
  onStop: (jobId: number) => void;
  stopping: boolean;
  onDismiss?: () => void;
}

function humanizeBytes(bytes: number): string {
  if (!bytes || bytes === 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(1024));
  const val = bytes / Math.pow(1024, i);
  return `${val % 1 === 0 ? val : val.toFixed(1)} ${units[i]}`;
}

function formatETA(seconds: number): string {
  if (!seconds || seconds <= 0) return 'calculating...';
  if (seconds < 60) return '< 1 min';
  if (seconds < 3600) return `~${Math.ceil(seconds / 60)} min`;
  const hours = Math.floor(seconds / 3600);
  const mins = Math.ceil((seconds % 3600) / 60);
  return `~${hours}h ${mins}m`;
}

type DisplayState = 'analyzing' | 'running' | 'complete' | 'skipped' | 'stopped' | 'error';

function getDisplayState(progress: BackupProgress): DisplayState {
  const status = progress.status;
  if (status === 'complete') return 'complete';
  if (status === 'skipped') return 'skipped';
  if (status === 'stopped') return 'stopped';
  if (status === 'error') return 'error';
  const pct = progress.percent ?? 0;
  if (pct === 0 && !progress.bytes_total) return 'analyzing';
  return 'running';
}

export default function RunningBackup({ progress, onStop, stopping, onDismiss }: RunningBackupProps) {
  const [confirmStop, setConfirmStop] = useState(false);

  // Auto-dismiss on terminal states
  useEffect(() => {
    if (!progress) return;
    const state = getDisplayState(progress);
    if (state === 'complete' || state === 'skipped') {
      const timer = setTimeout(() => {
        onDismiss?.();
      }, state === 'skipped' ? 3000 : 5000);
      return () => clearTimeout(timer);
    }
  }, [progress, onDismiss]);

  if (!progress) return null;

  const bytesDone = progress.bytes_done ?? 0;
  const bytesTotal = progress.bytes_total ?? 0;
  const pct = progress.percent ?? 0;
  const clampedPercent = Math.min(100, Math.max(0, pct));
  const displayState = getDisplayState(progress);

  const { job_id, job_name, server_name, eta_seconds, current_file } = progress;

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

  // Determine border gradient based on state
  const borderGradient =
    displayState === 'complete'
      ? 'linear-gradient(135deg, #22c55e 0%, #16a34a 100%)'
      : displayState === 'skipped'
      ? 'linear-gradient(135deg, #22c55e 0%, #16a34a 100%)'
      : displayState === 'error'
      ? 'linear-gradient(135deg, #ef4444 0%, #dc2626 100%)'
      : displayState === 'stopped'
      ? 'linear-gradient(135deg, #f59e0b 0%, #d97706 100%)'
      : 'linear-gradient(135deg, #3b82f6 0%, #6366f1 50%, #3b82f6 100%)';

  return (
    <>
      <div
        className="rounded-xl p-px mb-6"
        style={{ background: borderGradient }}
      >
        <div className="bg-white rounded-[11px] p-5">
          {/* Header */}
          <div className="flex items-center justify-between mb-3">
            <div className="flex items-center gap-2">
              {displayState === 'analyzing' && (
                <RefreshCw size={15} className="text-blue-500 animate-spin" style={{ animationDuration: '2s' }} />
              )}
              {displayState === 'running' && (
                <RefreshCw size={15} className="text-blue-500 animate-spin" style={{ animationDuration: '2s' }} />
              )}
              {(displayState === 'complete' || displayState === 'skipped') && (
                <CheckCircle size={15} className="text-green-500" />
              )}
              {displayState === 'error' && (
                <XCircle size={15} className="text-red-500" />
              )}
              {displayState === 'stopped' && (
                <AlertCircle size={15} className="text-amber-500" />
              )}
              <span className="text-sm font-semibold text-gray-800">
                {displayState === 'analyzing' && 'Analyzing Sources...'}
                {displayState === 'running' && 'Running Backup'}
                {displayState === 'complete' && 'Backup Complete'}
                {displayState === 'skipped' && 'Nothing to Backup'}
                {displayState === 'stopped' && 'Backup Stopped'}
                {displayState === 'error' && 'Backup Failed'}
              </span>
            </div>
            {(displayState === 'analyzing' || displayState === 'running') && (
              <button
                onClick={handleStopClick}
                disabled={stopping}
                className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-semibold text-red-600 border border-red-200 rounded-lg hover:bg-red-50 transition-colors disabled:opacity-50 disabled:cursor-not-allowed"
              >
                <Square size={11} />
                {stopping ? 'Stopping...' : 'Stop'}
              </button>
            )}
          </div>

          {/* Job name & server */}
          <p className="text-sm font-medium text-gray-800 mb-3">
            {job_name}
            <span className="font-normal text-gray-400 mx-1.5">—</span>
            <span className="text-gray-500">{server_name}</span>
          </p>

          {/* State-specific content */}
          {displayState === 'analyzing' && (
            <div className="flex items-center gap-2 text-sm text-gray-500">
              <span className="inline-block w-2 h-2 rounded-full bg-blue-400 animate-pulse" />
              Scanning sources and comparing files...
            </div>
          )}

          {displayState === 'running' && (
            <>
              {/* Progress bar */}
              <div className="mb-2">
                <div className="flex items-center justify-between mb-1">
                  <span className="text-xs text-gray-500">
                    {humanizeBytes(bytesDone)} / {humanizeBytes(bytesTotal)}
                  </span>
                  <span className="text-xs font-bold text-gray-800">{clampedPercent}%</span>
                </div>
                <div className="h-2.5 w-full bg-gray-100 rounded-full overflow-hidden">
                  <div
                    className="h-full rounded-full transition-all duration-500"
                    style={{
                      width: `${clampedPercent}%`,
                      background: 'linear-gradient(90deg, #22c55e 0%, #16a34a 100%)',
                    }}
                  />
                </div>
              </div>

              {/* ETA & current file */}
              <div className="flex items-center justify-between mt-2">
                <span className="text-xs text-gray-500 font-mono truncate flex-1 mr-3" title={current_file}>
                  {current_file || 'Waiting for file...'}
                </span>
                <span className="text-xs text-gray-400 shrink-0">
                  {formatETA(eta_seconds ?? 0)} remaining
                </span>
              </div>
            </>
          )}

          {displayState === 'complete' && (
            <div className="text-sm text-green-600 font-medium">
              {progress.message || 'All files backed up successfully. Dismissing shortly...'}
            </div>
          )}

          {displayState === 'skipped' && (
            <div className="text-sm text-green-600 font-medium">
              All files are already up to date. Nothing needed to be transferred.
            </div>
          )}

          {displayState === 'stopped' && (
            <div className="text-sm text-amber-600 font-medium">
              {progress.message || 'Backup was stopped by user. Files already transferred are kept.'}
            </div>
          )}

          {displayState === 'error' && (
            <div className="text-sm text-red-600 font-medium">
              {progress.message || 'An error occurred during the backup.'}
            </div>
          )}
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
