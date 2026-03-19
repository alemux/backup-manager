import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Download,
  Filter,
  ChevronLeft,
  ChevronRight,
  ClipboardList,
  User,
} from 'lucide-react';
import { auditApi, type AuditEntry } from '../api/audit';

// ─── Helpers ─────────────────────────────────────────────────────────────────

function formatDate(iso: string): string {
  if (!iso) return '—';
  try {
    return new Date(iso).toLocaleString(undefined, {
      year: 'numeric',
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    });
  } catch {
    return iso;
  }
}

// Common action → badge color mapping
const ACTION_COLORS: Record<string, string> = {
  login: 'bg-blue-100 text-blue-700',
  logout: 'bg-gray-100 text-gray-600',
  create: 'bg-green-100 text-green-700',
  update: 'bg-amber-100 text-amber-700',
  delete: 'bg-red-100 text-red-700',
  trigger: 'bg-purple-100 text-purple-700',
  export: 'bg-indigo-100 text-indigo-700',
  test: 'bg-cyan-100 text-cyan-700',
};

function actionColor(action: string): string {
  const lower = action.toLowerCase();
  for (const [key, cls] of Object.entries(ACTION_COLORS)) {
    if (lower.includes(key)) return cls;
  }
  return 'bg-gray-100 text-gray-600';
}

// ─── Filter bar ───────────────────────────────────────────────────────────────

interface Filters {
  from: string;
  to: string;
  action: string;
  user_id: string;
}

function FilterBar({
  filters,
  onChange,
  onReset,
}: {
  filters: Filters;
  onChange: (f: Filters) => void;
  onReset: () => void;
}) {
  return (
    <div className="bg-white rounded-xl border border-gray-100 shadow-sm p-4 flex flex-wrap items-end gap-3">
      <div>
        <label className="block text-xs font-medium text-gray-500 mb-1">From</label>
        <input
          type="date"
          value={filters.from}
          onChange={(e) => onChange({ ...filters, from: e.target.value })}
          className="border border-gray-300 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>
      <div>
        <label className="block text-xs font-medium text-gray-500 mb-1">To</label>
        <input
          type="date"
          value={filters.to}
          onChange={(e) => onChange({ ...filters, to: e.target.value })}
          className="border border-gray-300 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
        />
      </div>
      <div>
        <label className="block text-xs font-medium text-gray-500 mb-1">Action</label>
        <input
          type="text"
          value={filters.action}
          placeholder="e.g. login"
          onChange={(e) => onChange({ ...filters, action: e.target.value })}
          className="border border-gray-300 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-36"
        />
      </div>
      <div>
        <label className="block text-xs font-medium text-gray-500 mb-1">User ID</label>
        <input
          type="text"
          value={filters.user_id}
          placeholder="e.g. 1"
          onChange={(e) => onChange({ ...filters, user_id: e.target.value })}
          className="border border-gray-300 rounded-lg px-3 py-1.5 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500 w-24"
        />
      </div>
      <button
        onClick={onReset}
        className="px-3 py-1.5 text-sm text-gray-500 border border-gray-200 rounded-lg hover:bg-gray-50"
      >
        Reset
      </button>
    </div>
  );
}

// ─── Pagination ───────────────────────────────────────────────────────────────

function Pagination({
  page,
  perPage,
  total,
  onPage,
}: {
  page: number;
  perPage: number;
  total: number;
  onPage: (p: number) => void;
}) {
  const totalPages = Math.max(1, Math.ceil(total / perPage));
  const start = (page - 1) * perPage + 1;
  const end = Math.min(page * perPage, total);

  return (
    <div className="flex items-center justify-between px-1">
      <p className="text-sm text-gray-500">
        {total === 0 ? 'No entries' : `${start}–${end} of ${total}`}
      </p>
      <div className="flex items-center gap-1">
        <button
          onClick={() => onPage(page - 1)}
          disabled={page <= 1}
          className="p-1.5 rounded-lg text-gray-400 hover:text-gray-700 disabled:opacity-30"
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
        {Array.from({ length: Math.min(totalPages, 7) }, (_, i) => {
          let p = i + 1;
          if (totalPages > 7) {
            if (page <= 4) p = i + 1;
            else if (page >= totalPages - 3) p = totalPages - 6 + i;
            else p = page - 3 + i;
          }
          return (
            <button
              key={p}
              onClick={() => onPage(p)}
              className={`w-8 h-8 rounded-lg text-sm font-medium ${
                p === page ? 'bg-blue-600 text-white' : 'text-gray-600 hover:bg-gray-100'
              }`}
            >
              {p}
            </button>
          );
        })}
        <button
          onClick={() => onPage(page + 1)}
          disabled={page >= totalPages}
          className="p-1.5 rounded-lg text-gray-400 hover:text-gray-700 disabled:opacity-30"
        >
          <ChevronRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  );
}

// ─── Page ────────────────────────────────────────────────────────────────────

const DEFAULT_FILTERS: Filters = { from: '', to: '', action: '', user_id: '' };

