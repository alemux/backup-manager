import { useState, useRef, useEffect } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { Send, Trash2, Bot, Loader2 } from 'lucide-react';
import { assistantApi, type Message } from '../api/assistant';
import ChatMessage from '../components/ChatMessage';

export default function AssistantPage() {
  const [input, setInput] = useState('');
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const queryClient = useQueryClient();

  const { data: history = [], isLoading: historyLoading } = useQuery({
    queryKey: ['assistant-conversations'],
    queryFn: assistantApi.getConversations,
  });

  // Local messages: history + any in-flight messages
  const [localMessages, setLocalMessages] = useState<Message[]>([]);

  // Sync local messages when history changes
  useEffect(() => {
    setLocalMessages(history);
  }, [history]);

  const chatMutation = useMutation({
    mutationFn: (message: string) => assistantApi.chat(message),
    onMutate: (message) => {
      // Optimistically add user message
      const tempMsg: Message = {
        id: Date.now(),
        role: 'user',
        content: message,
        created_at: new Date().toISOString(),
      };
      setLocalMessages((prev) => [...prev, tempMsg]);
    },
    onSuccess: (response) => {
      // Add assistant response and refresh history
      setLocalMessages((prev) => [...prev, response]);
      queryClient.invalidateQueries({ queryKey: ['assistant-conversations'] });
    },
    onError: (error) => {
      // Remove optimistic user message on error
      setLocalMessages(history);
      console.error('Chat error:', error);
    },
  });

  const clearMutation = useMutation({
    mutationFn: assistantApi.clearConversations,
    onSuccess: () => {
      setLocalMessages([]);
      queryClient.invalidateQueries({ queryKey: ['assistant-conversations'] });
    },
  });

  // Auto-scroll to bottom
  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [localMessages, chatMutation.isPending]);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const message = input.trim();
    if (!message || chatMutation.isPending) return;
    setInput('');
    chatMutation.mutate(message);
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSubmit(e as unknown as React.FormEvent);
    }
  };

  const isEmpty = localMessages.length === 0;

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)]">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-gray-200 bg-white">
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-xl bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center">
            <Bot size={20} className="text-white" />
          </div>
          <div>
            <h1 className="text-lg font-semibold text-gray-900">AI Assistant</h1>
            <p className="text-xs text-gray-500">Context-aware backup system advisor</p>
          </div>
        </div>
        <button
          onClick={() => clearMutation.mutate()}
          disabled={isEmpty || clearMutation.isPending}
          className="inline-flex items-center gap-1.5 px-3 py-1.5 text-sm font-medium text-gray-600 hover:text-red-600 hover:bg-red-50 disabled:opacity-40 disabled:cursor-not-allowed rounded-lg transition-colors"
          title="Clear conversation history"
        >
          <Trash2 size={14} />
          Clear History
        </button>
      </div>

      {/* Messages area */}
      <div className="flex-1 overflow-y-auto px-6 py-6 space-y-0 bg-white">
        {historyLoading ? (
          <div className="flex items-center justify-center h-full">
            <Loader2 size={24} className="animate-spin text-gray-400" />
          </div>
        ) : isEmpty ? (
          <div className="flex flex-col items-center justify-center h-full text-center">
            <div className="w-16 h-16 rounded-2xl bg-gradient-to-br from-blue-100 to-purple-100 flex items-center justify-center mb-4">
              <Bot size={32} className="text-blue-500" />
            </div>
            <h2 className="text-xl font-semibold text-gray-800 mb-2">Ask me anything about your backup system</h2>
            <p className="text-gray-500 text-sm max-w-md">
              I can help you understand backup status, troubleshoot failures, review configurations, and optimize your backup strategies.
            </p>
            <div className="mt-6 grid grid-cols-1 sm:grid-cols-2 gap-2 w-full max-w-lg">
              {[
                'Why did my last backup fail?',
                'Which servers have not been backed up recently?',
                'Show me the health status of my servers',
                'How should I configure retention policies?',
              ].map((suggestion) => (
                <button
                  key={suggestion}
                  onClick={() => {
                    setInput(suggestion);
                    inputRef.current?.focus();
                  }}
                  className="text-left px-4 py-2.5 text-sm text-gray-600 bg-gray-50 hover:bg-blue-50 hover:text-blue-700 border border-gray-200 hover:border-blue-200 rounded-xl transition-colors"
                >
                  {suggestion}
                </button>
              ))}
            </div>
          </div>
        ) : (
          <>
            {localMessages.map((msg) => (
              <ChatMessage key={msg.id} message={msg} />
            ))}
            {chatMutation.isPending && (
              <div className="flex justify-start mb-4">
                <div className="max-w-[75%]">
                  <div className="flex items-center gap-2 mb-1">
                    <div className="w-6 h-6 rounded-full bg-gradient-to-br from-blue-500 to-purple-600 flex items-center justify-center flex-shrink-0">
                      <span className="text-white text-xs font-bold">AI</span>
                    </div>
                    <span className="text-xs text-gray-500 font-medium">Assistant</span>
                  </div>
                  <div className="bg-gray-100 rounded-2xl rounded-tl-sm px-4 py-3 shadow-sm">
                    <div className="flex gap-1 items-center h-5">
                      <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '0ms' }} />
                      <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '150ms' }} />
                      <span className="w-2 h-2 bg-gray-400 rounded-full animate-bounce" style={{ animationDelay: '300ms' }} />
                    </div>
                  </div>
                </div>
              </div>
            )}
            {chatMutation.isError && (
              <div className="flex justify-center mb-4">
                <p className="text-sm text-red-500 bg-red-50 border border-red-200 rounded-lg px-4 py-2">
                  Failed to send message. Please try again.
                </p>
              </div>
            )}
          </>
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* Input area */}
      <div className="px-6 py-4 border-t border-gray-200 bg-white">
        <form onSubmit={handleSubmit} className="flex items-end gap-3">
          <textarea
            ref={inputRef}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={handleKeyDown}
            placeholder="Ask me anything about your backup system..."
            rows={1}
            className="flex-1 resize-none rounded-xl border border-gray-300 px-4 py-3 text-sm text-gray-900 placeholder-gray-400 focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent transition-shadow max-h-32 overflow-y-auto"
            style={{ minHeight: '44px' }}
            disabled={chatMutation.isPending}
          />
          <button
            type="submit"
            disabled={!input.trim() || chatMutation.isPending}
            className="flex-shrink-0 w-11 h-11 rounded-xl bg-blue-600 hover:bg-blue-700 disabled:opacity-40 disabled:cursor-not-allowed text-white flex items-center justify-center transition-colors"
          >
            {chatMutation.isPending ? (
              <Loader2 size={18} className="animate-spin" />
            ) : (
              <Send size={18} />
            )}
          </button>
        </form>
        <p className="text-xs text-gray-400 mt-2 text-center">
          Press Enter to send, Shift+Enter for new line
        </p>
      </div>
    </div>
  );
}
