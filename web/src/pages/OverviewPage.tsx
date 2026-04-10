import { useEffect } from 'react';
import {
  Activity, CheckCircle, XCircle, PlayCircle,
  Zap, DollarSign, TrendingUp,
} from 'lucide-react';
import { useTaskStore } from '../stores/taskStore';

export function OverviewPage() {
  const { tasks, fetchTasks } = useTaskStore();

  useEffect(() => {
    fetchTasks();
  }, []);

  const total = tasks.length;
  const running = tasks.filter((t) =>
    ['initializing', 'planning', 'running', 'merging', 'auto_resolving', 'agent_resolving', 'validating'].includes(t.status)
  ).length;
  const completed = tasks.filter((t) => t.status === 'completed').length;
  const failed = tasks.filter((t) => t.status === 'failed').length;

  const totalTokens = tasks.reduce((sum, t) => sum + t.progress.total_tokens, 0);
  const totalCost = tasks.reduce((sum, t) => sum + t.progress.estimated_cost, 0);
  const successRate = total > 0
    ? Math.round(((completed) / Math.max(completed + failed, 1)) * 100)
    : 0;

  return (
    <div className="max-w-4xl mx-auto p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-800">Dashboard</h1>
        <p className="text-sm text-gray-500 mt-1">Overview of all AgentForge tasks</p>
      </div>

      {/* 统计卡片 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        <OverviewCard
          icon={Activity}
          iconColor="text-indigo-500 bg-indigo-100"
          label="Total Tasks"
          value={String(total)}
        />
        <OverviewCard
          icon={PlayCircle}
          iconColor="text-green-500 bg-green-100"
          label="Running"
          value={String(running)}
        />
        <OverviewCard
          icon={CheckCircle}
          iconColor="text-emerald-500 bg-emerald-100"
          label="Completed"
          value={String(completed)}
        />
        <OverviewCard
          icon={XCircle}
          iconColor="text-red-500 bg-red-100"
          label="Failed"
          value={String(failed)}
        />
      </div>

      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        <OverviewCard
          icon={Zap}
          iconColor="text-amber-500 bg-amber-100"
          label="Total Tokens"
          value={formatTokens(totalTokens)}
        />
        <OverviewCard
          icon={DollarSign}
          iconColor="text-green-500 bg-green-100"
          label="Total Cost"
          value={`$${totalCost.toFixed(2)}`}
        />
        <OverviewCard
          icon={TrendingUp}
          iconColor="text-blue-500 bg-blue-100"
          label="Success Rate"
          value={`${successRate}%`}
        />
      </div>

      {/* 最近任务 */}
      <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
        <div className="px-4 py-3 border-b border-gray-200">
          <h2 className="text-sm font-semibold text-gray-700">Recent Tasks</h2>
        </div>
        {tasks.length === 0 ? (
          <div className="p-8 text-center text-sm text-gray-400">
            No tasks created yet
          </div>
        ) : (
          <div className="divide-y divide-gray-100">
            {tasks.slice(0, 10).map((task) => (
              <div key={task.id} className="px-4 py-3 flex items-center justify-between">
                <div className="min-w-0">
                  <p className="text-sm font-medium text-gray-800 truncate">{task.name}</p>
                  <p className="text-xs text-gray-500 mt-0.5">
                    {task.template} • {new Date(task.created_at).toLocaleDateString()}
                  </p>
                </div>
                <div className="flex items-center gap-3 shrink-0">
                  {task.progress.features_total > 0 && (
                    <span className="text-xs text-gray-500">
                      {task.progress.features_completed}/{task.progress.features_total}
                    </span>
                  )}
                  <span
                    className={`text-xs px-2 py-0.5 rounded-full font-medium ${statusColor(task.status)}`}
                  >
                    {task.status}
                  </span>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function OverviewCard({
  icon: Icon,
  iconColor,
  label,
  value,
}: {
  icon: React.ComponentType<{ className?: string }>;
  iconColor: string;
  label: string;
  value: string;
}) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 p-4">
      <div className="flex items-center gap-3">
        <div className={`p-2 rounded-lg ${iconColor}`}>
          <Icon className="w-5 h-5" />
        </div>
        <div>
          <span className="text-xs text-gray-500">{label}</span>
          <p className="text-xl font-bold text-gray-800">{value}</p>
        </div>
      </div>
    </div>
  );
}

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(n);
}

function statusColor(status: string): string {
  const map: Record<string, string> = {
    pending: 'bg-gray-100 text-gray-600',
    running: 'bg-green-100 text-green-700',
    completed: 'bg-emerald-100 text-emerald-700',
    failed: 'bg-red-100 text-red-700',
    cancelled: 'bg-gray-100 text-gray-500',
  };
  return map[status] || 'bg-gray-100 text-gray-600';
}
