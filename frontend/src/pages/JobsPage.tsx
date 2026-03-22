import { useCallback, useEffect, useRef, useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Plus, BriefcaseIcon, Loader2 } from 'lucide-react';
import { jobsApi, type Job } from '../api/jobs';
import JobCard from '../components/JobCard';
import CreateJobModal from '../components/CreateJobModal';
import RunHistory from '../components/RunHistory';
import BackupTerminal, { type LogEntry } from '../components/BackupTerminal';
import RunningBackup, { type BackupProgress } from '../components/RunningBackup';
import { useWebSocket } from '../hooks/useWebSocket';

export default function JobsPage() {
  const queryClient = useQueryClient();
  const [createOpen, setCreateOpen] = useState(false);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [terminalExpanded, setTerminalExpanded] = useState(false);
  const [progress, setProgress] = useState<BackupProgress | null>(null);
  const [stopping, setStopping] = useState(false);
  const logIdCounter = useRef(0);

  const { data: jobs, isLoading, isError, error } = useQuery<Job[]>({
    queryKey: ['jobs'],
    queryFn: () => jobsApi.list(),
    refetchInterval: 30000,
  });

  const stopMutation = useMutation({
    mutationFn: (id: number) => jobsApi.stop(id),
    onMutate: () => {
      setStopping(true);
    },
    onSettled: () => {
      queryClient.invalidateQueries({ queryKey: ['jobs'] });
      queryClient.invalidateQueries({ queryKey: ['runs'] });
    },
    onError: () => {
      setStopping(false);
    },
  });

  const { lastMessage } = useWebSocket('/ws/status');

  // Process incoming WebSocket messages
  useEffect(() => {
    if (!lastMessage) return;
    const msg = lastMessage as {
      type?: string;
      data?: Record<string, unknown>;
      timestamp?: string;
    };

    if (msg.type === 'progress') {
      const d = msg.data as BackupProgress | undefined;
      if (d) {
        const terminalStatus = d.status as string | undefined;
        if (terminalStatus === 'complete' || terminalStatus === 'skipped') {
          // Show completion state briefly then clear
          setProgress(d);
          setStopping(false);
          setTimeout(() => {
            setProgress(null);
            queryClient.invalidateQueries({ queryKey: ['jobs'] });
            queryClient.invalidateQueries({ queryKey: ['runs'] });
          }, 4000);
        } else {
          setProgress(d);
          setStopping(false); // reset stopping once new progress arrives
        }
      }
    } else if (msg.type === 'log') {
      const raw = (msg.data?.message as string) ?? '';
      const rawLevel = (msg.data?.level as string) ?? 'info';

      // Infer level from message content too
      let level: LogEntry['level'] = 'info';
      if (rawLevel === 'error' || raw.toUpperCase().startsWith('ERROR')) {
        level = 'error';
      } else if (rawLevel === 'warn' || raw.toLowerCase().includes('warning')) {
        level = 'warn';
      } else if (raw.toLowerCase().includes('complete') && raw.toLowerCase().includes('success')) {
        level = 'success';
      } else if (rawLevel === 'success') {
        level = 'success';
      }

      const entry: LogEntry = {
        id: ++logIdCounter.current,
        timestamp: msg.timestamp ? new Date(msg.timestamp) : new Date(),
        message: raw,
        level,
      };

      setLogs((prev) => [...prev, entry]);
      // Auto-expand terminal when logs arrive
      setTerminalExpanded(true);
    } else if (msg.type === 'status') {
      // When status changes away from running, clear progress and stopping state
      const d = msg.data as { status?: string } | undefined;
      if (d?.status && d.status !== 'running') {
        setProgress(null);
        setStopping(false);
        queryClient.invalidateQueries({ queryKey: ['jobs'] });
        queryClient.invalidateQueries({ queryKey: ['runs'] });
      }
    }
  }, [lastMessage, queryClient]);

  const handleRunNow = useCallback(() => {
    setTerminalExpanded(true);
  }, []);

  const handleStop = useCallback(
    (jobId: number) => {
      stopMutation.mutate(jobId);
    },
    [stopMutation],
  );

  return (
    <div className="p-6 max-w-7xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Backup Jobs</h1>
          <p className="text-sm text-gray-500 mt-0.5">
            Schedule and manage automated backup jobs.
          </p>
        </div>
        <button
          onClick={() => setCreateOpen(true)}
          className="inline-flex items-center gap-2 px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors shadow-sm"
        >
          <Plus size={16} />
          Create Job
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
          Failed to load jobs: {error instanceof Error ? error.message : 'Unknown error'}
        </div>
      )}

      {/* Empty state */}
      {!isLoading && !isError && jobs?.length === 0 && (
        <div className="text-center py-20">
          <BriefcaseIcon size={48} className="text-gray-300 mx-auto mb-4" />
          <h3 className="text-base font-semibold text-gray-600 mb-1">No backup jobs yet</h3>
          <p className="text-sm text-gray-400 mb-6">
            Create your first backup job to start protecting your data.
          </p>
          <button
            onClick={() => setCreateOpen(true)}
            className="inline-flex items-center gap-2 px-4 py-2.5 bg-blue-600 hover:bg-blue-700 text-white text-sm font-semibold rounded-lg transition-colors"
          >
            <Plus size={16} />
            Create Job
          </button>
        </div>
      )}

      {/* Jobs grid */}
      {!isLoading && !isError && jobs && jobs.length > 0 && (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
          {jobs.map((job) => (
            <JobCard key={job.id} job={job} onRunNow={handleRunNow} />
          ))}
        </div>
      )}

      {/* Running Backup progress card */}
      {!isLoading && !isError && (
        <RunningBackup
          progress={progress}
          onStop={handleStop}
          stopping={stopping}
          onDismiss={() => setProgress(null)}
        />
      )}

      {/* Backup Terminal */}
      {!isLoading && !isError && (
        <div className="mb-6">
          <BackupTerminal
            logs={logs}
            onClear={() => setLogs([])}
            expanded={terminalExpanded}
            onToggle={() => setTerminalExpanded((v) => !v)}
          />
        </div>
      )}

      {/* Run history */}
      {!isLoading && !isError && (
        <div className="bg-white border border-gray-200 rounded-xl p-5">
          <RunHistory />
        </div>
      )}

      {/* Create modal */}
      {createOpen && <CreateJobModal onClose={() => setCreateOpen(false)} />}
    </div>
  );
}
