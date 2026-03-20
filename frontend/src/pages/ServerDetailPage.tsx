import { useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  ArrowLeft,
  Loader2,
  AlertCircle,
  Trash2,
  RefreshCw,
  Database,
  Globe,
  Settings,
  Monitor,
  Server,
  Clock,
  Plus,
} from 'lucide-react';
import { serversApi } from '../api/servers';
import type { DiscoveryChange } from '../api/servers';
import type { Server as ServerType, BackupSource, DiscoveryResult } from '../types';

function StatusBadge({ status }: { status: ServerType['status'] }) {
  const styles: Record<ServerType['status'], string> = {
    online: 'bg-green-100 text-green-700',
    offline: 'bg-red-100 text-red-700',
    warning: 'bg-yellow-100 text-yellow-700',
    unknown: 'bg-gray-100 text-gray-600',
  };
  const dots: Record<ServerType['status'], string> = {
    online: 'bg-green-500',
    offline: 'bg-red-500',
    warning: 'bg-yellow-400',
    unknown: 'bg-gray-400',
  };
  return (
    <span className={`inline-flex items-center gap-1.5 text-xs font-medium px-2.5 py-1 rounded-full capitalize ${styles[status]}`}>
      <span className={`w-1.5 h-1.5 rounded-full ${dots[status]}`} />
      {status}
    </span>
  );
}

function SourceTypeBadge({ type }: { type: BackupSource['type'] }) {
  const styles = {
    web: 'bg-green-100 text-green-700',
    database: 'bg-purple-100 text-purple-700',
    config: 'bg-gray-100 text-gray-600',
  };
  const icons = {
    web: <Globe size={12} />,
    database: <Database size={12} />,
    config: <Settings size={12} />,
  };
  return (
    <span className={`inline-flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full capitalize ${styles[type]}`}>
      {icons[type]} {type}
    </span>
  );
}

function ChangeBadge({ type }: { type: string }) {
  if (type === 'added') return <span className="text-xs font-semibold text-green-700 bg-green-100 px-2 py-0.5 rounded-full">added</span>;
  if (type === 'removed') return <span className="text-xs font-semibold text-red-700 bg-red-100 px-2 py-0.5 rounded-full">removed</span>;
  return <span className="text-xs font-semibold text-yellow-700 bg-yellow-100 px-2 py-0.5 rounded-full">changed</span>;
}

