import { useState, useEffect, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Search, BookOpen, ChevronRight, Loader2, AlertCircle } from 'lucide-react';
import { docsApi, type DocEntry } from '../api/docs';
import React from 'react';

// ---------------------------------------------------------------------------
// Markdown renderer
// ---------------------------------------------------------------------------

function formatInline(text: string): React.ReactNode[] {
  const parts: React.ReactNode[] = [];
  // Matches **bold**, *italic*, `code`, and [label](url)
  const pattern = /(\*\*[^*]+\*\*|\*[^*]+\*|`[^`]+`|\[[^\]]+\]\([^)]+\))/g;
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
        <code
          key={parts.length}
          className="bg-gray-100 text-gray-800 rounded px-1 py-0.5 text-[0.85em] font-mono border border-gray-200"
        >
          {token.slice(1, -1)}
        </code>
      );
    } else if (token.startsWith('[')) {
      const labelMatch = token.match(/\[([^\]]+)\]\(([^)]+)\)/);
      if (labelMatch) {
        parts.push(
          <a
            key={parts.length}
            href={labelMatch[2]}
            target="_blank"
            rel="noopener noreferrer"
            className="text-blue-600 hover:underline"
          >
            {labelMatch[1]}
          </a>
        );
      }
    }
    last = match.index + token.length;
  }
  if (last < text.length) {
    parts.push(text.slice(last));
  }
  return parts;
}

function renderMarkdown(raw: string): React.ReactNode[] {
  // Strip HTML comment headers (<!-- title: ... --> etc.)
  const text = raw.replace(/<!--[^>]*-->/g, '').trimStart();
  const nodes: React.ReactNode[] = [];
  const lines = text.split('\n');
  let i = 0;

  while (i < lines.length) {
    const line = lines[i];

    // Fenced code block
    if (line.startsWith('```')) {
      const lang = line.slice(3).trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !lines[i].startsWith('```')) {
        codeLines.push(lines[i]);
        i++;
      }
      nodes.push(
        <div key={nodes.length} className="my-4 rounded-lg overflow-hidden border border-gray-200">
          {lang && (
            <div className="bg-gray-700 text-gray-300 text-xs px-4 py-1.5 font-mono select-none">
              {lang}
            </div>
          )}
          <pre className="bg-gray-800 text-gray-100 p-4 overflow-x-auto text-sm leading-relaxed">
            <code>{codeLines.join('\n')}</code>
          </pre>
        </div>
      );
      i++;
      continue;
    }

    // Headings
    const h3 = line.match(/^### (.+)/);
    const h2 = line.match(/^## (.+)/);
    const h1 = line.match(/^# (.+)/);
    if (h1) {
      nodes.push(
        <h1 key={nodes.length} className="text-2xl font-bold text-gray-900 mt-6 mb-3 leading-snug">
          {formatInline(h1[1])}
        </h1>
      );
      i++;
      continue;
    }
    if (h2) {
      nodes.push(
        <h2 key={nodes.length} className="text-xl font-semibold text-gray-800 mt-8 mb-3 leading-snug border-b border-gray-200 pb-1">
          {formatInline(h2[1])}
        </h2>
      );
      i++;
      continue;
    }
    if (h3) {
      nodes.push(
        <h3 key={nodes.length} className="text-base font-semibold text-gray-800 mt-5 mb-2">
          {formatInline(h3[1])}
        </h3>
      );
      i++;
      continue;
    }

    // Horizontal rule
    if (/^---+$/.test(line.trim())) {
      nodes.push(<hr key={nodes.length} className="my-6 border-gray-200" />);
      i++;
      continue;
    }

    // Table row detection
    if (line.includes('|') && line.trim().startsWith('|')) {
      const tableLines: string[] = [];
      while (i < lines.length && lines[i].includes('|') && lines[i].trim().startsWith('|')) {
        tableLines.push(lines[i]);
        i++;
      }
      // Filter out separator rows (| --- | --- |)
      const headerRow = tableLines[0];
      const bodyRows = tableLines.slice(2); // skip separator row at index 1

      const parseRow = (row: string) =>
        row
          .split('|')
          .map((c) => c.trim())
          .filter((c) => c.length > 0);

      nodes.push(
        <div key={nodes.length} className="my-4 overflow-x-auto">
          <table className="min-w-full text-sm border border-gray-200 rounded-lg overflow-hidden">
            <thead className="bg-gray-50">
              <tr>
                {parseRow(headerRow).map((cell, idx) => (
                  <th key={idx} className="px-4 py-2 text-left font-semibold text-gray-700 border-b border-gray-200">
                    {formatInline(cell)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {bodyRows.map((row, ridx) => (
                <tr key={ridx} className={ridx % 2 === 0 ? 'bg-white' : 'bg-gray-50'}>
                  {parseRow(row).map((cell, cidx) => (
                    <td key={cidx} className="px-4 py-2 text-gray-700 border-b border-gray-100">
                      {formatInline(cell)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      );
      continue;
    }

    // Ordered list
    if (/^\d+\.\s/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\d+\.\s/.test(lines[i])) {
        items.push(lines[i].replace(/^\d+\.\s+/, ''));
        i++;
      }
      nodes.push(
        <ol key={nodes.length} className="list-decimal list-outside ml-6 my-3 space-y-1.5 text-gray-700">
          {items.map((item, idx) => (
            <li key={idx} className="text-[0.95rem] leading-relaxed pl-1">
              {formatInline(item)}
            </li>
          ))}
        </ol>
      );
      continue;
    }

    // Unordered list
    if (/^[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^[-*]\s+/, ''));
        i++;
      }
      nodes.push(
        <ul key={nodes.length} className="list-disc list-outside ml-6 my-3 space-y-1.5 text-gray-700">
          {items.map((item, idx) => (
            <li key={idx} className="text-[0.95rem] leading-relaxed pl-1">
              {formatInline(item)}
            </li>
          ))}
        </ul>
      );
      continue;
    }

    // Blockquote
    if (line.startsWith('> ')) {
      const quoteLines: string[] = [];
      while (i < lines.length && lines[i].startsWith('> ')) {
        quoteLines.push(lines[i].slice(2));
        i++;
      }
      nodes.push(
        <blockquote key={nodes.length} className="border-l-4 border-blue-400 pl-4 my-4 text-gray-600 italic">
          {quoteLines.map((ql, qi) => (
            <p key={qi} className="text-[0.95rem] leading-relaxed">
              {formatInline(ql)}
            </p>
          ))}
        </blockquote>
      );
      continue;
    }

    // Empty line spacer
    if (line.trim() === '') {
      nodes.push(<div key={nodes.length} className="my-2" />);
      i++;
      continue;
    }

    // Paragraph
    nodes.push(
      <p key={nodes.length} className="text-[0.95rem] leading-relaxed text-gray-700">
        {formatInline(line)}
      </p>
    );
    i++;
  }

  return nodes;
}

// ---------------------------------------------------------------------------
// Page component
// ---------------------------------------------------------------------------

function groupByCategory(entries: DocEntry[]): Record<string, DocEntry[]> {
  return entries.reduce<Record<string, DocEntry[]>>((acc, entry) => {
    if (!acc[entry.category]) acc[entry.category] = [];
    acc[entry.category].push(entry);
    return acc;
  }, {});
}

export default function DocsPage() {
  const [activeSlug, setActiveSlug] = useState<string | null>(null);
  const [searchQuery, setSearchQuery] = useState('');
  const [debouncedQuery, setDebouncedQuery] = useState('');
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  // Debounce search input
  useEffect(() => {
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => setDebouncedQuery(searchQuery), 300);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [searchQuery]);

  // Fetch doc list (or search results)
  const { data: entries = [], isLoading: listLoading } = useQuery({
    queryKey: ['docs-list', debouncedQuery],
    queryFn: () =>
      debouncedQuery ? docsApi.search(debouncedQuery) : docsApi.list(),
  });

  // Auto-select first entry when list loads
  useEffect(() => {
    if (entries.length > 0 && !activeSlug) {
      setActiveSlug(entries[0].slug);
    }
  }, [entries, activeSlug]);

  // If search removes active doc from results, reset selection
  useEffect(() => {
    if (entries.length > 0 && activeSlug) {
      const found = entries.find((e) => e.slug === activeSlug);
      if (!found) setActiveSlug(entries[0].slug);
    }
  }, [entries, activeSlug]);

  // Fetch active document content
  const {
    data: docContent,
    isLoading: contentLoading,
    isError: contentError,
  } = useQuery({
    queryKey: ['doc-content', activeSlug],
    queryFn: () => docsApi.get(activeSlug!),
    enabled: !!activeSlug,
  });

  // Scroll content to top when active doc changes
  useEffect(() => {
    contentRef.current?.scrollTo({ top: 0 });
  }, [activeSlug]);

  const grouped = groupByCategory(entries);
  const categoryOrder = ['Setup', 'Backups', 'Recovery', 'Support'];
  const orderedCategories = [
    ...categoryOrder.filter((c) => grouped[c]),
    ...Object.keys(grouped).filter((c) => !categoryOrder.includes(c)),
  ];

  return (
    <div className="flex h-[calc(100vh-4rem)] bg-white">
      {/* Sidebar */}
      <aside className="w-64 flex-shrink-0 border-r border-gray-200 flex flex-col bg-gray-50">
        {/* Search bar */}
        <div className="px-4 py-3 border-b border-gray-200">
          <div className="relative">
            <Search
              size={14}
              className="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 pointer-events-none"
            />
            <input
              type="text"
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="Search docs..."
              className="w-full pl-8 pr-3 py-2 text-sm rounded-lg border border-gray-200 bg-white focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent placeholder-gray-400"
            />
          </div>
        </div>

        {/* Doc list */}
        <nav className="flex-1 overflow-y-auto py-3">
          {listLoading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={18} className="animate-spin text-gray-400" />
            </div>
          ) : entries.length === 0 ? (
            <p className="text-sm text-gray-500 text-center py-8 px-4">
              No results for &ldquo;{debouncedQuery}&rdquo;
            </p>
          ) : (
            orderedCategories.map((category) => (
              <div key={category} className="mb-4">
                <p className="px-4 py-1 text-xs font-semibold text-gray-400 uppercase tracking-wider">
                  {category}
                </p>
                {grouped[category].map((entry) => {
                  const isActive = entry.slug === activeSlug;
                  return (
                    <button
                      key={entry.slug}
                      onClick={() => setActiveSlug(entry.slug)}
                      className={[
                        'w-full flex items-center justify-between px-4 py-2 text-sm text-left transition-colors',
                        isActive
                          ? 'bg-blue-50 text-blue-700 font-medium border-r-2 border-blue-600'
                          : 'text-gray-600 hover:bg-gray-100 hover:text-gray-900',
                      ].join(' ')}
                    >
                      <span className="flex items-center gap-2">
                        <BookOpen size={13} className="flex-shrink-0 opacity-60" />
                        {entry.title}
                      </span>
                      {isActive && (
                        <ChevronRight size={13} className="flex-shrink-0 text-blue-500" />
                      )}
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </nav>
      </aside>

      {/* Content panel */}
      <main ref={contentRef} className="flex-1 overflow-y-auto">
        {contentLoading ? (
          <div className="flex items-center justify-center h-full">
            <Loader2 size={24} className="animate-spin text-gray-400" />
          </div>
        ) : contentError ? (
          <div className="flex flex-col items-center justify-center h-full gap-3">
            <AlertCircle size={32} className="text-red-400" />
            <p className="text-gray-600">Failed to load documentation.</p>
          </div>
        ) : !docContent ? (
          <div className="flex flex-col items-center justify-center h-full text-center px-6">
            <BookOpen size={40} className="text-gray-300 mb-4" />
            <h2 className="text-lg font-semibold text-gray-700 mb-1">Select a document</h2>
            <p className="text-gray-400 text-sm">
              Choose a topic from the sidebar to start reading.
            </p>
          </div>
        ) : (
          <article className="max-w-3xl mx-auto px-10 py-10">
            <div className="space-y-1">
              {renderMarkdown(docContent.content)}
            </div>
          </article>
        )}
      </main>
    </div>
  );
}
