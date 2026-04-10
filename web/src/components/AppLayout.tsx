import { useEffect } from 'react';
import { Outlet, useParams, useNavigate, useLocation } from 'react-router-dom';
import { Hammer, Wifi, WifiOff, LayoutDashboard, FileBox, Settings } from 'lucide-react';
import { useTaskStore } from '../stores/taskStore';
import { useWSStore } from '../stores/wsStore';
import { TaskSidebar } from './TaskSidebar';
import { TaskDetail } from './TaskDetail';
import { StatsPanel } from './StatsPanel';

/** 全局页面路径（不显示三栏布局） */
const GLOBAL_PAGES = ['/overview', '/templates', '/settings'];

export function AppLayout() {
  const { taskId } = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const { fetchTasks, activeTaskId, setActiveTask } = useTaskStore();
  const { connected, connect } = useWSStore();

  const isGlobalPage = GLOBAL_PAGES.some((p) => location.pathname.startsWith(p));

  useEffect(() => {
    fetchTasks();
    connect();
  }, []);

  useEffect(() => {
    if (taskId) setActiveTask(taskId);
  }, [taskId]);

  return (
    <div className="h-screen flex flex-col bg-gray-50">
      {/* Top Nav */}
      <header className="h-12 bg-white border-b border-gray-200 flex items-center px-4 shrink-0">
        <button
          onClick={() => navigate('/tasks')}
          className="flex items-center gap-2 hover:opacity-80 transition"
        >
          <Hammer className="w-5 h-5 text-indigo-600" />
          <span className="font-bold text-lg text-gray-800">AgentForge</span>
        </button>

        {/* Nav links */}
        <nav className="ml-6 flex items-center gap-1">
          <NavLink
            to="/tasks"
            label="Tasks"
            active={location.pathname.startsWith('/tasks')}
          />
          <NavLink
            to="/overview"
            label="Dashboard"
            active={location.pathname === '/overview'}
          />
          <NavLink
            to="/templates"
            label="Templates"
            active={location.pathname === '/templates'}
          />
          <NavLink
            to="/settings"
            label="Settings"
            active={location.pathname === '/settings'}
          />
        </nav>

        <div className="ml-auto flex items-center gap-3">
          <span className="text-xs text-gray-500">
            {connected ? (
              <span className="flex items-center gap-1 text-green-600">
                <Wifi className="w-3 h-3" /> Connected
              </span>
            ) : (
              <span className="flex items-center gap-1 text-red-500">
                <WifiOff className="w-3 h-3" /> Disconnected
              </span>
            )}
          </span>
        </div>
      </header>

      {/* Content */}
      {isGlobalPage ? (
        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>
      ) : (
        <div className="flex flex-1 overflow-hidden">
          {/* Left: Task List */}
          <TaskSidebar />

          {/* Center: Task Detail */}
          <main className="flex-1 overflow-auto">
            {activeTaskId ? (
              <TaskDetail />
            ) : (
              <div className="flex items-center justify-center h-full text-gray-400">
                <div className="text-center">
                  <Hammer className="w-12 h-12 mx-auto mb-3 opacity-30" />
                  <p>Select a task or create a new one</p>
                </div>
              </div>
            )}
            <Outlet />
          </main>

          {/* Right: Stats Panel */}
          {activeTaskId && <StatsPanel />}
        </div>
      )}
    </div>
  );
}

function NavLink({ to, label, active }: { to: string; label: string; active: boolean }) {
  const navigate = useNavigate();
  return (
    <button
      onClick={() => navigate(to)}
      className={`px-3 py-1 rounded-md text-sm transition ${
        active
          ? 'bg-indigo-50 text-indigo-600 font-medium'
          : 'text-gray-500 hover:text-gray-700 hover:bg-gray-100'
      }`}
    >
      {label}
    </button>
  );
}
