import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { RefreshCw, Server, Briefcase, CheckCircle, XCircle, Activity } from 'lucide-react';
import { dashboardApi } from '../api/dashboard';
import type { DashboardSummary } from '../api/dashboard';
import ServerStatusCard from '../components/ServerStatusCard';
import BackupTimeline from '../components/BackupTimeline';
import DiskUsageChart from '../components/DiskUsageChart';
import AlertsList from '../components/AlertsList';
import { useWebSocket } from '../hooks/useWebSocket';

function StatCard({
  icon,
  label,
  value,
  sub,
  color,
}: {
  icon: React.ReactNode;
  label: string;
  value: string | number;
  sub?: string;
  color: string;
}) {
  return (
    <div className="bg-white rounded-xl border border-gray-100 shadow-sm p-4 flex items-center gap-4">
      <div className={`p-2.5 rounded-lg ${color}`}>{icon}</div>
      <div>
        <p className="text-2xl font-bold text-gray-900">{value}</p>
        <p className="text-sm text-gray-500">{label}</p>
        {sub && <p className="text-xs text-gray-400 mt-0.5">{sub}</p>}
      </div>
    </div>
  );
}

export default function DashboardPage() {
  const [liveData, setLiveData] = useState<DashboardSummary | null>(null);

  const {
    data,
    isLoading,
    isError,
    refetch,
    dataUpdatedAt,
  } = useQuery({
    queryKey: ['dashboard-summary'],
    queryFn: dashboardApi.getSummary,
    refetchInterval: 30_000,
  });

  // WebSocket for live status updates
  const { lastMessage, isConnected } = useWebSocket('/ws/status');

  // When a WS message arrives with server status, update server statuses without full refetch
  useEffect(() => {
    if (!lastMessage || !data) return;
    const msg = lastMessage as { type?: string; server_id?: number; status?: string };
    if (msg.type === 'server_status' && msg.server_id != null && msg.status) {
      setLiveData((prev) => {
        const base = prev ?? data;
        return {
          ...base,
          servers: base.servers.map((s) =>
            s.id === msg.server_id ? { ...s, status: msg.status! } : s
          ),
        };
      });
    }
  }, [lastMessage, data]);

  // Sync live data with fresh query data
  useEffect(() => {
    if (data) setLiveData(data);
  }, [data]);

  const summary = liveData ?? data;

  const lastRefresh = dataUpdatedAt
    ? new Date(dataUpdatedAt).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
    : null;

  return (
    <div className="p-6 space-y-6 max-w-screen-xl mx-auto">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-900">Dashboard</h1>
          {lastRefresh && (
            <p className="text-xs text-gray-400 mt-0.5">Last updated {lastRefresh}</p>
          )}
        </div>
        <div className="flex items-center gap-3">
          {/* Live indicator */}
          <div className="flex items-center gap-1.5 text-xs text-gray-500">
            <span
              className={`w-2 h-2 rounded-full ${
                isConnected ? 'bg-green-500 animate-pulse' : 'bg-gray-300'
              }`}
            />
            {isConnected ? 'Live' : 'Offline'}
          </div>
          <button
            onClick={() => refetch()}
            className="flex items-center gap-1.5 text-sm text-gray-500 hover:text-gray-800 bg-white border border-gray-200 rounded-lg px-3 py-1.5 shadow-sm hover:shadow transition-all"
          >
            <RefreshCw className="w-3.5 h-3.5" />
            Refresh
          </button>
        </div>
      </div>

      {/* Loading / Error states */}
      {isLoading && (
        <div className="flex justify-center py-12">
          <div className="w-8 h-8 border-2 border-blue-500 border-t-transparent rounded-full animate-spin" />
        </div>
      )}

      {isError && !summary && (
        <div className="bg-red-50 border border-red-200 rounded-xl p-4 text-red-700 text-sm">
          Failed to load dashboard data. The backend may be unavailable.
        </div>
      )}

      {summary && (
        <>
          {/* Stats row */}
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
            <StatCard
              icon={<Server className="w-5 h-5 text-blue-600" />}
              label="Servers"
              value={`${summary.stats.servers_online} / ${summary.stats.total_servers}`}
              sub="online"
              color="bg-blue-50"
            />
            <StatCard
              icon={<Briefcase className="w-5 h-5 text-purple-600" />}
              label="Active Jobs"
              value={summary.stats.total_jobs}
              color="bg-purple-50"
            />
            <StatCard
              icon={<CheckCircle className="w-5 h-5 text-green-600" />}
              label="24h Runs"
              value={summary.stats.last_24h_runs}
              sub={`${summary.stats.last_24h_success} ok, ${summary.stats.last_24h_failed} failed`}
              color="bg-green-50"
            />
            <StatCard
              icon={<XCircle className="w-5 h-5 text-red-500" />}
              label="Failed (24h)"
              value={summary.stats.last_24h_failed}
              color="bg-red-50"
            />
          </div>

          {/* Server Status + Alerts */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
            {/* Server Status */}
            <div className="bg-white rounded-xl border border-gray-100 shadow-sm p-5">
              <div className="flex items-center gap-2 mb-4">
                <Activity className="w-4 h-4 text-gray-400" />
                <h2 className="font-semibold text-gray-800">Server Status</h2>
                <span className="ml-auto text-xs text-gray-400">
                  {summary.servers.length} server{summary.servers.length !== 1 ? 's' : ''}
                </span>
              </div>
              {summary.servers.length === 0 ? (
                <p className="text-sm text-gray-400 text-center py-6">No servers configured yet.</p>
              ) : (
                <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                  {summary.servers.map((s) => (
                    <ServerStatusCard key={s.id} server={s} />
                  ))}
                </div>
              )}
            </div>

            {/* Active Alerts */}
            <div className="bg-white rounded-xl border border-gray-100 shadow-sm p-5">
              <div className="flex items-center gap-2 mb-4">
                <XCircle className="w-4 h-4 text-gray-400" />
                <h2 className="font-semibold text-gray-800">Active Alerts</h2>
                {summary.alerts.length > 0 && (
                  <span className="ml-auto inline-flex items-center justify-center w-5 h-5 rounded-full bg-red-100 text-red-600 text-xs font-bold">
                    {summary.alerts.length}
                  </span>
                )}
              </div>
              <AlertsList alerts={summary.alerts} />
            </div>
          </div>

          {/* Recent Backups Timeline */}
          <div className="bg-white rounded-xl border border-gray-100 shadow-sm p-5">
            <div className="flex items-center gap-2 mb-4">
              <RefreshCw className="w-4 h-4 text-gray-400" />
              <h2 className="font-semibold text-gray-800">Recent Backups (last 24h)</h2>
              <span className="ml-auto text-xs text-gray-400">
                {summary.recent_runs.length} run{summary.recent_runs.length !== 1 ? 's' : ''}
              </span>
            </div>
            <BackupTimeline runs={summary.recent_runs} />
          </div>

          {/* Disk Usage */}
          <div className="bg-white rounded-xl border border-gray-100 shadow-sm p-5">
            <div className="flex items-center gap-2 mb-4">
              <Server className="w-4 h-4 text-gray-400" />
              <h2 className="font-semibold text-gray-800">Disk Usage</h2>
            </div>
            <DiskUsageChart diskUsage={summary.disk_usage} />
          </div>
        </>
      )}
    </div>
  );
}
