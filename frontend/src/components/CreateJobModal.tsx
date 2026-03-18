import { useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { X, Loader2 } from 'lucide-react';
import { serversApi } from '../api/servers';
import { jobsApi } from '../api/jobs';
import ScheduleSelector from './ScheduleSelector';
import type { BackupSource, Server } from '../types';

interface CreateJobModalProps {
  onClose: () => void;
}

export default function CreateJobModal({ onClose }: CreateJobModalProps) {
  const queryClient = useQueryClient();

  const [name, setName] = useState('');
  const [serverId, setServerId] = useState<number | ''>('');
  const [selectedSources, setSelectedSources] = useState<number[]>([]);
  const [schedule, setSchedule] = useState('0 3 * * *');
  const [retentionDaily, setRetentionDaily] = useState(7);
  const [retentionWeekly, setRetentionWeekly] = useState(4);
  const [retentionMonthly, setRetentionMonthly] = useState(3);
  const [bandwidthLimit, setBandwidthLimit] = useState<string>('');
  const [timeout, setTimeout_] = useState(120);
  const [error, setError] = useState('');

  const { data: servers, isLoading: serversLoading } = useQuery<Server[]>({
    queryKey: ['servers'],
    queryFn: () => serversApi.list(),
  });

  const { data: sources, isLoading: sourcesLoading } = useQuery<BackupSource[]>({
    queryKey: ['sources', serverId],
    queryFn: () => serversApi.listSources(serverId as number),
    enabled: typeof serverId === 'number',
  });

  const createMutation = useMutation({
    mutationFn: (data: unknown) => jobsApi.create(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] });
      onClose();
    },
    onError: (err: Error) => {
      setError(err.message);
    },
  });

  function handleServerChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const val = e.target.value;
    setServerId(val === '' ? '' : parseInt(val, 10));
    setSelectedSources([]);
  }

  function toggleSource(id: number) {
    setSelectedSources((prev) =>
      prev.includes(id) ? prev.filter((s) => s !== id) : [...prev, id]
    );
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError('');

    if (!name.trim()) { setError('Job name is required.'); return; }
    if (serverId === '') { setError('Please select a server.'); return; }
    if (selectedSources.length === 0) { setError('Select at least one backup source.'); return; }

    createMutation.mutate({
      name: name.trim(),
      server_id: serverId,
      source_ids: selectedSources,
      schedule,
      retention_daily: retentionDaily,
      retention_weekly: retentionWeekly,
      retention_monthly: retentionMonthly,
      bandwidth_limit_mbps: bandwidthLimit !== '' ? parseFloat(bandwidthLimit) : null,
      timeout_minutes: timeout,
      enabled: true,
    });
  }

  return (
    <div className="fixed inset-0 bg-black/40 flex items-center justify-center z-50 p-4">
      <div className="bg-white rounded-2xl shadow-2xl w-full max-w-2xl max-h-[90vh] flex flex-col">
        {/* Header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-gray-100">
          <h2 className="text-lg font-bold text-gray-900">Create Backup Job</h2>
          <button
            onClick={onClose}
            className="text-gray-400 hover:text-gray-600 transition-colors"
          >
            <X size={20} />
          </button>
        </div>

        {/* Body */}
        <form onSubmit={handleSubmit} className="flex-1 overflow-y-auto px-6 py-5 space-y-5">
          {/* Job name */}
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Job Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Nightly Production Backup"
              className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
          </div>

          {/* Server */}
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-1">Server</label>
            {serversLoading ? (
              <div className="flex items-center gap-2 text-sm text-gray-400">
                <Loader2 size={14} className="animate-spin" /> Loading servers…
              </div>
            ) : (
              <select
                value={serverId}
                onChange={handleServerChange}
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              >
                <option value="">Select a server</option>
                {servers?.map((s) => (
                  <option key={s.id} value={s.id}>{s.name} ({s.host})</option>
                ))}
              </select>
            )}
          </div>

          {/* Sources */}
          {serverId !== '' && (
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Backup Sources</label>
              {sourcesLoading ? (
                <div className="flex items-center gap-2 text-sm text-gray-400">
                  <Loader2 size={14} className="animate-spin" /> Loading sources…
                </div>
              ) : sources && sources.length > 0 ? (
                <div className="border border-gray-200 rounded-lg divide-y divide-gray-100 max-h-40 overflow-y-auto">
                  {sources.map((src) => (
                    <label
                      key={src.id}
                      className="flex items-center gap-3 px-3 py-2.5 hover:bg-gray-50 cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={selectedSources.includes(src.id)}
                        onChange={() => toggleSource(src.id)}
                        className="accent-blue-600"
                      />
                      <div className="min-w-0">
                        <p className="text-sm font-medium text-gray-800 truncate">{src.name}</p>
                        <p className="text-xs text-gray-400">{src.type} {src.source_path ? `— ${src.source_path}` : ''}</p>
                      </div>
                    </label>
                  ))}
                </div>
              ) : (
                <p className="text-sm text-gray-400">No backup sources found for this server.</p>
              )}
            </div>
          )}

          {/* Schedule */}
          <div className="border border-gray-200 rounded-lg p-4">
            <ScheduleSelector value={schedule} onChange={setSchedule} />
          </div>

          {/* Retention */}
          <div>
            <label className="block text-sm font-medium text-gray-700 mb-2">Retention Policy</label>
            <div className="grid grid-cols-3 gap-3">
              <div>
                <label className="block text-xs text-gray-500 mb-1">Daily copies</label>
                <input
                  type="number"
                  min={1}
                  value={retentionDaily}
                  onChange={(e) => setRetentionDaily(parseInt(e.target.value, 10) || 1)}
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">Weekly copies</label>
                <input
                  type="number"
                  min={0}
                  value={retentionWeekly}
                  onChange={(e) => setRetentionWeekly(parseInt(e.target.value, 10) || 0)}
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
              <div>
                <label className="block text-xs text-gray-500 mb-1">Monthly copies</label>
                <input
                  type="number"
                  min={0}
                  value={retentionMonthly}
                  onChange={(e) => setRetentionMonthly(parseInt(e.target.value, 10) || 0)}
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
            </div>
          </div>

          {/* Advanced */}
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">
                Bandwidth Limit <span className="text-gray-400 font-normal">(MB/s, optional)</span>
              </label>
              <input
                type="number"
                min={0}
                step={0.1}
                value={bandwidthLimit}
                onChange={(e) => setBandwidthLimit(e.target.value)}
                placeholder="Unlimited"
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-gray-700 mb-1">Timeout (minutes)</label>
              <input
                type="number"
                min={1}
                value={timeout}
                onChange={(e) => setTimeout_(parseInt(e.target.value, 10) || 120)}
                className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              />
            </div>
          </div>

          {error && (
            <div className="bg-red-50 border border-red-200 text-red-700 rounded-lg px-4 py-3 text-sm">
              {error}
            </div>
          )}
        </form>

        {/* Footer */}
        <div className="flex items-center justify-end gap-3 px-6 py-4 border-t border-gray-100">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-sm font-medium text-gray-600 hover:text-gray-800 transition-colors"
          >
            Cancel
          </button>
          <button
            onClick={handleSubmit}
            disabled={createMutation.isPending}
            className="inline-flex items-center gap-2 px-5 py-2 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors disabled:opacity-60"
          >
            {createMutation.isPending && <Loader2 size={14} className="animate-spin" />}
            Create Job
          </button>
        </div>
      </div>
    </div>
  );
}
