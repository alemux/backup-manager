import {
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  Cell,
  LabelList,
} from 'recharts';
import type { DiskUsage } from '../api/dashboard';

interface Props {
  diskUsage: DiskUsage[];
}

function formatBytes(bytes: number): string {
  if (!bytes) return '0 B';
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(1)} TB`;
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`;
  if (bytes >= 1e6) return `${(bytes / 1e6).toFixed(1)} MB`;
  return `${(bytes / 1e3).toFixed(1)} KB`;
}

function usageColor(pct: number): string {
  if (pct >= 90) return '#ef4444'; // red
  if (pct >= 70) return '#f59e0b'; // yellow
  return '#22c55e'; // green
}

interface TooltipPayload {
  payload?: {
    name: string;
    use_percent: number;
    used_bytes: number;
    total_bytes: number;
    free_bytes: number;
  };
}

function CustomTooltip({ payload }: { payload?: TooltipPayload[] }) {
  if (!payload || payload.length === 0) return null;
  const d = payload[0]?.payload;
  if (!d) return null;
  return (
    <div className="bg-gray-900 text-white text-xs rounded-lg shadow-xl p-3 space-y-1">
      <p className="font-semibold">{d.name}</p>
      <p>Used: {formatBytes(d.used_bytes)}</p>
      <p>Free: {formatBytes(d.free_bytes)}</p>
      <p>Total: {formatBytes(d.total_bytes)}</p>
      <p>Usage: {d.use_percent.toFixed(1)}%</p>
    </div>
  );
}

export default function DiskUsageChart({ diskUsage }: Props) {
  if (!diskUsage || diskUsage.length === 0) {
    return (
      <div className="text-center py-8 text-gray-400 text-sm">No disk usage data available.</div>
    );
  }

  const data = diskUsage.map((d) => ({
    name: d.path,
    use_percent: Math.round(d.use_percent * 10) / 10,
    used_bytes: d.used_bytes,
    free_bytes: d.free_bytes,
    total_bytes: d.total_bytes,
    label: `${formatBytes(d.used_bytes)} / ${formatBytes(d.total_bytes)}`,
  }));

  return (
    <div className="space-y-4">
      <ResponsiveContainer width="100%" height={Math.max(80, data.length * 56)}>
        <BarChart
          layout="vertical"
          data={data}
          margin={{ top: 0, right: 80, bottom: 0, left: 20 }}
        >
          <XAxis
            type="number"
            domain={[0, 100]}
            tickFormatter={(v) => `${v}%`}
            tick={{ fontSize: 11, fill: '#9ca3af' }}
            axisLine={false}
            tickLine={false}
          />
          <YAxis
            type="category"
            dataKey="name"
            tick={{ fontSize: 12, fill: '#6b7280' }}
            width={80}
            axisLine={false}
            tickLine={false}
          />
          <Tooltip content={<CustomTooltip />} cursor={{ fill: 'rgba(0,0,0,0.04)' }} />
          <Bar dataKey="use_percent" radius={[0, 4, 4, 0]} maxBarSize={24}>
            {data.map((entry, index) => (
              <Cell key={`cell-${index}`} fill={usageColor(entry.use_percent)} />
            ))}
            <LabelList
              dataKey="label"
              position="right"
              style={{ fontSize: 11, fill: '#6b7280' }}
            />
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </div>
  );
}
