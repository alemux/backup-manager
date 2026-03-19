import { useState } from 'react';
import { useQuery, keepPreviousData } from '@tanstack/react-query';
import { Archive, ChevronLeft, ChevronRight, Lock, Unlock, AlertTriangle, CheckCircle } from 'lucide-react';
import { snapshotsApi, type Snapshot, type SnapshotListParams } from '../api/snapshots';
import SnapshotCalendar from '../components/SnapshotCalendar';
import SnapshotFilters from '../components/SnapshotFilters';
import SnapshotDetail from '../components/SnapshotDetail';

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`;
}

function formatDate(dateStr: string): string {
  try {
    return new Date(dateStr).toLocaleString();
  } catch {
    return dateStr;
  }
}

function OverallSyncBadge({ snapshot }: { snapshot: Snapshot }) {
  const statuses = snapshot.sync_statuses;
  if (!statuses || statuses.length === 0) {
    return <span className="text-xs text-gray-400">No syncs</span>;
  }
  const hasFailed = statuses.some(s => s.status === 'failed');
  const allSuccess = statuses.every(s => s.status === 'success');
  if (hasFailed) {
    return (
      <span className="inline-flex items-center gap-1 text-xs font-medium text-red-600">
        <AlertTriangle size={12} />
        Failed
      </span>
    );
  }
  if (allSuccess) {
    return (
      <span className="inline-flex items-center gap-1 text-xs font-medium text-green-600">
        <CheckCircle size={12} />
        Synced
      </span>
    );
  }
  return <span className="text-xs text-yellow-600 font-medium">Pending</span>;
}

export default function SnapshotsPage() {
  const [filters, setFilters] = useState<SnapshotListParams>({ page: 1, per_page: 20 });
  const [selectedSnapshotId, setSelectedSnapshotId] = useState<number | null>(null);
  const [selectedDate, setSelectedDate] = useState<string | null>(null);

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['snapshots', filters],
    queryFn: () => snapshotsApi.list(filters),
    placeholderData: keepPreviousData,
  });

  function handleDaySelect(date: string | null) {
    setSelectedDate(date);
    setFilters(f => ({
      ...f,
      date_from: date ?? undefined,
      date_to: date ?? undefined,
      page: 1,
    }));
  }

  function handleFiltersChange(newFilters: SnapshotListParams) {
    // If filters changed from date controls, clear calendar selection
    if (newFilters.date_from !== filters.date_from || newFilters.date_to !== filters.date_to) {
      setSelectedDate(null);
    }
    setFilters(newFilters);
  }

  function handlePageChange(newPage: number) {
    setFilters(f => ({ ...f, page: newPage }));
  }

  const total = data?.total ?? 0;
  const page = data?.page ?? 1;
  const totalPages = data?.total_pages ?? 1;
  const snapshots = data?.snapshots ?? [];

  return (
    <div className="p-6 max-w-screen-xl mx-auto">
      {/* Header */}
      <div className="mb-6">
        <div className="flex items-center gap-3">
          <div className="p-2 bg-blue-50 rounded-lg">
            <Archive size={20} className="text-blue-600" />
          </div>
          <div>
            <h1 className="text-2xl font-bold text-gray-900">Snapshots</h1>
            <p className="text-sm text-gray-500 mt-0.5">
              Browse and manage backup snapshots.
            </p>
          </div>
        </div>
      </div>

      {/* Layout: Calendar + main content */}
      <div className="flex gap-6">
        {/* Sidebar: Calendar */}
        <aside className="w-72 shrink-0">
          <SnapshotCalendar
            onDaySelect={handleDaySelect}
            selectedDate={selectedDate}
          />
        </aside>

        {/* Main content */}
        <div className="flex-1 min-w-0 space-y-4">
          {/* Filters */}
          <SnapshotFilters filters={filters} onChange={handleFiltersChange} />

          {/* Stats bar */}
          <div className="flex items-center justify-between text-sm text-gray-500">
            <span>
              {isLoading ? 'Loading...' : `${total} snapshot${total !== 1 ? 's' : ''} found`}
            </span>
            {totalPages > 1 && (
              <span>Page {page} of {totalPages}</span>
            )}
          </div>

          {/* Error */}
          {isError && (
            <div className="bg-red-50 border border-red-200 text-red-700 rounded-xl p-5 text-sm">
              Failed to load snapshots: {error instanceof Error ? error.message : 'Unknown error'}
            </div>
          )}

          {/* Table */}
          {!isError && (
            <div className="bg-white rounded-xl border border-gray-200 shadow-sm overflow-hidden">
              {isLoading ? (
                <div className="py-16 text-center text-sm text-gray-400">Loading snapshots...</div>
              ) : snapshots.length === 0 ? (
                <div className="py-16 text-center">
                  <Archive size={32} className="mx-auto text-gray-300 mb-3" />
                  <p className="text-sm text-gray-500">No snapshots found.</p>
                  <p className="text-xs text-gray-400 mt-1">Try adjusting filters or selecting a different date.</p>
                </div>
              ) : (
                <div className="overflow-x-auto">
                  <table className="w-full text-sm">
                    <thead>
                      <tr className="border-b border-gray-100 bg-gray-50">
                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Date</th>
                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Server</th>
                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Source</th>
                        <th className="text-left px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Type</th>
                        <th className="text-right px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Size</th>
                        <th className="text-center px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Enc.</th>
                        <th className="text-center px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Sync</th>
                        <th className="text-center px-4 py-3 text-xs font-semibold text-gray-500 uppercase tracking-wider">Actions</th>
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-gray-50">
                      {snapshots.map(sn => (
                        <tr
                          key={sn.id}
                          onClick={() => setSelectedSnapshotId(sn.id === selectedSnapshotId ? null : sn.id)}
                          className={[
                            'hover:bg-blue-50 cursor-pointer transition-colors',
                            sn.id === selectedSnapshotId ? 'bg-blue-50' : '',
                          ].join(' ')}
                        >
                          <td className="px-4 py-3 text-gray-600 whitespace-nowrap">
                            {formatDate(sn.created_at)}
                          </td>
                          <td className="px-4 py-3 text-gray-800 font-medium whitespace-nowrap">
                            {sn.server_name}
                          </td>
                          <td className="px-4 py-3 text-gray-600 max-w-[160px] truncate">
                            {sn.source_name}
                          </td>
                          <td className="px-4 py-3">
                            <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
                              sn.source_type === 'database'
                                ? 'bg-blue-100 text-blue-700'
                                : sn.source_type === 'web'
                                ? 'bg-purple-100 text-purple-700'
                                : 'bg-gray-100 text-gray-600'
                            }`}>
                              {sn.source_type}
                            </span>
                          </td>
                          <td className="px-4 py-3 text-right text-gray-700 whitespace-nowrap">
                            {formatBytes(sn.size_bytes)}
                          </td>
                          <td className="px-4 py-3 text-center">
                            {sn.is_encrypted ? (
                              <span title="Encrypted">
                                <Lock size={14} className="mx-auto text-amber-500" />
                              </span>
                            ) : (
                              <span title="Unencrypted">
                                <Unlock size={14} className="mx-auto text-gray-300" />
                              </span>
                            )}
                          </td>
                          <td className="px-4 py-3 text-center">
                            <OverallSyncBadge snapshot={sn} />
                          </td>
                          <td className="px-4 py-3 text-center">
                            <button
                              onClick={e => {
                                e.stopPropagation();
                                snapshotsApi.download(sn.id);
                              }}
                              className="text-xs text-blue-600 hover:text-blue-800 font-medium px-2 py-1 rounded hover:bg-blue-50 transition-colors"
                              title="Download"
                            >
                              Download
                            </button>
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              )}

              {/* Pagination */}
              {totalPages > 1 && (
                <div className="flex items-center justify-between px-4 py-3 border-t border-gray-100 bg-gray-50">
                  <button
                    onClick={() => handlePageChange(page - 1)}
                    disabled={page <= 1}
                    className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium text-gray-600 border border-gray-300 rounded-lg hover:bg-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                  >
                    <ChevronLeft size={14} />
                    Previous
                  </button>
                  <span className="text-xs text-gray-500">
                    Page {page} of {totalPages}
                  </span>
                  <button
                    onClick={() => handlePageChange(page + 1)}
                    disabled={page >= totalPages}
                    className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium text-gray-600 border border-gray-300 rounded-lg hover:bg-white disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                  >
                    Next
                    <ChevronRight size={14} />
                  </button>
                </div>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Side panel */}
      {selectedSnapshotId !== null && (
        <>
          {/* Backdrop */}
          <div
            className="fixed inset-0 bg-black/20 z-30"
            onClick={() => setSelectedSnapshotId(null)}
          />
          <SnapshotDetail
            snapshotId={selectedSnapshotId}
            onClose={() => setSelectedSnapshotId(null)}
          />
        </>
      )}
    </div>
  );
}
