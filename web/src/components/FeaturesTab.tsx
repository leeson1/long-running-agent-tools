import { useEffect, useState } from 'react';
import {
  ChevronRight,
  ChevronDown,
  ArrowRight,
  Layers,
  AlertTriangle,
} from 'lucide-react';
import { useTaskStore } from '../stores/taskStore';
import { useWSStore } from '../stores/wsStore';
import { api, type Feature, type FeatureList } from '../lib/api';

/** Feature 状态图标 */
function featureStatusIcon(f: Feature, runningIds: Set<string>, failedIds: Set<string>): string {
  if (f.passes) return '✅';
  if (failedIds.has(f.id)) return '❌';
  if (runningIds.has(f.id)) return '🔄';
  return '⬜';
}

/** Feature 状态的文本标签 */
function featureStatusLabel(f: Feature, runningIds: Set<string>, failedIds: Set<string>): string {
  if (f.passes) return 'Completed';
  if (failedIds.has(f.id)) return 'Failed';
  if (runningIds.has(f.id)) return 'Running';
  return 'Pending';
}

export function FeaturesTab() {
  const { tasks, activeTaskId } = useTaskStore();
  const { events } = useWSStore();
  const [features, setFeatures] = useState<FeatureList | null>(null);
  const [expandedIds, setExpandedIds] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(false);

  const task = tasks.find((t) => t.id === activeTaskId);
  const refreshKey = events.filter(
    (event) =>
      event.task_id === activeTaskId &&
      (event.type === 'feature_update' || event.type === 'batch_update' || event.type === 'task_status')
  ).length;

  useEffect(() => {
    if (!activeTaskId) return;
    setLoading(true);
    api
      .getFeatures(activeTaskId)
      .then(setFeatures)
      .catch(() => setFeatures(null))
      .finally(() => setLoading(false));
  }, [activeTaskId, refreshKey]);

  if (!task || !activeTaskId) return null;

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        <div className="text-center">
          <Layers className="w-8 h-8 mx-auto mb-2 opacity-30 animate-pulse" />
          <p className="text-sm">Loading features...</p>
        </div>
      </div>
    );
  }

  if (!features || features.features.length === 0) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        <div className="text-center">
          <Layers className="w-8 h-8 mx-auto mb-2 opacity-30" />
          <p className="text-sm">No features yet</p>
          <p className="text-xs mt-1">Features will appear after the planning phase</p>
        </div>
      </div>
    );
  }

  // 模拟 running / failed 状态 (实际由 WS 事件驱动)
  const runningIds = new Set<string>();
  const failedIds = new Set<string>();

  // 按 Batch 分组
  const batches = new Map<number | 'unassigned', Feature[]>();
  for (const f of features.features) {
    const key = f.batch ?? 'unassigned';
    if (!batches.has(key)) batches.set(key, []);
    batches.get(key)!.push(f);
  }

  const sortedEntries = Array.from(batches.entries()).sort((a, b) => {
    if (a[0] === 'unassigned') return 1;
    if (b[0] === 'unassigned') return -1;
    return (a[0] as number) - (b[0] as number);
  });

  const currentBatch = task.progress.current_batch;

  const toggleExpanded = (id: string) => {
    setExpandedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  // 计算总体进度
  const totalFeatures = features.features.length;
  const completedFeatures = features.features.filter((f) => f.passes).length;

  return (
    <div className="space-y-4 pb-4">
      {/* 总进度概览 */}
      <div className="bg-white rounded-lg border border-gray-200 p-3">
        <div className="flex items-center justify-between mb-2">
          <span className="text-sm font-semibold text-gray-700">Overall Progress</span>
          <span className="text-sm font-bold text-indigo-600">
            {completedFeatures}/{totalFeatures}
          </span>
        </div>
        <div className="w-full bg-gray-200 rounded-full h-2.5">
          <div
            className="bg-indigo-600 h-2.5 rounded-full transition-all duration-500"
            style={{ width: `${totalFeatures > 0 ? (completedFeatures / totalFeatures) * 100 : 0}%` }}
          />
        </div>
        <div className="flex items-center gap-4 mt-2 text-xs text-gray-500">
          <span className="flex items-center gap-1">✅ {completedFeatures} completed</span>
          <span className="flex items-center gap-1">🔄 {runningIds.size} running</span>
          <span className="flex items-center gap-1">❌ {failedIds.size} failed</span>
          <span className="flex items-center gap-1">
            ⬜ {totalFeatures - completedFeatures - runningIds.size - failedIds.size} pending
          </span>
        </div>
      </div>

      {/* Batch 分组展示 */}
      {sortedEntries.map(([batch, items]) => {
        const batchNum = batch === 'unassigned' ? -1 : (batch as number);
        const isCurrent = batchNum === currentBatch;
        const batchCompleted = items.every((f) => f.passes);
        const batchFailed = items.some((f) => failedIds.has(f.id));
        const completedInBatch = items.filter((f) => f.passes).length;

        return (
          <div key={String(batch)} className="space-y-1.5">
            {/* Batch 标题 + 进度条 */}
            <div
              className={`flex items-center gap-2 px-2 py-1.5 rounded-lg ${
                isCurrent
                  ? 'bg-indigo-50 border border-indigo-200'
                  : batchCompleted
                    ? 'bg-green-50 border border-green-200'
                    : batchFailed
                      ? 'bg-red-50 border border-red-200'
                      : 'bg-gray-50 border border-gray-200'
              }`}
            >
              <Layers
                className={`w-4 h-4 ${
                  isCurrent ? 'text-indigo-500' : batchCompleted ? 'text-green-500' : 'text-gray-400'
                }`}
              />
              <span
                className={`text-xs font-semibold uppercase ${
                  isCurrent ? 'text-indigo-700' : batchCompleted ? 'text-green-700' : 'text-gray-600'
                }`}
              >
                {batch === 'unassigned' ? 'Unassigned' : `Batch ${batch}`}
              </span>
              {isCurrent && (
                <span className="text-xs bg-indigo-600 text-white px-1.5 py-0.5 rounded-full">
                  Current
                </span>
              )}
              <div className="flex-1 mx-2">
                <div className="w-full bg-gray-200 rounded-full h-1.5">
                  <div
                    className={`h-1.5 rounded-full transition-all duration-500 ${
                      batchCompleted ? 'bg-green-500' : isCurrent ? 'bg-indigo-500' : 'bg-gray-400'
                    }`}
                    style={{
                      width: `${items.length > 0 ? (completedInBatch / items.length) * 100 : 0}%`,
                    }}
                  />
                </div>
              </div>
              <span className="text-xs text-gray-500">
                {completedInBatch}/{items.length}
              </span>
            </div>

            {/* Feature 卡片 */}
            {items.map((f) => {
              const isExpanded = expandedIds.has(f.id);
              const isStuck = failedIds.has(f.id);
              const icon = featureStatusIcon(f, runningIds, failedIds);
              const statusLabel = featureStatusLabel(f, runningIds, failedIds);

              return (
                <div
                  key={f.id}
                  className={`ml-4 rounded-lg border transition ${
                    isStuck
                      ? 'border-red-300 bg-red-50 shadow-sm shadow-red-100'
                      : f.passes
                        ? 'border-green-200 bg-green-50/50'
                        : runningIds.has(f.id)
                          ? 'border-indigo-200 bg-indigo-50/50'
                          : 'border-gray-200 bg-white'
                  }`}
                >
                  {/* 卡片头 */}
                  <button
                    onClick={() => toggleExpanded(f.id)}
                    className="w-full flex items-center gap-2 px-3 py-2.5 text-left"
                  >
                    {isExpanded ? (
                      <ChevronDown className="w-3.5 h-3.5 text-gray-400 shrink-0" />
                    ) : (
                      <ChevronRight className="w-3.5 h-3.5 text-gray-400 shrink-0" />
                    )}
                    <span className="text-base">{icon}</span>
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2">
                        <span className="text-sm font-medium text-gray-800 truncate">{f.id}</span>
                        <span className="text-xs text-gray-500 bg-gray-100 px-1.5 py-0.5 rounded">
                          {f.category}
                        </span>
                        <span
                          className={`text-xs px-1.5 py-0.5 rounded ${
                            f.passes
                              ? 'bg-green-100 text-green-700'
                              : failedIds.has(f.id)
                                ? 'bg-red-100 text-red-700'
                                : runningIds.has(f.id)
                                  ? 'bg-indigo-100 text-indigo-700'
                                  : 'bg-gray-100 text-gray-500'
                          }`}
                        >
                          {statusLabel}
                        </span>
                      </div>
                      <p className="text-xs text-gray-600 mt-0.5 truncate">{f.description}</p>
                    </div>
                    {isStuck && (
                      <AlertTriangle className="w-4 h-4 text-red-500 shrink-0" />
                    )}
                  </button>

                  {/* 展开详情 */}
                  {isExpanded && (
                    <div className="px-3 pb-3 ml-8 space-y-2">
                      {/* Steps */}
                      {f.steps && f.steps.length > 0 && (
                        <div>
                          <span className="text-xs font-semibold text-gray-500 uppercase">Steps</span>
                          <ol className="mt-1 space-y-1 list-decimal list-inside">
                            {f.steps.map((step, i) => (
                              <li key={i} className="text-xs text-gray-600 leading-relaxed">
                                {step}
                              </li>
                            ))}
                          </ol>
                        </div>
                      )}

                      {/* 依赖关系 */}
                      {f.depends_on.length > 0 && (
                        <div>
                          <span className="text-xs font-semibold text-gray-500 uppercase">Dependencies</span>
                          <div className="mt-1 flex items-center gap-1 flex-wrap">
                            {f.depends_on.map((dep) => {
                              const depFeature = features.features.find((ff) => ff.id === dep);
                              const depDone = depFeature?.passes;
                              return (
                                <span
                                  key={dep}
                                  className={`inline-flex items-center gap-1 text-xs px-2 py-0.5 rounded-full ${
                                    depDone
                                      ? 'bg-green-100 text-green-700'
                                      : 'bg-gray-100 text-gray-600'
                                  }`}
                                >
                                  <ArrowRight className="w-3 h-3" />
                                  {dep}
                                  {depDone && <span>✅</span>}
                                </span>
                              );
                            })}
                          </div>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        );
      })}
    </div>
  );
}
