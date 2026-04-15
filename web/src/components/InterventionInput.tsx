import { useState } from 'react';
import { Send, ChevronDown } from 'lucide-react';
import { useConversationStore } from '../stores/conversationStore';
import { api } from '../lib/api';

interface Props {
  taskId: string;
  workerIds: string[];
  disabled?: boolean;
}

export function InterventionInput({ taskId, workerIds, disabled = false }: Props) {
  const [text, setText] = useState('');
  const [targetWorker, setTargetWorker] = useState<string>('global');
  const [sending, setSending] = useState(false);
  const [showWorkerSelect, setShowWorkerSelect] = useState(false);
  const { appendMessage } = useConversationStore();

  const handleSend = async () => {
    const content = text.trim();
    if (!content || disabled) return;

    setSending(true);
    try {
      // 追加到对话流（用户消息样式）
      const msg = {
        id: `user-${Date.now()}`,
        timestamp: new Date().toISOString(),
        role: 'user' as const,
        content,
        workerId: targetWorker === 'global' ? undefined : targetWorker,
      };
      appendMessage(taskId, msg);

      // 发送到后端
      await api.sendIntervention(taskId, {
        content,
        target_worker: targetWorker === 'global' ? undefined : targetWorker,
      });

      setText('');
    } catch (e) {
      // 追加错误消息
      appendMessage(taskId, {
        id: `sys-err-${Date.now()}`,
        timestamp: new Date().toISOString(),
        role: 'system',
        content: `Failed to send: ${(e as Error).message}`,
        isError: true,
      });
    } finally {
      setSending(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  };

  return (
    <div className="border-t border-gray-200 bg-white p-3">
      <div className="flex items-end gap-2">
        {/* Worker 选择器 */}
        <div className="relative">
          <button
            onClick={() => setShowWorkerSelect(!showWorkerSelect)}
            disabled={disabled}
            className="flex items-center gap-1 px-2.5 py-2 text-xs font-medium text-gray-600 bg-gray-100 rounded-lg hover:bg-gray-200 transition whitespace-nowrap"
          >
            {targetWorker === 'global' ? 'Global' : targetWorker}
            <ChevronDown className="w-3 h-3" />
          </button>
          {showWorkerSelect && (
            <div className="absolute bottom-full left-0 mb-1 bg-white border border-gray-200 rounded-lg shadow-lg py-1 min-w-[120px] z-10">
              <button
                onClick={() => {
                  setTargetWorker('global');
                  setShowWorkerSelect(false);
                }}
                className={`w-full text-left px-3 py-1.5 text-xs hover:bg-gray-50 ${
                  targetWorker === 'global' ? 'text-indigo-600 font-medium' : 'text-gray-600'
                }`}
              >
                Global (All)
              </button>
              {workerIds.map((id) => (
                <button
                  key={id}
                  onClick={() => {
                    setTargetWorker(id);
                    setShowWorkerSelect(false);
                  }}
                  className={`w-full text-left px-3 py-1.5 text-xs hover:bg-gray-50 ${
                    targetWorker === id ? 'text-indigo-600 font-medium' : 'text-gray-600'
                  }`}
                >
                  {id}
                </button>
              ))}
            </div>
          )}
        </div>

        {/* 文本输入 */}
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={handleKeyDown}
          disabled={disabled}
          rows={1}
          placeholder={
            disabled
              ? 'Start the pipeline before sending intervention instructions...'
              : 'Send intervention instruction to the running agent...'
          }
          className="flex-1 px-3 py-2 border border-gray-300 rounded-lg text-sm resize-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500 min-h-[38px] max-h-[120px] disabled:bg-gray-100 disabled:text-gray-400"
          style={{
            height: 'auto',
            overflow: text.split('\n').length > 3 ? 'auto' : 'hidden',
          }}
          onInput={(e) => {
            const target = e.target as HTMLTextAreaElement;
            target.style.height = 'auto';
            target.style.height = `${Math.min(target.scrollHeight, 120)}px`;
          }}
        />

        {/* 发送按钮 */}
        <button
          onClick={handleSend}
          disabled={!text.trim() || sending || disabled}
          className="p-2 bg-indigo-600 text-white rounded-lg hover:bg-indigo-700 transition disabled:opacity-50 disabled:cursor-not-allowed shrink-0"
        >
          <Send className="w-4 h-4" />
        </button>
      </div>
      <p className="text-xs text-gray-400 mt-1.5">
        {disabled
          ? 'Interventions are only available while a task is actively running.'
          : 'Press Enter to send, Shift+Enter for new line'}
      </p>
    </div>
  );
}
