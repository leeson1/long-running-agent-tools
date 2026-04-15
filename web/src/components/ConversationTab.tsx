import { useEffect, useRef } from 'react';
import { Bot, User, AlertTriangle, Info } from 'lucide-react';
import { useConversationStore, type ConversationMessage } from '../stores/conversationStore';
import { useTaskStore } from '../stores/taskStore';
import { ToolCallPanel } from './ToolCallPanel';
import { InterventionInput } from './InterventionInput';

export function ConversationTab() {
  const { activeTaskId, tasks } = useTaskStore();
  const { getMessages, getWorkerIds, activeWorkerFilter, setWorkerFilter } =
    useConversationStore();

  const messagesEndRef = useRef<HTMLDivElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const shouldAutoScroll = useRef(true);

  const messages = activeTaskId ? getMessages(activeTaskId) : [];
  const workerIds = activeTaskId ? getWorkerIds(activeTaskId) : [];
  const task = tasks.find((t) => t.id === activeTaskId);
  const isActive = !!task && [
    'initializing',
    'planning',
    'running',
    'merging',
    'auto_resolving',
    'agent_resolving',
    'validating',
  ].includes(task.status);

  // 自动滚动到底部
  useEffect(() => {
    if (shouldAutoScroll.current && messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [messages.length]);

  // 检测用户是否手动滚动
  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
    shouldAutoScroll.current = atBottom;
  };

  if (!activeTaskId) return null;

  return (
    <div className="flex flex-col h-full">
      {/* Worker 切换标签 */}
      {workerIds.length > 1 && (
        <div className="flex items-center gap-1 px-3 py-2 bg-gray-50 border-b border-gray-200 overflow-x-auto">
          <button
            onClick={() => setWorkerFilter(null)}
            className={`px-2.5 py-1 rounded-full text-xs font-medium transition whitespace-nowrap ${
              !activeWorkerFilter
                ? 'bg-indigo-600 text-white'
                : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200'
            }`}
          >
            All Workers
          </button>
          {workerIds.map((id) => (
            <button
              key={id}
              onClick={() => setWorkerFilter(id)}
              className={`px-2.5 py-1 rounded-full text-xs font-medium transition whitespace-nowrap ${
                activeWorkerFilter === id
                  ? 'bg-indigo-600 text-white'
                  : 'bg-white text-gray-600 hover:bg-gray-100 border border-gray-200'
              }`}
            >
              {id}
            </button>
          ))}
        </div>
      )}

      {/* 消息列表 */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-y-auto px-4 py-3 space-y-3"
      >
        {messages.length === 0 ? (
          <div className="flex items-center justify-center h-full text-gray-400">
            <div className="text-center">
              <Bot className="w-10 h-10 mx-auto mb-2 opacity-30" />
              <p className="text-sm">Waiting for agent messages...</p>
              <p className="text-xs mt-1">Messages will appear here once the task starts</p>
            </div>
          </div>
        ) : (
          messages.map((msg) => (
            <MessageBubble key={msg.id} message={msg} />
          ))
        )}
        <div ref={messagesEndRef} />
      </div>

      {/* 干预输入框 */}
      <div className="px-3 py-2 border-t border-amber-200 bg-amber-50 text-xs text-amber-800">
        This panel shows agent execution logs. The input below sends an intervention to a running
        task; it is not a general chat box.
      </div>
      <InterventionInput taskId={activeTaskId} workerIds={workerIds} disabled={!isActive} />
    </div>
  );
}

function MessageBubble({ message }: { message: ConversationMessage }) {
  switch (message.role) {
    case 'tool_call':
    case 'tool_result':
      return <ToolCallPanel message={message} />;

    case 'user':
      return (
        <div className="flex justify-end">
          <div className="max-w-[80%] flex items-start gap-2">
            <div className="bg-indigo-600 text-white rounded-xl rounded-tr-sm px-3 py-2 text-sm">
              <p className="whitespace-pre-wrap">{message.content}</p>
              <div className="flex items-center justify-end gap-1 mt-1">
                {message.workerId && (
                  <span className="text-xs text-indigo-200">→ {message.workerId}</span>
                )}
                <span className="text-xs text-indigo-200">
                  {new Date(message.timestamp).toLocaleTimeString()}
                </span>
              </div>
            </div>
            <User className="w-6 h-6 p-1 bg-indigo-100 text-indigo-600 rounded-full shrink-0 mt-1" />
          </div>
        </div>
      );

    case 'system':
      return (
        <div className="flex items-start gap-2 px-2">
          <Info className="w-4 h-4 text-blue-500 shrink-0 mt-0.5" />
          <div className="text-xs text-blue-600 bg-blue-50 rounded px-2 py-1.5 flex-1">
            {message.content}
            <span className="text-blue-400 ml-2">
              {new Date(message.timestamp).toLocaleTimeString()}
            </span>
          </div>
        </div>
      );

    case 'assistant':
    default:
      return (
        <div className="flex items-start gap-2 max-w-[85%]">
          <Bot className="w-6 h-6 p-1 bg-gray-100 text-gray-600 rounded-full shrink-0 mt-1" />
          <div className="bg-white border border-gray-200 rounded-xl rounded-tl-sm px-3 py-2 text-sm shadow-sm">
            {message.workerId && (
              <div className="flex items-center gap-1 mb-1">
                <span className="text-xs font-medium text-indigo-500 bg-indigo-50 px-1.5 py-0.5 rounded">
                  {message.workerId}
                </span>
                {message.featureId && (
                  <span className="text-xs text-gray-400">#{message.featureId}</span>
                )}
              </div>
            )}
            <p className="whitespace-pre-wrap text-gray-700">{message.content}</p>
            {message.isError && (
              <div className="flex items-center gap-1 mt-1.5 text-red-500">
                <AlertTriangle className="w-3 h-3" />
                <span className="text-xs">Error</span>
              </div>
            )}
            <span className="text-xs text-gray-400 mt-1 block">
              {new Date(message.timestamp).toLocaleTimeString()}
            </span>
          </div>
        </div>
      );
  }
}
