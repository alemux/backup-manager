import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Plus, Server, Loader2 } from 'lucide-react';
import { serversApi } from '../api/servers';
import ServerCard from '../components/ServerCard';
import AddServerWizard from '../components/AddServerWizard';
import type { Server as ServerType } from '../types';

export default function ServersPage() {
  const [wizardOpen, setWizardOpen] = useState(false);

  const { data: servers, isLoading, isError, error } = useQuery<ServerType[]>({
    queryKey: ['servers'],
    queryFn: () => serversApi.list(),
  });

  return (
    <div className="p-6 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Servers</h1>
          <p className="text-sm text-gray-500 mt-0.5">
            Manage your backup servers and their sources.
          </p>
        </div>
        <button
          onClick={() => setWizardOpen(true)}
          className="inline-flex items-center gap-2 px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors shadow-sm"
        >
          <Plus size={16} />
          Add Server
        </button>
      </div>

      {/* Loading */}
      {isLoading && (
        <div className="flex items-center justify-center py-20">
          <Loader2 size={24} className="animate-spin text-blue-600" />
        </div>
      )}

      {/* Error */}
      {isError && (
        <div className="bg-red-50 border border-red-200 text-red-700 rounded-xl p-5 text-sm">
          Failed to load servers: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      )}

      {/* Empty state */}
      {!isLoading && !isError && servers?.length === 0 && (
        <div className="text-center py-20">
          <Server size={48} className="text-gray-300 mx-auto mb-4" />
          <h3 className="text-base font-semibold text-gray-600 mb-1">No servers configured</h3>
          <p className="text-sm text-gray-400 mb-6">
            Add your first server to get started.
          </p>
          <button
            onClick={() => setWizardOpen(true)}
            className="inline-flex items-center gap-2 px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors"
          >
            <Plus size={16} />
            Add Server
          </button>
        </div>
      )}

      {/* Server grid */}
      {!isLoading && !isError && servers && servers.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {servers.map((server) => (
            <ServerCard key={server.id} server={server} />
          ))}
        </div>
      )}

      {/* Wizard */}
      {wizardOpen && <AddServerWizard onClose={() => setWizardOpen(false)} />}
    </div>
  );
}