function ChangesList({ changes, serverId, onSourceAdded }: { changes: DiscoveryChange[]; serverId: number; onSourceAdded: () => void }) {
  const [adding, setAdding] = useState<string | null>(null);

  if (changes.length === 0) {
    return (
      <div className="mt-4 p-4 bg-green-50 border border-green-200 rounded-lg text-sm text-green-700 flex items-center gap-2">
        No changes detected since last scan.
      </div>
    );
  }

  const addAsSource = async (change: DiscoveryChange) => {
    setAdding(change.name);
    try {
      let sourceData: Record<string, string>;
      if (change.category === 'database') {
        sourceData = { name: change.name, type: 'database', db_name: change.name };
      } else if (change.category === 'vhost') {
        sourceData = { name: change.name, type: 'web', source_path: `/var/www/${change.name}` };
      } else if (change.category === 'process') {
        sourceData = { name: `${change.name} (PM2)`, type: 'config', source_path: `/home/${change.name}` };
      } else {
        sourceData = { name: change.name, type: 'config', source_path: `/etc/${change.name}` };
      }
      await serversApi.createSource(serverId, sourceData);
      onSourceAdded();
    } catch (e) {
      alert(e instanceof Error ? e.message : 'Failed to add source');
    } finally {
      setAdding(null);
    }
  };

  return (
    <div className="mt-4 p-4 bg-amber-50 border border-amber-200 rounded-lg">
      <p className="text-sm font-semibold text-amber-800 mb-3">
        {changes.length} change{changes.length !== 1 ? 's' : ''} detected since last scan
      </p>
      <div className="space-y-2">
        {changes.map((c, i) => (
          <div key={i} className="flex items-center gap-3 text-sm">
            <ChangeBadge type={c.type} />
            <div className="flex-1">
              <span className="font-medium text-gray-800">{c.name}</span>
              <span className="text-gray-500 ml-1.5 text-xs">({c.category})</span>
              <p className="text-xs text-gray-500 mt-0.5">{c.details}</p>
            </div>
            {c.type === 'added' && (
              <button
                onClick={() => addAsSource(c)}
                disabled={adding === c.name}
                className="inline-flex items-center gap-1 px-2.5 py-1 text-xs font-medium bg-green-600 text-white rounded-lg hover:bg-green-700 disabled:opacity-50 whitespace-nowrap"
              >
                {adding === c.name ? (
                  <Loader2 size={11} className="animate-spin" />
                ) : (
                  <Plus size={11} />
                )}
                Add as source
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

export default function ServerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const serverId = parseInt(id ?? '0');

  const [rescanChanges, setRescanChanges] = useState<DiscoveryChange[] | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [showAddSource, setShowAddSource] = useState(false);
  const [newSource, setNewSource] = useState({ name: '', type: 'web', source_path: '', db_name: '' });
  const [addingSource, setAddingSource] = useState(false);
  const [addSourceError, setAddSourceError] = useState<string | null>(null);

  const handleAddSource = async () => {
    setAddingSource(true);
    setAddSourceError(null);
    try {
      const payload: Record<string, string> = { name: newSource.name, type: newSource.type };
      if (newSource.type === 'database') {
        payload.db_name = newSource.db_name;
      } else {
        payload.source_path = newSource.source_path;
      }
      await serversApi.createSource(serverId, payload);
      queryClient.invalidateQueries({ queryKey: ['server-sources', serverId] });
      setShowAddSource(false);
      setNewSource({ name: '', type: 'web', source_path: '', db_name: '' });
    } catch (e) {
      setAddSourceError(e instanceof Error ? e.message : 'Failed to add source');
    } finally {
      setAddingSource(false);
    }
  };

  const { data: server, isLoading: serverLoading, isError: serverError } = useQuery<ServerType>({
    queryKey: ['server', serverId],
    queryFn: () => serversApi.get(serverId),
    enabled: !!serverId,
  });

  const { data: sources, isLoading: sourcesLoading } = useQuery<BackupSource[]>({
    queryKey: ['server-sources', serverId],
    queryFn: () => serversApi.listSources(serverId),
    enabled: !!serverId,
  });

  // Load previous discovery results on page mount (Linux servers only)
  const { data: prevDiscovery, isLoading: prevDiscoveryLoading } = useQuery<DiscoveryResult>({
    queryKey: ['server-discovery', serverId],
    queryFn: () => serversApi.getPreviousDiscovery(serverId),
    enabled: !!serverId && server?.type === 'linux',
    retry: false,
  });

  // Active discovery result: either the latest rescan or the previously saved one
  const [latestDiscovery, setLatestDiscovery] = useState<DiscoveryResult | null>(null);
  const discoveryResult: DiscoveryResult | null = latestDiscovery ?? prevDiscovery ?? null;

  const rescanMutation = useMutation({
    mutationFn: () => serversApi.rescan(serverId),
    onSuccess: (data) => {
      setLatestDiscovery(data.discovery);
      setRescanChanges(data.changes);
      queryClient.invalidateQueries({ queryKey: ['server-discovery', serverId] });
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => serversApi.delete(serverId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['servers'] });
      navigate('/servers');
    },
  });

  if (serverLoading) {
    return (
      <div className="flex items-center justify-center py-20">
        <Loader2 size={24} className="animate-spin text-blue-600" />
      </div>
    );
  }

  if (serverError || !server) {
    return (
      <div className="p-6">
        <div className="bg-red-50 border border-red-200 text-red-700 rounded-xl p-5 text-sm flex items-center gap-2">
          <AlertCircle size={16} />
          Server not found or failed to load.
        </div>
        <Link to="/servers" className="inline-flex items-center gap-1.5 text-sm text-blue-600 hover:underline mt-4">
          <ArrowLeft size={15} /> Back to Servers
        </Link>
      </div>
    );
  }

  return (
    <div className="p-6 max-w-4xl mx-auto">
      {/* Back link */}
      <Link
        to="/servers"
        className="inline-flex items-center gap-1.5 text-sm text-gray-500 hover:text-gray-800 mb-5 transition-colors"
      >
        <ArrowLeft size={15} /> Back to Servers
      </Link>

      {/* Server header */}
      <div className="bg-white border border-gray-200 rounded-xl p-6 mb-5">
        <div className="flex items-start justify-between flex-wrap gap-4">
          <div className="flex items-center gap-4">
            <div className="w-12 h-12 rounded-xl bg-gray-100 flex items-center justify-center">
              {server.type === 'linux' ? (
                <Monitor size={24} className="text-green-600" />
              ) : (
                <Server size={24} className="text-blue-600" />
              )}
            </div>
            <div>
              <h1 className="text-xl font-bold text-gray-900">{server.name}</h1>
              <p className="text-sm text-gray-500 font-mono mt-0.5">
                {server.host}:{server.port}
              </p>
            </div>
          </div>
          <div className="flex items-center gap-3">
            <span
              className={`inline-flex items-center gap-1 text-xs font-medium px-2.5 py-1 rounded-full ${
                server.type === 'linux'
                  ? 'bg-green-100 text-green-700'
                  : 'bg-blue-100 text-blue-700'
              }`}
            >
              {server.type === 'linux' ? <Monitor size={11} /> : <Server size={11} />}
              {server.type === 'linux' ? 'Linux' : 'Windows'}
            </span>
            <StatusBadge status={server.status} />
          </div>
        </div>
        <div className="mt-4 pt-4 border-t border-gray-100 grid grid-cols-2 sm:grid-cols-3 gap-4 text-sm text-gray-600">
          <div>
            <span className="block text-xs text-gray-400 font-medium mb-0.5">Connection</span>
            <span className="uppercase font-semibold text-gray-700">{server.connection_type}</span>
          </div>
          <div>
            <span className="block text-xs text-gray-400 font-medium mb-0.5">Added</span>
            <span>{new Date(server.created_at).toLocaleDateString()}</span>
          </div>
        </div>
      </div>

      {/* Backup Sources */}
      <div className="bg-white border border-gray-200 rounded-xl p-6 mb-5">
        <div className="flex items-center justify-between mb-4">
          <h2 className="text-base font-bold text-gray-800">Backup Sources</h2>
          <button
            onClick={() => setShowAddSource(true)}
            className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700"
          >
            <Plus size={13} /> Add Source
          </button>
        </div>
        {sourcesLoading ? (
          <div className="flex items-center gap-2 text-gray-400 text-sm">
            <Loader2 size={15} className="animate-spin" /> Loading sources...
          </div>
        ) : !sources || sources.length === 0 ? (
          <p className="text-sm text-gray-400">No backup sources configured. Add your first source to start backing up.</p>
        ) : (
          <div className="divide-y divide-gray-100">
            {sources.map((src) => (
              <div key={src.id} className="py-3 flex items-center gap-3">
                <SourceTypeBadge type={src.type} />
                <div className="flex-1 min-w-0">
                  <p className="text-sm font-medium text-gray-800">{src.name}</p>
                  {src.source_path && (
                    <p className="text-xs text-gray-400 font-mono truncate">{src.source_path}</p>
                  )}
                  {src.db_name && (
                    <p className="text-xs text-gray-400 font-mono">{src.db_name}</p>
                  )}
                </div>
                <span
                  className={`text-xs px-2 py-0.5 rounded-full font-medium ${
                    src.enabled ? 'bg-green-100 text-green-700' : 'bg-gray-100 text-gray-400'
                  }`}
                >
                  {src.enabled ? 'Enabled' : 'Disabled'}
                </span>
              </div>
            ))}
          </div>
        )}

        {/* Add Source Form */}
        {showAddSource && (
          <div className="mt-4 pt-4 border-t border-gray-200">
            <h3 className="text-sm font-semibold text-gray-700 mb-3">Add Backup Source</h3>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">Name</label>
                <input
                  type="text"
                  value={newSource.name}
                  onChange={(e) => setNewSource((s) => ({ ...s, name: e.target.value }))}
                  placeholder="My Project"
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-gray-600 mb-1">Type</label>
                <select
                  value={newSource.type}
                  onChange={(e) => setNewSource((s) => ({ ...s, type: e.target.value }))}
                  className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
                >
                  <option value="web">Web Files</option>
                  <option value="database">Database</option>
                  <option value="config">Configuration</option>
                </select>
              </div>
              {newSource.type !== 'database' ? (
                <div className="sm:col-span-2">
                  <label className="block text-xs font-medium text-gray-600 mb-1">Path</label>
                  <input
                    type="text"
                    value={newSource.source_path}
                    onChange={(e) => setNewSource((s) => ({ ...s, source_path: e.target.value }))}
                    placeholder="/var/www/myproject"
                    className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
              ) : (
                <div className="sm:col-span-2">
                  <label className="block text-xs font-medium text-gray-600 mb-1">Database Name</label>
                  <input
                    type="text"
                    value={newSource.db_name}
                    onChange={(e) => setNewSource((s) => ({ ...s, db_name: e.target.value }))}
                    placeholder="my_database"
                    className="w-full border border-gray-300 rounded-lg px-3 py-2 text-sm font-mono focus:outline-none focus:ring-2 focus:ring-blue-500"
                  />
                </div>
              )}
            </div>
            {addSourceError && <p className="text-sm text-red-600 mt-2">{addSourceError}</p>}
            <div className="flex items-center gap-2 mt-3">
              <button
                onClick={handleAddSource}
                disabled={addingSource}
                className="px-4 py-2 text-sm font-medium bg-blue-600 text-white rounded-lg hover:bg-blue-700 disabled:opacity-50"
              >
                {addingSource ? 'Adding...' : 'Add'}
              </button>
              <button
                onClick={() => { setShowAddSource(false); setAddSourceError(null); }}
                className="px-4 py-2 text-sm font-medium text-gray-600 border border-gray-300 rounded-lg hover:bg-gray-50"
              >
                Cancel
              </button>
            </div>
          </div>
        )}
      </div>

      {/* Discovery (Linux only) */}
      {server.type === 'linux' && (
        <div className="bg-white border border-gray-200 rounded-xl p-6 mb-5">
          <div className="flex items-center justify-between mb-1">
            <h2 className="text-base font-bold text-gray-800">Discovered Services</h2>
            <div className="flex items-center gap-3">
              <span className="text-xs text-gray-400 flex items-center gap-1">
                <Clock size={11} /> Auto-scan: every 24h
              </span>
              <button
                onClick={() => { setRescanChanges(null); rescanMutation.mutate(); }}
                disabled={rescanMutation.isPending}
                className="inline-flex items-center gap-2 px-3 py-1.5 text-sm font-medium bg-blue-50 hover:bg-blue-100 text-blue-700 rounded-lg border border-blue-200 transition-colors disabled:opacity-60"
              >
                {rescanMutation.isPending ? (
                  <Loader2 size={13} className="animate-spin" />
                ) : (
                  <RefreshCw size={13} />
                )}
                Rescan
              </button>
            </div>
          </div>

          {rescanMutation.isError && (
            <div className="flex items-center gap-2 text-red-600 text-sm mb-3 mt-2">
              <AlertCircle size={14} />
              {rescanMutation.error instanceof Error
                ? rescanMutation.error.message
                : 'Rescan failed'}
            </div>
          )}

          {/* Show change results after a rescan */}
          {rescanChanges !== null && <ChangesList changes={rescanChanges} serverId={serverId} onSourceAdded={() => queryClient.invalidateQueries({ queryKey: ['server-sources', serverId] })} />}

          {/* Loading state */}
          {(prevDiscoveryLoading || rescanMutation.isPending) && (
            <div className="flex items-center gap-3 py-4 mt-2">
              <Loader2 size={18} className="animate-spin text-blue-600" />
              <span className="text-gray-500 text-sm">
                {rescanMutation.isPending ? 'Scanning server for services...' : 'Loading last scan results...'}
              </span>
            </div>
          )}

          {/* Last scan results */}
          {!prevDiscoveryLoading && !rescanMutation.isPending && discoveryResult && (
            <div className="mt-4">
              <p className="text-xs text-gray-400 mb-3 flex items-center gap-1">
                <Clock size={11} />
                Last scan: {discoveryResult.scanned_at
                  ? new Date(discoveryResult.scanned_at).toLocaleString()
                  : 'unknown'}
              </p>
              {discoveryResult.services.length === 0 ? (
                <p className="text-sm text-gray-400">No services detected.</p>
              ) : (
                <div className="space-y-2">
                  {discoveryResult.services.map((svc) => (
                    <div key={svc.name} className="border border-gray-100 rounded-lg p-3">
                      <p className="text-sm font-semibold capitalize text-gray-800 mb-1">{svc.name}</p>
                      <div className="text-xs text-gray-500 font-mono">
                        {Object.entries(svc.data).map(([k, v]) => (
                          <div key={k}>
                            <span className="font-medium">{k}:</span> {JSON.stringify(v)}
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          )}

          {!prevDiscoveryLoading && !rescanMutation.isPending && !discoveryResult && (
            <p className="text-sm text-gray-400 mt-3">
              No previous scan results. Click Rescan to detect installed services.
            </p>
          )}
        </div>
      )}

      {/* Danger Zone */}
      <div className="bg-white border border-red-200 rounded-xl p-6">
        <h2 className="text-base font-bold text-red-700 mb-3">Danger Zone</h2>
        {!confirmDelete ? (
          <button
            onClick={() => setConfirmDelete(true)}
            className="inline-flex items-center gap-2 px-4 py-2 bg-red-50 hover:bg-red-100 text-red-600 text-sm font-medium rounded-lg border border-red-200 transition-colors"
          >
            <Trash2 size={14} />
            Delete Server
          </button>
        ) : (
          <div className="flex items-center gap-3">
            <p className="text-sm text-red-700 font-medium">
              Are you sure? This will permanently delete this server and all its sources.
            </p>
            <button
              onClick={() => deleteMutation.mutate()}
              disabled={deleteMutation.isPending}
              className="px-4 py-2 bg-red-600 hover:bg-red-700 text-white text-sm font-semibold rounded-lg transition-colors disabled:opacity-60 flex items-center gap-2"
            >
              {deleteMutation.isPending && <Loader2 size={13} className="animate-spin" />}
              Yes, Delete
            </button>
            <button
              onClick={() => setConfirmDelete(false)}
              className="px-4 py-2 bg-gray-100 hover:bg-gray-200 text-gray-700 text-sm font-medium rounded-lg transition-colors"
            >
              Cancel
            </button>
          </div>
        )}
        {deleteMutation.isError && (
          <p className="text-red-600 text-sm mt-2 flex items-center gap-1.5">
            <AlertCircle size={14} />
            {deleteMutation.error instanceof Error ? deleteMutation.error.message : 'Delete failed'}
          </p>
        )}
      </div>
    </div>
  );
}
