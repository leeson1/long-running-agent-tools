import { create } from 'zustand';
import { api, createWebSocket, type WSEvent } from '../lib/api';
import { useConversationStore, type MessageRole } from './conversationStore';
import { useTaskStore } from './taskStore';

interface WSStore {
  connected: boolean;
  events: WSEvent[];
  ws: WebSocket | null;

  connect: (taskId?: string) => void;
  disconnect: () => void;
  clearEvents: () => void;
}

const TASK_REFRESH_DELAY_MS = 150;
const pendingTaskRefreshes = new Map<string, number>();

function scheduleTaskRefresh(taskId: string) {
  if (!taskId || pendingTaskRefreshes.has(taskId)) return;

  const timer = window.setTimeout(async () => {
    pendingTaskRefreshes.delete(taskId);
    try {
      const task = await api.getTask(taskId);
      useTaskStore.getState().updateTaskInList(task);
    } catch {
      // 任务可能已被删除，忽略刷新失败
    }
  }, TASK_REFRESH_DELAY_MS);

  pendingTaskRefreshes.set(taskId, timer);
}

/**
 * 将 WebSocket 事件路由到 conversationStore
 */
function routeEventToConversation(event: WSEvent) {
  const convStore = useConversationStore.getState();
  const data = event.data || {};

  switch (event.type) {
    case 'task_status':
    case 'feature_update':
    case 'batch_update':
      scheduleTaskRefresh(event.task_id);
      break;
    default:
      break;
  }

  switch (event.type) {
    case 'agent_message': {
      convStore.appendMessage(event.task_id, {
        id: event.id,
        timestamp: event.timestamp,
        role: 'assistant' as MessageRole,
        sessionId: event.session_id,
        workerId: (data.worker_id as string) || undefined,
        featureId: (data.feature_id as string) || undefined,
        content: (data.content as string) || (data.message as string) || '',
      });
      break;
    }

    case 'tool_call': {
      convStore.appendMessage(event.task_id, {
        id: event.id,
        timestamp: event.timestamp,
        role: 'tool_call' as MessageRole,
        sessionId: event.session_id,
        workerId: (data.worker_id as string) || undefined,
        featureId: (data.feature_id as string) || undefined,
        content: (data.tool_name as string) || 'Tool Call',
        toolName: (data.tool_name as string) || undefined,
        toolInput: (data.tool_input as string) || (data.input as string) || undefined,
        toolOutput: (data.tool_output as string) || (data.output as string) || undefined,
        isError: (data.is_error as boolean) || false,
      });
      break;
    }

    case 'session_start': {
      convStore.appendMessage(event.task_id, {
        id: event.id,
        timestamp: event.timestamp,
        role: 'system' as MessageRole,
        sessionId: event.session_id,
        workerId: (data.worker_id as string) || undefined,
        content: `Session started: ${event.session_id || 'unknown'}${data.type ? ` (${data.type})` : ''}`,
      });
      break;
    }

    case 'session_end': {
      convStore.appendMessage(event.task_id, {
        id: event.id,
        timestamp: event.timestamp,
        role: 'system' as MessageRole,
        sessionId: event.session_id,
        workerId: (data.worker_id as string) || undefined,
        content: `Session ended: ${event.session_id || 'unknown'}${data.status ? ` (${data.status})` : ''}`,
      });
      break;
    }

    case 'merge_conflict': {
      convStore.appendMessage(event.task_id, {
        id: event.id,
        timestamp: event.timestamp,
        role: 'system' as MessageRole,
        content: `⚠️ Merge conflict detected${data.feature_id ? ` in ${data.feature_id}` : ''}${data.files ? `: ${(data.files as string[]).join(', ')}` : ''}`,
        isError: true,
      });
      break;
    }

    case 'alert': {
      convStore.appendMessage(event.task_id, {
        id: event.id,
        timestamp: event.timestamp,
        role: 'system' as MessageRole,
        content: `🔔 ${(data.message as string) || (data.content as string) || 'Alert'}`,
        isError: (data.level as string) === 'error',
      });
      break;
    }

    case 'intervention': {
      convStore.appendMessage(event.task_id, {
        id: event.id,
        timestamp: event.timestamp,
        role: 'system' as MessageRole,
        content: `✅ Intervention delivered${(data.target_worker as string) ? ` to ${data.target_worker}` : ''}`,
      });
      break;
    }

    // feature_update, batch_update, task_status, log 不写入对话流
    default:
      break;
  }
}

export const useWSStore = create<WSStore>((set, get) => ({
  connected: false,
  events: [],
  ws: null,

  connect: (taskId?: string) => {
    const existing = get().ws;
    if (existing) {
      existing.close();
    }

    const ws = createWebSocket(taskId);

    ws.onopen = () => set({ connected: true });

    ws.onclose = () => {
      set({ connected: false, ws: null });
      // 自动重连
      setTimeout(() => {
        if (!get().ws) {
          get().connect(taskId);
        }
      }, 3000);
    };

    ws.onmessage = (e) => {
      const raw = typeof e.data === 'string' ? e.data : '';
      const frames = raw
        .split('\n')
        .map((line) => line.trim())
        .filter(Boolean);

      for (const frame of frames) {
        try {
          const event: WSEvent = JSON.parse(frame);
          set((s) => ({
            events: [...s.events.slice(-500), event], // 保留最近 500 条
          }));
          routeEventToConversation(event);
        } catch {
          // 忽略解析错误
        }
      }
    };

    ws.onerror = () => {
      // onclose 会处理重连
    };

    set({ ws });
  },

  disconnect: () => {
    const ws = get().ws;
    if (ws) {
      ws.close();
      set({ ws: null, connected: false });
    }
  },

  clearEvents: () => set({ events: [] }),
}));
