import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Shield, RefreshCw, Trash2, PlayCircle, Server, AlertTriangle, ChevronDown, ChevronRight } from 'lucide-react';
import { recoveryApi, type Playbook } from '../api/recovery';
import { serversApi } from '../api/servers';
import PlaybookWizard from '../components/PlaybookWizard';
import type { Server as ServerType } from '../types';

function scenarioLabel(scenario: string): string {
  switch (scenario) {
    case 'full_server': return 'Full Server';
    case 'single_database': return 'Database';
    case 'single_project': return 'Project';
    case 'config_only': return 'Config';
    case 'certificates': return 'Certificates';
    default: return scenario;
  }
}

function scenarioBadgeClass(scenario: string): string {
  switch (scenario) {
    case 'full_server': return 'bg-red-100 text-red-700';
    case 'single_database': return 'bg-blue-100 text-blue-700';
    case 'single_project': return 'bg-green-100 text-green-700';
    case 'config_only': return 'bg-yellow-100 text-yellow-700';
    case 'certificates': return 'bg-purple-100 text-purple-700';
    default: return 'bg-gray-100 text-gray-700';
  }
}

interface ServerGroupProps {
  server: ServerType;
  playbooks: Playbook[];
  onGenerate: (serverId: number) => void;
  isGenerating: boolean;
  onSelect: (playbook: Playbook) => void;
  onDelete: (id: number) => void;
}

