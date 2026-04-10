import { useEffect, useState } from 'react';
import { GitCommit, ChevronDown, ChevronRight, RotateCcw, Clock } from 'lucide-react';
import { useTaskStore } from '../stores/taskStore';
import { api } from '../lib/api';

interface CommitInfo {
  hash: string;
  shortHash: string;
  message: string;
  author: string;
  timestamp: string;
  sessionId?: string;
  batchNum?: number;
  diff?: string;
}

export function GitTab() {
  const { activeTaskId } = useTaskStore();
  const [commits, setCommits] = useState<CommitInfo[]>([]);
  const [expandedHash, setExpandedHash] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [rollbackConfirm, setRollbackConfirm] = useState<string | null>(null);

  useEffect(() => {
    if (!activeTaskId) return;
    setLoading(true);
    // 从 API 获取 git log（通过 events 推导或直接 API）
    api
      .getEvents(activeTaskId)
      .then((events) => {
        // 从事件中解析 commit 信息
        const parsed: CommitInfo[] = [];
        for (const raw of events) {
          try {
            const e = typeof raw === 'string' ? JSON.parse(raw) : raw;
            if (e.type === 'git_commit' || (e.data && e.data.commit_hash)) {
              parsed.push({
                hash: e.data?.commit_hash || e.data?.hash || '',
                shortHash: (e.data?.commit_hash || e.data?.hash || '').slice(0, 7),
                message: e.data?.message || e.data?.commit_message || 'No message',
                author: e.data?.author || 'Agent',
                timestamp: e.timestamp || new Date().toISOString(),
                sessionId: e.session_id,
                batchNum: e.data?.batch_num,
                diff: e.data?.diff,
              });
            }
          } catch {
            // skip unparseable events
          }
        }
        setCommits(parsed.reverse());
      })
      .catch(() => setCommits([]))
      .finally(() => setLoading(false));
  }, [activeTaskId]);

  if (!activeTaskId) return null;

  const toggleExpand = (hash: string) => {
    setExpandedHash(expandedHash === hash ? null : hash);
  };

  return (
    <div className="space-y-1">
      {loading && (
        <div className="flex items-center justify-center py-12 text-gray-400">
          <GitCommit className="w-8 h-8 animate-pulse opacity-30" />
        </div>
      )}

      {!loading && commits.length === 0 && (
        <div className="flex items-center justify-center py-12 text-gray-400">
          <div className="text-center">
            <GitCommit className="w-8 h-8 mx-auto mb-2 opacity-30" />
            <p className="text-sm">No commits yet</p>
            <p className="text-xs mt-1">Git history will appear as the agent makes commits</p>
          </div>
        </div>
      )}

      {/* 时间线 */}
      {commits.length > 0 && (
        <div className="relative">
          {/* 垂直时间线 */}
          <div className="absolute left-5 top-0 bottom-0 w-0.5 bg-gray-200" />

          {commits.map((commit, index) => (
            <div key={commit.hash || index} className="relative pl-12 pb-4">
              {/* 时间线节点 */}
              <div
                className={`absolute left-3.5 w-3 h-3 rounded-full border-2 ${
                  index === 0
                    ? 'bg-indigo-600 border-indigo-600'
                    : 'bg-white border-gray-300'
                }`}
                style={{ top: '6px' }}
              />

              {/* Commit 卡片 */}
              <div className="bg-white rounded-lg border border-gray-200 overflow-hidden">
                <button
                  onClick={() => toggleExpand(commit.hash)}
                  className="w-full text-left p-3 hover:bg-gray-50 transition"
                >
                  <div className="flex items-start gap-2">
                    <div className="shrink-0 mt-0.5">
                      {expandedHash === commit.hash ? (
                        <ChevronDown className="w-3.5 h-3.5 text-gray-400" />
                      ) : (
                        <ChevronRight className="w-3.5 h-3.5 text-gray-400" />
                      )}
                    </div>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm font-medium text-gray-800 truncate">
                        {commit.message}
                      </p>
                      <div className="flex items-center gap-2 mt-1 flex-wrap">
                        <code className="text-xs bg-gray-100 text-gray-600 px-1.5 py-0.5 rounded font-mono">
                          {commit.shortHash}
                        </code>
                        <span className="text-xs text-gray-400 flex items-center gap-1">
                          <Clock className="w-3 h-3" />
                          {new Date(commit.timestamp).toLocaleString()}
                        </span>
                        {commit.sessionId && (
                          <span className="text-xs text-indigo-500 bg-indigo-50 px-1.5 py-0.5 rounded">
                            {commit.sessionId.slice(0, 12)}
                          </span>
                        )}
                        {commit.batchNum !== undefined && (
                          <span className="text-xs text-purple-500 bg-purple-50 px-1.5 py-0.5 rounded">
                            Batch {commit.batchNum}
                          </span>
                        )}
                      </div>
                    </div>
                  </div>
                </button>

                {/* 展开的 diff 详情 */}
                {expandedHash === commit.hash && (
                  <div className="border-t border-gray-200 p-3 bg-gray-50">
                    <div className="flex items-center justify-between mb-2">
                      <span className="text-xs font-medium text-gray-500">
                        Author: {commit.author}
                      </span>
                      <button
                        onClick={(e) => {
                          e.stopPropagation();
                          setRollbackConfirm(commit.hash);
                        }}
                        className="flex items-center gap-1 px-2 py-1 text-xs text-orange-600 bg-orange-50 rounded hover:bg-orange-100 transition"
                      >
                        <RotateCcw className="w-3 h-3" /> Rollback
                      </button>
                    </div>
                    {commit.diff ? (
                      <pre className="text-xs bg-gray-900 text-gray-200 rounded p-3 overflow-x-auto max-h-80 overflow-y-auto">
                        {commit.diff}
                      </pre>
                    ) : (
                      <p className="text-xs text-gray-400">
                        No diff available. Run the task to generate commit history.
                      </p>
                    )}
                  </div>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* 回滚确认对话框 */}
      {rollbackConfirm && (
        <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
          <div className="bg-white rounded-xl shadow-xl p-6 max-w-sm mx-4">
            <h3 className="text-lg font-semibold text-gray-800 mb-2">Confirm Rollback</h3>
            <p className="text-sm text-gray-600 mb-4">
              Are you sure you want to rollback to commit{' '}
              <code className="bg-gray-100 px-1 py-0.5 rounded text-xs">
                {rollbackConfirm.slice(0, 7)}
              </code>
              ? This action cannot be undone easily.
            </p>
            <div className="flex justify-end gap-2">
              <button
                onClick={() => setRollbackConfirm(null)}
                className="px-4 py-2 text-sm text-gray-600 hover:text-gray-800 transition"
              >
                Cancel
              </button>
              <button
                onClick={() => {
                  // TODO: 实现回滚 API 调用
                  setRollbackConfirm(null);
                }}
                className="px-4 py-2 bg-orange-600 text-white rounded-md text-sm hover:bg-orange-700 transition"
              >
                Rollback
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
