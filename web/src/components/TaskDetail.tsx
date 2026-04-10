import { useState } from 'react';
import {
  Play, Square, MessageSquare, Layers, Clock,
  List, ScrollText, GitBranch, BarChart3,
} from 'lucide-react';
import { useTaskStore } from '../stores/taskStore';
import { StatusBadge } from './StatusBadge';
import { ConversationTab } from './ConversationTab';
import { FeaturesTab } from './FeaturesTab';
import { SessionsTab } from './SessionsTab';
import { EventsTab } from './EventsTab';
import { LogsTab } from './LogsTab';
import { GitTab } from './GitTab';
import { StatsTab } from './StatsTab';

type Tab = 'conversation' | 'features' | 'sessions' | 'events' | 'logs' | 'git' | 'stats';

export function TaskDetail() {
  const { tasks, activeTaskId, startTask, stopTask } = useTaskStore();
  const [tab, setTab] = useState<Tab>('conversation');

  const task = tasks.find((t) => t.id === activeTaskId);

  if (!task) return null;

  const isActive = [
    'initializing',
    'planning',
    'running',
    'merging',
    'auto_resolving',
    'agent_resolving',
    'validating',
  ].includes(task.status);
  const canStart = task.status === 'pending' || task.status === 'failed';

  const tabs: { key: Tab; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
    { key: 'conversation', label: 'Conversation', icon: MessageSquare },
    { key: 'features', label: 'Features', icon: Layers },
    { key: 'logs', label: 'Logs', icon: ScrollText },
    { key: 'git', label: 'Git', icon: GitBranch },
    { key: 'stats', label: 'Stats', icon: BarChart3 },
    { key: 'sessions', label: 'Sessions', icon: Clock },
    { key: 'events', label: 'Events', icon: List },
  ];

  return (
    <div className="h-full flex flex-col">
      {/* Task Header */}
      <div className="p-4 border-b border-gray-200 bg-white">
        <div className="flex items-center justify-between">
          <div className="min-w-0 flex-1">
            <h2 className="text-lg font-semibold text-gray-800 truncate">{task.name}</h2>
            <p className="text-sm text-gray-500 mt-0.5 truncate">
              {task.description || 'No description'}
            </p>
          </div>
          <div className="flex items-center gap-2 shrink-0 ml-4">
            <StatusBadge status={task.status} />
            {canStart && (
              <button
                onClick={() => startTask(task.id)}
                className="flex items-center gap-1 px-3 py-1.5 bg-green-600 text-white rounded-md text-sm hover:bg-green-700 transition"
              >
                <Play className="w-3.5 h-3.5" /> Start
              </button>
            )}
            {isActive && (
              <button
                onClick={() => stopTask(task.id)}
                className="flex items-center gap-1 px-3 py-1.5 bg-red-600 text-white rounded-md text-sm hover:bg-red-700 transition"
              >
                <Square className="w-3.5 h-3.5" /> Stop
              </button>
            )}
          </div>
        </div>
      </div>

      {/* Tabs */}
      <div className="flex border-b border-gray-200 bg-white px-4 overflow-x-auto">
        {tabs.map(({ key, label, icon: Icon }) => (
          <button
            key={key}
            onClick={() => setTab(key)}
            className={`flex items-center gap-1.5 px-3 py-2 text-sm border-b-2 transition whitespace-nowrap ${
              tab === key
                ? 'border-indigo-600 text-indigo-600 font-medium'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
          >
            <Icon className="w-3.5 h-3.5" />
            {label}
          </button>
        ))}
      </div>

      {/* Tab Content */}
      <div className="flex-1 overflow-hidden">
        {tab === 'conversation' && <ConversationTab />}
        {tab === 'features' && (
          <div className="h-full overflow-auto p-4">
            <FeaturesTab />
          </div>
        )}
        {tab === 'logs' && <LogsTab />}
        {tab === 'git' && (
          <div className="h-full overflow-auto p-4">
            <GitTab />
          </div>
        )}
        {tab === 'stats' && (
          <div className="h-full overflow-auto p-4">
            <StatsTab />
          </div>
        )}
        {tab === 'sessions' && (
          <div className="h-full overflow-auto p-4">
            <SessionsTab />
          </div>
        )}
        {tab === 'events' && (
          <div className="h-full overflow-auto p-4">
            <EventsTab />
          </div>
        )}
      </div>
    </div>
  );
}
