import { useNavigate } from 'react-router-dom';
import { Monitor, Server, Wifi, HardDrive } from 'lucide-react';
import type { Server as ServerType } from '../types';

interface ServerCardProps {
  server: ServerType;
}

function StatusDot({ status }: { status: ServerType['status'] }) {
  const colors: Record<ServerType['status'], string> = {
    online: 'bg-green-500',
    offline: 'bg-red-500',
    warning: 'bg-yellow-400',
    unknown: 'bg-gray-400',
  };
  const labels: Record<ServerType['status'], string> = {
    online: 'Online',
    offline: 'Offline',
    warning: 'Warning',
    unknown: 'Unknown',
  };
  return (
    <span className="flex items-center gap-1.5 text-sm text-gray-500">
      <span className={`inline-block w-2 h-2 rounded-full ${colors[status]}`} />
      {labels[status]}
    </span>
  );
}

export default function ServerCard({ server }: ServerCardProps) {
  const navigate = useNavigate();

  const typeBadge =
    server.type === 'linux' ? (
      <span className="inline-flex items-center gap-1 text-xs font-medium bg-green-100 text-green-700 px-2 py-0.5 rounded-full">
        <Monitor size={11} />
        Linux
      </span>
    ) : (
      <span className="inline-flex items-center gap-1 text-xs font-medium bg-blue-100 text-blue-700 px-2 py-0.5 rounded-full">
        <Server size={11} />
        Windows
      </span>
    );

  const connIcon =
    server.connection_type === 'ssh' ? (
      <Wifi size={13} className="text-gray-400" />
    ) : (
      <HardDrive size={13} className="text-gray-400" />
    );

  return (
    <div
      onClick={() => navigate(`/servers/${server.id}`)}
      className="bg-white border border-gray-200 rounded-xl p-5 cursor-pointer transition-all hover:shadow-md hover:border-gray-300 select-none"
    >
      <div className="flex items-start justify-between mb-3">
        <h3 className="font-semibold text-gray-900 text-base leading-tight pr-2">
          {server.name}
        </h3>
        {typeBadge}
      </div>

      <p className="text-sm text-gray-500 font-mono mb-3">
        {server.host}:{server.port}
      </p>

      <div className="flex items-center justify-between">
        <StatusDot status={server.status} />
        <span className="flex items-center gap-1 text-xs text-gray-400 uppercase tracking-wide font-medium">
          {connIcon}
          {server.connection_type}
        </span>
      </div>
    </div>
  );
}
