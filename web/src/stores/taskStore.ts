import { create } from 'zustand';
import { api, type Task, type CreateTaskRequest } from '../lib/api';

interface TaskStore {
  tasks: Task[];
  activeTaskId: string | null;
  loading: boolean;
  error: string | null;

  fetchTasks: (status?: string) => Promise<void>;
  setActiveTask: (id: string | null) => void;
  createTask: (data: CreateTaskRequest) => Promise<Task>;
  deleteTask: (id: string) => Promise<void>;
  startTask: (id: string) => Promise<void>;
  stopTask: (id: string) => Promise<void>;
  updateTaskInList: (task: Task) => void;
}

export const useTaskStore = create<TaskStore>((set, get) => ({
  tasks: [],
  activeTaskId: null,
  loading: false,
  error: null,

  fetchTasks: async (status?: string) => {
    set({ loading: true, error: null });
    try {
      const tasks = await api.listTasks(status);
      set({ tasks, loading: false });
    } catch (e) {
      set({ error: (e as Error).message, loading: false });
    }
  },

  setActiveTask: (id) => set({ activeTaskId: id }),

  createTask: async (data) => {
    const task = await api.createTask(data);
    set((s) => ({ tasks: [task, ...s.tasks] }));
    return task;
  },

  deleteTask: async (id) => {
    await api.deleteTask(id);
    set((s) => ({
      tasks: s.tasks.filter((t) => t.id !== id),
      activeTaskId: s.activeTaskId === id ? null : s.activeTaskId,
    }));
  },

  startTask: async (id) => {
    await api.startTask(id);
    await get().fetchTasks();
  },

  stopTask: async (id) => {
    await api.stopTask(id);
    await get().fetchTasks();
  },

  updateTaskInList: (task) => {
    set((s) => ({
      tasks: s.tasks.some((t) => t.id === task.id)
        ? s.tasks.map((t) => (t.id === task.id ? task : t))
        : [task, ...s.tasks],
    }));
  },
}));
