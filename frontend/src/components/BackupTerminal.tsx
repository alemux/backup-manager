import { useEffect, useRef, useState } from 'react';
import { ChevronDown, ChevronUp, Terminal, Trash2 } from 'lucide-react';

export interface LogEntry {
  id: number;
  timestamp: Date;
  message: string;
  level: 'info' | 'warn' | 'error' | 'success';
}

interface BackupTerminalProps {
  logs: LogEntry[];
  onClear: () => void;
  expanded: boolean;
  onToggle: () => void;
}

function levelColor(level: LogEntry['level']): string {
  switch (level) {
    case 'error':
      return 'text-red-400';
    case 'warn':
      return 'text-yellow-400';
    case 'success':
      return 'text-green-400';
    case 'info':
    default:
      return 'text-gray-200';
  }
}

function formatTime(date: Date): string {
  return date.toLocaleTimeString('en-GB', { hour12: false });
}

export default function BackupTerminal({ logs, onClear, expanded, onToggle }: BackupTerminalProps) {
  const bottomRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const containerRef = useRef<HTMLDivElement>(null);

  // Auto-scroll when new logs arrive
  useEffect(() => {
    if (autoScroll && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logs, autoScroll]);

  // Detect manual scroll up to disable auto-scroll
  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const isAtBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    setAutoScroll(isAtBottom);
  };

  return (
    <div className="bg-gray-900 border border-gray-700 rounded-xl overflow-hidden">
      {/* Header bar */}
      <div className="flex items-center justify-between px-4 py-2.5 bg-gray-800 border-b border-gray-700">
        <div className="flex items-center gap-2">
          <Terminal size={15} className="text-green-400" />
          <span className="text-sm font-semibold text-gray-200">Backup Terminal</span>
          {logs.length > 0 && (
            <span className="text-xs text-gray-500 font-mono">{logs.length} line{logs.length !== 1 ? 's' : ''}</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={onClear}
            title="Clear terminal"
            className="inline-flex items-center gap-1.5 px-2 py-1 text-xs text-gray-400 hover:text-gray-200 hover:bg-gray-700 rounded transition-colors"
          >
            <Trash2 size={12} />
            Clear
          </button>
          <button
            onClick={onToggle}
            title={expanded ? 'Collapse' : 'Expand'}
            className="inline-flex items-center gap-1.5 px-2 py-1 text-xs text-gray-400 hover:text-gray-200 hover:bg-gray-700 rounded transition-colors"
          >
            {expanded ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
          </button>
        </div>
      </div>

      {/* Terminal body */}
      {expanded && (
        <div
          ref={containerRef}
          onScroll={handleScroll}
          className="h-64 overflow-y-auto p-3 font-mono text-xs leading-5"
          style={{ background: '#0d1117' }}
        >
          {logs.length === 0 ? (
            <span className="text-gray-600 select-none">No active backup — terminal is idle.</span>
          ) : (
            <>
              {logs.map((entry) => (
                <div key={entry.id} className="flex gap-2 hover:bg-white/5 px-1 rounded">
                  <span className="text-gray-600 shrink-0 select-none">[{formatTime(entry.timestamp)}]</span>
                  <span className={levelColor(entry.level)}>{entry.message}</span>
                </div>
              ))}
              <div ref={bottomRef} />
            </>
          )}
        </div>
      )}
    </div>
  );
}