function ServerGroup({ server, playbooks, onGenerate, isGenerating, onSelect, onDelete }: ServerGroupProps) {
  const [expanded, setExpanded] = useState(true);

  return (
    <div className="border border-gray-200 rounded-lg overflow-hidden">
      {/* Server header */}
      <div className="flex items-center justify-between px-4 py-3 bg-gray-50 border-b border-gray-200">
        <button
          className="flex items-center gap-2 flex-1 min-w-0 text-left"
          onClick={() => setExpanded(e => !e)}
        >
          {expanded ? <ChevronDown size={16} className="text-gray-500" /> : <ChevronRight size={16} className="text-gray-500" />}
          <Server size={16} className="text-gray-600 flex-shrink-0" />
          <span className="font-semibold text-gray-800 truncate">{server.name}</span>
          <span className="text-xs text-gray-500 truncate hidden sm:inline">({server.host})</span>
          <span className="ml-2 text-xs text-gray-400">{playbooks.length} playbook{playbooks.length !== 1 ? 's' : ''}</span>
        </button>
        <button
          onClick={() => onGenerate(server.id)}
          disabled={isGenerating}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 text-xs font-medium text-white bg-blue-600 hover:bg-blue-700 disabled:opacity-60 disabled:cursor-not-allowed rounded-lg transition-colors ml-2 flex-shrink-0"
        >
          <RefreshCw size={13} className={isGenerating ? 'animate-spin' : ''} />
          Generate Playbooks
        </button>
      </div>

      {/* Playbook list */}
      {expanded && (
        <div className="divide-y divide-gray-100">
          {playbooks.length === 0 ? (
            <div className="px-4 py-6 text-center text-sm text-gray-500">
              No playbooks yet. Click "Generate Playbooks" to create them automatically.
            </div>
          ) : (
            playbooks.map(p => (
              <div key={p.id} className="flex items-center justify-between px-4 py-3 hover:bg-gray-50 group">
                <div className="flex items-center gap-3 flex-1 min-w-0">
                  <span className={`inline-flex px-2 py-0.5 rounded text-xs font-medium flex-shrink-0 ${scenarioBadgeClass(p.scenario)}`}>
                    {scenarioLabel(p.scenario)}
                  </span>
                  <span className="text-sm text-gray-800 truncate">{p.title}</span>
                  <span className="text-xs text-gray-400 flex-shrink-0">
                    {p.steps?.length ?? 0} steps
                  </span>
                </div>
                <div className="flex items-center gap-2 flex-shrink-0 ml-2">
                  <button
                    onClick={() => onSelect(p)}
                    className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs font-medium text-blue-600 hover:text-blue-800 hover:bg-blue-50 rounded transition-colors"
                    title="Open wizard"
                  >
                    <PlayCircle size={14} />
                    Open
                  </button>
                  <button
                    onClick={() => {
                      if (confirm(`Delete playbook "${p.title}"?`)) {
                        onDelete(p.id);
                      }
                    }}
                    className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded transition-colors opacity-0 group-hover:opacity-100"
                    title="Delete playbook"
                  >
                    <Trash2 size={14} />
                  </button>
                </div>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

export default function RecoveryPage() {
  const queryClient = useQueryClient();
  const [selectedPlaybook, setSelectedPlaybook] = useState<Playbook | null>(null);
  const [generatingFor, setGeneratingFor] = useState<number | null>(null);
  const [generateError, setGenerateError] = useState<string | null>(null);

  const { data: playbooks = [], isLoading: loadingPlaybooks } = useQuery({
    queryKey: ['playbooks'],
    queryFn: () => recoveryApi.list(),
  });

  const { data: servers = [], isLoading: loadingServers } = useQuery({
    queryKey: ['servers'],
    queryFn: () => serversApi.list(),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: number) => recoveryApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['playbooks'] });
    },
  });

  async function handleGenerate(serverId: number) {
    setGeneratingFor(serverId);
    setGenerateError(null);
    try {
      await recoveryApi.generate(serverId);
      queryClient.invalidateQueries({ queryKey: ['playbooks'] });
    } catch (e: unknown) {
      setGenerateError(e instanceof Error ? e.message : 'Failed to generate playbooks');
    } finally {
      setGeneratingFor(null);
    }
  }

  const isLoading = loadingPlaybooks || loadingServers;

  // Group playbooks by server_id
  const playbooksByServer = playbooks.reduce<Record<number, Playbook[]>>((acc, p) => {
    const sid = p.server_id ?? 0;
    if (!acc[sid]) acc[sid] = [];
    acc[sid].push(p);
    return acc;
  }, {});

  // Also handle unassigned playbooks (server_id null)
  const unassigned = playbooksByServer[0] ?? [];

  return (
    <div className="p-6 max-w-4xl mx-auto">
      {/* Page header */}
      <div className="mb-6">
        <div className="flex items-center gap-2 mb-1">
          <Shield className="text-blue-600" size={24} />
          <h1 className="text-2xl font-bold text-gray-900">Disaster Recovery</h1>
        </div>
        <p className="text-gray-500 text-sm">
          Auto-generated, step-by-step recovery playbooks for your servers. Generate playbooks based on configured backup sources.
        </p>
      </div>

      {generateError && (
        <div className="mb-4 flex items-center gap-2 p-3 bg-red-50 border border-red-200 rounded-lg text-sm text-red-700">
          <AlertTriangle size={16} className="flex-shrink-0" />
          {generateError}
        </div>
      )}

      {isLoading ? (
        <div className="flex items-center justify-center py-16 text-gray-500">
          <RefreshCw size={20} className="animate-spin mr-2" />
          Loading playbooks...
        </div>
      ) : servers.length === 0 ? (
        <div className="text-center py-16 text-gray-500">
          <Shield size={48} className="mx-auto mb-3 text-gray-300" />
          <p className="font-medium">No servers configured</p>
          <p className="text-sm mt-1">Add servers first to generate recovery playbooks.</p>
        </div>
      ) : (
        <div className="space-y-4">
          {servers.map(server => (
            <ServerGroup
              key={server.id}
              server={server}
              playbooks={playbooksByServer[server.id] ?? []}
              onGenerate={handleGenerate}
              isGenerating={generatingFor === server.id}
              onSelect={setSelectedPlaybook}
              onDelete={(id) => deleteMutation.mutate(id)}
            />
          ))}

          {unassigned.length > 0 && (
            <div className="border border-gray-200 rounded-lg overflow-hidden">
              <div className="px-4 py-3 bg-gray-50 border-b text-sm font-medium text-gray-600">
                Unassigned Playbooks
              </div>
              <div className="divide-y divide-gray-100">
                {unassigned.map(p => (
                  <div key={p.id} className="flex items-center justify-between px-4 py-3 hover:bg-gray-50 group">
                    <div className="flex items-center gap-3 flex-1 min-w-0">
                      <span className={`inline-flex px-2 py-0.5 rounded text-xs font-medium flex-shrink-0 ${scenarioBadgeClass(p.scenario)}`}>
                        {scenarioLabel(p.scenario)}
                      </span>
                      <span className="text-sm text-gray-800 truncate">{p.title}</span>
                    </div>
                    <div className="flex items-center gap-2 flex-shrink-0">
                      <button
                        onClick={() => setSelectedPlaybook(p)}
                        className="inline-flex items-center gap-1 px-2.5 py-1.5 text-xs font-medium text-blue-600 hover:text-blue-800 hover:bg-blue-50 rounded transition-colors"
                      >
                        <PlayCircle size={14} />
                        Open
                      </button>
                      <button
                        onClick={() => {
                          if (confirm(`Delete playbook "${p.title}"?`)) {
                            deleteMutation.mutate(p.id);
                          }
                        }}
                        className="p-1.5 text-gray-400 hover:text-red-600 hover:bg-red-50 rounded transition-colors opacity-0 group-hover:opacity-100"
                      >
                        <Trash2 size={14} />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Show a note if a server group has no playbooks */}
          {servers.every(s => !(playbooksByServer[s.id]?.length > 0)) && unassigned.length === 0 && (
            <div className="text-center py-8 text-gray-500 border-2 border-dashed border-gray-200 rounded-lg">
              <Shield size={36} className="mx-auto mb-2 text-gray-300" />
              <p className="font-medium">No playbooks yet</p>
              <p className="text-sm mt-1">Click "Generate Playbooks" on any server above to get started.</p>
            </div>
          )}
        </div>
      )}

      {/* Playbook wizard modal */}
      {selectedPlaybook && (
        <PlaybookWizard
          playbook={selectedPlaybook}
          onClose={() => setSelectedPlaybook(null)}
        />
      )}
    </div>
  );
}