export default function AuditLogPage() {
  const [page, setPage] = useState(1);
  const [perPage] = useState(20);
  const [filters, setFilters] = useState<Filters>(DEFAULT_FILTERS);
  const [showFilters, setShowFilters] = useState(false);

  // Build query params
  function buildParams(): Record<string, string> {
    const params: Record<string, string> = {
      page: String(page),
      per_page: String(perPage),
    };
    if (filters.from) params.from = new Date(filters.from).toISOString();
    if (filters.to) {
      const to = new Date(filters.to);
      to.setHours(23, 59, 59, 999);
      params.to = to.toISOString();
    }
    if (filters.action) params.action = filters.action;
    if (filters.user_id) params.user_id = filters.user_id;
    return params;
  }

  const { data, isLoading, isError, error } = useQuery({
    queryKey: ['audit-log', page, perPage, filters],
    queryFn: () => auditApi.list(buildParams()),
    placeholderData: (prev) => prev,
  });

  function handleFilterChange(f: Filters) {
    setFilters(f);
    setPage(1);
  }

  const entries: AuditEntry[] = data?.data ?? [];
  const total = data?.total ?? 0;

  return (
    <div className="p-6 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Audit Log</h1>
          <p className="text-sm text-gray-500 mt-0.5">Track user actions and system events.</p>
        </div>
        <div className="flex items-center gap-3">
          <button
            onClick={() => setShowFilters((v) => !v)}
            className={`inline-flex items-center gap-2 px-3.5 py-2 text-sm font-medium border rounded-lg transition-colors ${
              showFilters
                ? 'bg-blue-50 text-blue-700 border-blue-200'
                : 'bg-white text-gray-600 border-gray-200 hover:bg-gray-50'
            }`}
          >
            <Filter className="w-4 h-4" />
            Filters
            {Object.values(filters).some(Boolean) && (
              <span className="ml-1 w-2 h-2 rounded-full bg-blue-500 inline-block" />
            )}
          </button>
          <button
            onClick={() => auditApi.exportCSV()}
            className="inline-flex items-center gap-2 px-3.5 py-2 text-sm font-medium bg-white border border-gray-200 text-gray-600 rounded-lg hover:bg-gray-50"
          >
            <Download className="w-4 h-4" />
            Export CSV
          </button>
        </div>
      </div>

      {/* Filters */}
      {showFilters && (
        <div className="mb-4">
          <FilterBar
            filters={filters}
            onChange={handleFilterChange}
            onReset={() => { setFilters(DEFAULT_FILTERS); setPage(1); }}
          />
        </div>
      )}

      {/* Table */}
      {isLoading ? (
        <div className="flex justify-center py-20">
          <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
        </div>
      ) : isError ? (
        <div className="bg-red-50 border border-red-200 rounded-xl p-4 text-red-700 text-sm">
          Failed to load audit log: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      ) : entries.length === 0 ? (
        <div className="text-center py-20">
          <ClipboardList className="w-10 h-10 mx-auto mb-3 text-gray-200" />
          <p className="text-sm text-gray-400">No audit log entries found.</p>
          {Object.values(filters).some(Boolean) && (
            <button
              onClick={() => { setFilters(DEFAULT_FILTERS); setPage(1); }}
              className="mt-3 text-sm text-blue-600 hover:underline"
            >
              Clear filters
            </button>
          )}
        </div>
      ) : (
        <div className="space-y-3">
          <div className="bg-white rounded-xl border border-gray-100 shadow-sm overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-100 bg-gray-50">
                  <th className="text-left py-3 px-4 font-semibold text-gray-600 whitespace-nowrap">Timestamp</th>
                  <th className="text-left py-3 px-4 font-semibold text-gray-600">User</th>
                  <th className="text-left py-3 px-4 font-semibold text-gray-600">Action</th>
                  <th className="text-left py-3 px-4 font-semibold text-gray-600">Target</th>
                  <th className="text-left py-3 px-4 font-semibold text-gray-600">IP Address</th>
                  <th className="text-left py-3 px-4 font-semibold text-gray-600">Details</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr key={entry.id} className="border-b border-gray-50 hover:bg-gray-50">
                    <td className="py-3 px-4 text-gray-500 text-xs whitespace-nowrap">
                      {formatDate(entry.created_at)}
                    </td>
                    <td className="py-3 px-4">
                      {entry.username ? (
                        <div className="flex items-center gap-1.5">
                          <User className="w-3.5 h-3.5 text-gray-300 shrink-0" />
                          <span className="text-gray-700 font-medium">{entry.username}</span>
                        </div>
                      ) : entry.user_id ? (
                        <span className="text-gray-400 text-xs">#{entry.user_id}</span>
                      ) : (
                        <span className="text-gray-300 text-xs">—</span>
                      )}
                    </td>
                    <td className="py-3 px-4">
                      <span className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${actionColor(entry.action)}`}>
                        {entry.action}
                      </span>
                    </td>
                    <td className="py-3 px-4 text-gray-600 text-xs max-w-[160px] truncate">
                      {entry.target || <span className="text-gray-300">—</span>}
                    </td>
                    <td className="py-3 px-4 text-gray-500 text-xs whitespace-nowrap">
                      {entry.ip_address || <span className="text-gray-300">—</span>}
                    </td>
                    <td className="py-3 px-4 text-gray-500 text-xs max-w-[200px] truncate">
                      {entry.details ? (
                        <span title={entry.details}>{entry.details}</span>
                      ) : (
                        <span className="text-gray-300">—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <Pagination
            page={page}
            perPage={perPage}
            total={total}
            onPage={setPage}
          />
        </div>
      )}
    </div>
  );
}
