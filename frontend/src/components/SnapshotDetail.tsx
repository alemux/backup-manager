import { X, Download, Server, Database, FileText, Lock, Unlock, Shield, Clock } from 'lucide-react';
import { useQuery } from '@tanstack/react-query';
import { snapshotsApi, type SnapshotSyncStatus } from '../api/snapshots';

interface SnapshotDetailProps {
  snapshotId: number;
  onClose: () => void;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

function formatDate(dateStr: string | undefined): string {
  if (!dateStr) return '—';
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

function SyncBadge({ status }: { status: SnapshotSyncStatus['status'] }) {
  const styles: Record<string, string> = {
    success: 'bg-green-100 text-green-700 border-green-200',
    pending: 'bg-yellow-100 text-yellow-700 border-yellow-200',
    in_progress: 'bg-blue-100 text-blue-700 border-blue-200',
    failed: 'bg-red-100 text-red-700 border-red-200',
  };
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium border ${styles[status] ?? styles.pending}`}>
      {status.replace('_', ' ')}
    </span>
  );
}

function SourceIcon({ type }: { type: string }) {
  if (type === 'database') return <Database size={14} className="text-blue-500" />;
  if (type === 'web') return <Server size={14} className="text-purple-500" />;
  return <FileText size={14} className="text-gray-500" />;
}

export default function SnapshotDetail({ snapshotId, onClose }: SnapshotDetailProps) {
  const { data: sn, isLoading, isError } = useQuery({
    queryKey: ['snapshot', snapshotId],
    queryFn: () => snapshotsApi.get(snapshotId),
  });

  return (
    <div className="fixed inset-y-0 right-0 w-[420px] bg-white border-l border-gray-200 shadow-xl z-40 flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center justify-between px-5 py-4 border-b border-gray-100">
        <h2 className="text-base font-semibold text-gray-900">Snapshot Detail</h2>
        <button
          onClick={onClose}
          className="p-1.5 rounded-lg hover:bg-gray-100 text-gray-500 transition-colors"
          aria-label="Close"
        >
          <X size={18} />
        </button>
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-5 space-y-5">
        {isLoading && (
          <div className="text-sm text-gray-400 text-center py-10">Loading snapshot...</div>
        )}

        {isError && (
          <div className="bg-red-50 border border-red-200 text-red-700 text-sm rounded-lg p-4">
            Failed to load snapshot details.
          </div>
        )}

        {sn && (
          <>
            {/* Source info */}
            <section>
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-3">
                Source
              </h3>
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-sm">
                  <Server size={14} className="text-gray-400 shrink-0" />
                  <span className="text-gray-500 shrink-0">Server</span>
                  <span className="font-medium text-gray-800 ml-auto">{sn.server_name}</span>
                </div>
                <div className="flex items-center gap-2 text-sm">
                  <SourceIcon type={sn.source_type} />
                  <span className="text-gray-500 shrink-0">Type</span>
                  <span className="font-medium text-gray-800 ml-auto capitalize">{sn.source_type}</span>
                </div>
                <div className="flex items-start gap-2 text-sm">
                  <FileText size={14} className="text-gray-400 shrink-0 mt-0.5" />
                  <span className="text-gray-500 shrink-0">Source</span>
                  <span className="font-medium text-gray-800 ml-auto text-right break-all">
                    {sn.source_type === 'database' ? (sn.db_name ?? '—') : (sn.source_path ?? '—')}
                  </span>
                </div>
                <div className="flex items-center gap-2 text-sm">
                  <Clock size={14} className="text-gray-400 shrink-0" />
                  <span className="text-gray-500 shrink-0">Created</span>
                  <span className="font-medium text-gray-800 ml-auto">{formatDate(sn.created_at)}</span>
                </div>
              </div>
            </section>

            <hr className="border-gray-100" />

            {/* Snapshot info */}
            <section>
              <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-3">
                Snapshot Info
              </h3>
              <div className="space-y-2">
                <div className="flex items-center gap-2 text-sm">
                  <span className="text-gray-500">Size</span>
                  <span className="font-semibold text-gray-800 ml-auto">{formatBytes(sn.size_bytes)}</span>
                </div>

                {sn.checksum_sha256 && (
                  <div className="flex items-start gap-2 text-sm">
                    <span className="text-gray-500 shrink-0">SHA256</span>
                    <span className="font-mono text-xs text-gray-600 ml-auto text-right break-all bg-gray-50 rounded px-1.5 py-0.5">
                      {sn.checksum_sha256}
                    </span>
                  </div>
                )}

                <div className="flex items-center gap-2 text-sm">
                  <span className="text-gray-500">Encryption</span>
                  <span className="ml-auto">
                    {sn.is_encrypted ? (
                      <span className="inline-flex items-center gap-1 bg-amber-100 text-amber-700 border border-amber-200 px-2 py-0.5 rounded-full text-xs font-medium">
                        <Lock size={11} />
                        Encrypted
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1 bg-gray-100 text-gray-600 border border-gray-200 px-2 py-0.5 rounded-full text-xs font-medium">
                        <Unlock size={11} />
                        Unencrypted
                      </span>
                    )}
                  </span>
                </div>

                {sn.integrity_status && (
                  <div className="flex items-center gap-2 text-sm">
                    <span className="text-gray-500">Integrity</span>
                    <span className="ml-auto">
                      <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium border ${
                        sn.integrity_status === 'ok'
                          ? 'bg-green-100 text-green-700 border-green-200'
                          : sn.integrity_status === 'missing'
                          ? 'bg-yellow-100 text-yellow-700 border-yellow-200'
                          : 'bg-red-100 text-red-700 border-red-200'
                      }`}>
                        <Shield size={11} />
                        {sn.integrity_status}
                      </span>
                    </span>
                  </div>
                )}

                {sn.retention_expires_at && (
                  <div className="flex items-center gap-2 text-sm">
                    <span className="text-gray-500">Expires</span>
                    <span className="font-medium text-gray-800 ml-auto">
                      {formatDate(sn.retention_expires_at)}
                    </span>
                  </div>
                )}
              </div>
            </section>

            {/* Sync statuses */}
            {sn.sync_statuses && sn.sync_statuses.length > 0 && (
              <>
                <hr className="border-gray-100" />
                <section>
                  <h3 className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-3">
                    Destination Sync
                  </h3>
                  <div className="space-y-2">
                    {sn.sync_statuses.map(ss => (
                      <div key={ss.destination_id} className="flex items-center gap-2 text-sm">
                        <span className="text-gray-700 truncate flex-1">{ss.destination_name}</span>
                        <SyncBadge status={ss.status} />
                      </div>
                    ))}
                  </div>
                </section>
              </>
            )}
          </>
        )}
      </div>

      {/* Footer: Download button */}
      {sn && (
        <div className="px-5 py-4 border-t border-gray-100 bg-gray-50">
          <button
            onClick={() => snapshotsApi.download(sn.id)}
            className="w-full inline-flex items-center justify-center gap-2 px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors shadow-sm"
          >
            <Download size={16} />
            Download Snapshot
          </button>
        </div>
      )}
    </div>
  );
}
