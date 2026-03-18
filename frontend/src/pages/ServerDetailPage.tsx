import { useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  ArrowLeft,
  Loader2,
  AlertCircle,
  Trash2,
  Search,
  Database,
  Globe,
  Settings,
  Monitor,
  Server,
} from 'lucide-react';
import { serversApi } from '../api/servers';
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

export default function ServerDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const serverId = parseInt(id ?? '0');

  const [discoveryResult, setDiscoveryResult] = useState<DiscoveryResult | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

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

  const discoverMutation = useMutation({
    mutationFn: () => serversApi.discover(serverId),
    onSuccess: (data) => {
      setDiscoveryResult(data as DiscoveryResult);
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
        <h2 className="text-base font-bold text-gray-800 mb-4">Backup Sources</h2>
        {sourcesLoading ? (
          <div className="flex items-center gap-2 text-gray-400 text-sm">
            <Loader2 size={15} className="animate-spin" /> Loading sources...
          </div>
        ) : !sources || sources.length === 0 ? (
          <p className="text-sm text-gray-400">No backup sources configured.</p>
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
      </div>

      {/* Discovery (Linux only) */}
      {server.type === 'linux' && (
        <div className="bg-white border border-gray-200 rounded-xl p-6 mb-5">
          <div className="flex items-center justify-between mb-4">
            <h2 className="text-base font-bold text-gray-800">Discovered Services</h2>
            <button
              onClick={() => discoverMutation.mutate()}
              disabled={discoverMutation.isPending}
              className="inline-flex items-center gap-2 px-3 py-1.5 text-sm font-medium bg-gray-100 hover:bg-gray-200 text-gray-700 rounded-lg transition-colors disabled:opacity-60"
            >
              {discoverMutation.isPending ? (
                <Loader2 size={13} className="animate-spin" />
              ) : (
                <Search size={13} />
              )}
              Run Discovery
            </button>
          </div>

          {discoverMutation.isError && (
            <div className="flex items-center gap-2 text-red-600 text-sm mb-3">
              <AlertCircle size={14} />
              {discoverMutation.error instanceof Error
                ? discoverMutation.error.message
                : 'Discovery failed'}
            </div>
          )}

          {!discoveryResult && !discoverMutation.isPending && (
            <p className="text-sm text-gray-400">
              Run discovery to detect installed services (NGINX, MySQL, PM2, etc.)
            </p>
          )}

          {discoverMutation.isPending && (
            <div className="flex items-center gap-3 py-4">
              <Loader2 size={18} className="animate-spin text-blue-600" />
              <span className="text-gray-500 text-sm">Scanning server for services...</span>
            </div>
          )}

          {discoveryResult && (
            <div>
              <p className="text-xs text-gray-400 mb-3">
                Scanned: {new Date(discoveryResult.scanned_at).toLocaleString()}
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
