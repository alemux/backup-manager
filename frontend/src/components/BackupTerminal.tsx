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

interface LineStyle {
  color: string;
  prefix?: string;
  bold?: boolean;
  dim?: boolean;
  isPhase?: boolean;
}

function classifyLine(level: LogEntry['level'], message: string): LineStyle {
  const lower = message.toLowerCase();
  const trimmed = message.trim();

  // Phase separator lines
  if (trimmed.startsWith('═') || trimmed.startsWith('=====')) {
    return { color: 'text-blue-400', bold: true, isPhase: true };
  }

  // "Starting backup" header
  if (lower.startsWith('starting backup')) {
    return { color: 'text-blue-300', bold: true };
  }

  // Analysis complete
  if (lower.startsWith('analysis complete')) {
    return { color: 'text-green-400', bold: true };
  }

  // Nothing to backup
  if (lower.includes('nothing to backup') || lower.includes('nothing to transfer')) {
    return { color: 'text-green-400', bold: true, prefix: '✓ ' };
  }

  // Analyzing sources
  if (lower.startsWith('analyz')) {
    return { color: 'text-yellow-300' };
  }

  // Backup complete (success)
  if (lower.startsWith('backup complete') && lower.includes('success')) {
    return { color: 'text-green-400', bold: true, prefix: '✓ ' };
  }

  // Backup complete (failure)
  if (lower.startsWith('backup complete') && !lower.includes('success')) {
    return { color: 'text-red-400', bold: true };
  }

  // Source summary lines (indented with spaces)
  if (message.startsWith('  ') || message.startsWith('\t')) {
    return { color: 'text-gray-400', dim: true };
  }

  // File transfer lines from lftp/rsync
  if (
    lower.startsWith('transferring file') ||
    lower.startsWith('sending file') ||
    lower.startsWith('getting file') ||
    lower.includes("transferring '") ||
    lower.includes("getting '")
  ) {
    return { color: 'text-cyan-500', dim: true };
  }

  // Error lines
  if (level === 'error' || lower.startsWith('error') || lower.startsWith('err:')) {
    return { color: 'text-red-400' };
  }

  // Warning lines
  if (level === 'warn' || lower.includes('warning') || lower.startsWith('warn')) {
    return { color: 'text-yellow-400' };
  }

  // Success
  if (level === 'success') {
    return { color: 'text-green-400' };
  }

  // Default info
  return { color: 'text-gray-300' };
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
            <span className="text-xs text-gray-500 font-mono">
              {logs.length} line{logs.length !== 1 ? 's' : ''}
            </span>
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
          className="max-h-96 overflow-y-auto p-3 font-mono text-xs leading-5"
          style={{ background: '#0d1117' }}
        >
          {logs.length === 0 ? (
            <span className="text-gray-600 select-none">No active backup — terminal is idle.</span>
          ) : (
            <>
              {logs.map((entry) => {
                const style = classifyLine(entry.level, entry.message);
                if (style.isPhase) {
                  return (
                    <div key={entry.id} className={`my-1 ${style.color} font-bold select-none`}>
                      {entry.message}
                    </div>
                  );
                }
                return (
                  <div key={entry.id} className="flex gap-2 hover:bg-white/5 px-1 rounded">
                    <span className="text-gray-600 shrink-0 select-none">[{formatTime(entry.timestamp)}]</span>
                    <span
                      className={[
                        style.color,
                        style.bold ? 'font-semibold' : '',
                        style.dim ? 'opacity-60' : '',
                      ]
                        .filter(Boolean)
                        .join(' ')}
                    >
                      {style.prefix ?? ''}{entry.message}
                    </span>
                  </div>
                );
              })}
              <div ref={bottomRef} />
            </>
          )}
        </div>
      )}
    </div>
  );
}
