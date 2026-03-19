import React from 'react';
import type { Message } from '../api/assistant';

interface ChatMessageProps {
  message: Message;
}

function formatTime(isoString: string): string {
  try {
    const d = new Date(isoString);
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
  } catch {
    return '';
  }
}

// Format inline markdown elements: bold, italic, inline code (returns React nodes)
function formatInline(text: string): React.ReactNode[] {
  const parts: React.ReactNode[] = [];
  const pattern = /(\*\*[^*]+\*\*|\*[^*]+\*|`[^`]+`)/g;
  let last = 0;
  let match: RegExpExecArray | null;

  while ((match = pattern.exec(text)) !== null) {
    if (match.index > last) {
      parts.push(text.slice(last, match.index));
    }
    const token = match[0];
    if (token.startsWith('**')) {
      parts.push(<strong key={parts.length}>{token.slice(2, -2)}</strong>);
    } else if (token.startsWith('*')) {
      parts.push(<em key={parts.length}>{token.slice(1, -1)}</em>);
    } else if (token.startsWith('`')) {
      parts.push(
        <code key={parts.length} className="bg-gray-200 text-gray-800 rounded px-1 text-xs font-mono">
          {token.slice(1, -1)}
        </code>
      );
    }
    last = match.index + token.length;
  }
  if (last < text.length) {
    parts.push(text.slice(last));
  }
  return parts;
}

// Parse block-level markdown into React nodes
function parseMarkdown(text: string): React.ReactNode[] {
  const nodes: React.ReactNode[] = [];
  const lines = text.split('\n');
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    // Code block
    if (line.startsWith('```')) {
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith('```')) {
        codeLines.push(lines[i]);
        i++;
      }
      nodes.push(
        <pre key={nodes.length} className="bg-gray-800 text-gray-100 rounded p-3 my-2 overflow-x-auto text-sm">
          <code>{codeLines.join('\n')}</code>
        </pre>
      );
      i++;
      continue;
    }

    // Bullet list
    if (/^[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^[-*]\s+/, ''));
        i++;
      }
      nodes.push(
        <ul key={nodes.length} className="list-disc list-inside my-2 space-y-1 text-sm">
          {items.map((item, idx) => (
            <li key={idx}>{formatInline(item)}</li>
          ))}
        </ul>
      );
      continue;
    }

    // Empty line — spacer
    if (line.trim() === '') {
      nodes.push(<div key={nodes.length} className="my-1" />);
      i++;
      continue;
    }

    // Regular paragraph line
    nodes.push(
      <p key={nodes.length} className="text-sm leading-relaxed">
        {formatInline(line)}
      </p>
    );
    i++;
  }

  return nodes;
}

export default function ChatMessage({ message }: ChatMessageProps) {
  const isUser = message.role === 'user';
  const time = formatTime(message.created_at);

  if (isUser) {
    return (
      <div className="flex justify-end mb-4">
        <div className="max-w-[75%]">
          <div className="bg-blue-600 text-white rounded-2xl rounded-tr-sm px-4 py-3 shadow-sm">
            <p className="text-sm leading-relaxed whitespace-pre-wrap">{message.content}</p>
          </div>
          <p className="text-xs text-gray-400 mt-1 text-right">{time}</p>
        </div>
      </div>
    );
  }

  return (
    <div className="flex justify-start mb-4">
      <div className="max-w-[75%]">
        <div className="flex items-center gap-2 mb-1">
          <div className="w-6 h-6 rounded-full bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center flex-shrink-0">
            <span className="text-white text-xs font-bold">AI</span>
          </div>
          <span className="text-xs text-gray-500 font-medium">Assistant</span>
        </div>
        <div className="bg-gray-100 text-gray-800 rounded-2xl rounded-tl-sm px-4 py-3 shadow-sm">
          <div className="space-y-1">
            {parseMarkdown(message.content)}
          </div>
        </div>
        <p className="text-xs text-gray-400 mt-1">{time}</p>
      </div>
    </div>
  );
}
