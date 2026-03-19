import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Filter, X } from 'lucide-react';
import { serversApi } from '../api/servers';
import type { SnapshotListParams } from '../api/snapshots';

interface SnapshotFiltersProps {
  filters: SnapshotListParams;
  onChange: (filters: SnapshotListParams) => void;
}

const SOURCE_TYPES = [
  { value: 'all', label: 'All types' },
  { value: 'web', label: 'Web' },
  { value: 'database', label: 'Database' },
  { value: 'config', label: 'Config' },
];

export default function SnapshotFilters({ filters, onChange }: SnapshotFiltersProps) {
  const [local, setLocal] = useState<SnapshotListParams>(filters);

  const { data: servers } = useQuery({
    queryKey: ['servers'],
    queryFn: () => serversApi.list(),
  });

  function handleApply() {
    onChange({ ...local, page: 1 });
  }

  function handleClear() {
    const cleared: SnapshotListParams = { page: 1 };
    setLocal(cleared);
    onChange(cleared);
  }

  const hasFilters = !!(
    filters.server_id || filters.source_type || filters.date_from || filters.date_to
  );

  return (
    <div className="bg-white rounded-xl border border-gray-200 p-4 shadow-sm">
      <div className="flex items-center gap-2 mb-3">
        <Filter size={15} className="text-gray-400" />
        <span className="text-sm font-medium text-gray-700">Filters</span>
        {hasFilters && (
          <span className="ml-auto text-xs bg-blue-100 text-blue-700 font-medium px-2 py-0.5 rounded-full">
            Active
          </span>
        )}
      </div>

      <div className="flex flex-wrap gap-3 items-end">
        {/* Server dropdown */}
        <div className="flex flex-col gap-1 min-w-[160px]">
          <label className="text-xs font-medium text-gray-500">Server</label>
          <select
            value={local.server_id ?? ''}
            onChange={e => setLocal(l => ({ ...l, server_id: e.target.value || undefined }))}
            className="border border-gray-300 rounded-lg px-3 py-2 text-sm text-gray-700 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          >
            <option value="">All servers</option>
            {servers?.map(s => (
              <option key={s.id} value={String(s.id)}>{s.name}</option>
            ))}
          </select>
        </div>

        {/* Source type dropdown */}
        <div className="flex flex-col gap-1 min-w-[140px]">
          <label className="text-xs font-medium text-gray-500">Type</label>
          <select
            value={local.source_type ?? 'all'}
            onChange={e => setLocal(l => ({ ...l, source_type: e.target.value }))}
            className="border border-gray-300 rounded-lg px-3 py-2 text-sm text-gray-700 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          >
            {SOURCE_TYPES.map(t => (
              <option key={t.value} value={t.value}>{t.label}</option>
            ))}
          </select>
        </div>

        {/* Date from */}
        <div className="flex flex-col gap-1">
          <label className="text-xs font-medium text-gray-500">From</label>
          <input
            type="date"
            value={local.date_from ?? ''}
            onChange={e => setLocal(l => ({ ...l, date_from: e.target.value || undefined }))}
            className="border border-gray-300 rounded-lg px-3 py-2 text-sm text-gray-700 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          />
        </div>

        {/* Date to */}
        <div className="flex flex-col gap-1">
          <label className="text-xs font-medium text-gray-500">To</label>
          <input
            type="date"
            value={local.date_to ?? ''}
            onChange={e => setLocal(l => ({ ...l, date_to: e.target.value || undefined }))}
            className="border border-gray-300 rounded-lg px-3 py-2 text-sm text-gray-700 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent"
          />
        </div>

        {/* Buttons */}
        <div className="flex gap-2 ml-auto">
          {hasFilters && (
            <button
              onClick={handleClear}
              className="inline-flex items-center gap-1.5 px-3 py-2 border border-gray-300 text-sm text-gray-600 rounded-lg hover:bg-gray-50 transition-colors"
            >
              <X size={14} />
              Clear
            </button>
          )}
          <button
            onClick={handleApply}
            className="px-4 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors shadow-sm"
          >
            Apply
          </button>
        </div>
      </div>
    </div>
  );
}
