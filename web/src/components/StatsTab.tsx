import { useEffect, useState } from 'react';
import {
  LineChart, Line, BarChart, Bar, XAxis, YAxis, CartesianGrid,
  Tooltip, ResponsiveContainer, Legend,
} from 'recharts';
import { BarChart3 } from 'lucide-react';
import { useTaskStore } from '../stores/taskStore';
import { api, type Session } from '../lib/api';

interface TokenDataPoint {
  name: string;
  input: number;
  output: number;
  total: number;
}

interface CostDataPoint {
  name: string;
  cost: number;
  cumulative: number;
}

interface DurationDataPoint {
  name: string;
  duration: number; // seconds
  type: string;
}

export function StatsTab() {
  const { tasks, activeTaskId } = useTaskStore();
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(false);

  const task = tasks.find((t) => t.id === activeTaskId);

  useEffect(() => {
    if (!activeTaskId) return;
    setLoading(true);
    api
      .listSessions(activeTaskId)
      .then(setSessions)
      .catch(() => setSessions([]))
      .finally(() => setLoading(false));
  }, [activeTaskId]);

  if (!task || !activeTaskId) return null;

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full text-gray-400">
        <BarChart3 className="w-8 h-8 animate-pulse opacity-30" />
      </div>
    );
  }

  // 准备图表数据
  const tokenData: TokenDataPoint[] = sessions.map((s, i) => ({
    name: s.type === 'worker' ? `W${i + 1}` : `S${i + 1}`,
    input: s.result.tokens_input,
    output: s.result.tokens_output,
    total: s.result.tokens_input + s.result.tokens_output,
  }));

  // 成本估算：input $3/M, output $15/M (Claude Sonnet 类似)
  let cumCost = 0;
  const costData: CostDataPoint[] = sessions.map((s, i) => {
    const cost = (s.result.tokens_input * 3 + s.result.tokens_output * 15) / 1_000_000;
    cumCost += cost;
    return {
      name: s.type === 'worker' ? `W${i + 1}` : `S${i + 1}`,
      cost: parseFloat(cost.toFixed(4)),
      cumulative: parseFloat(cumCost.toFixed(4)),
    };
  });

  // Session 时长
  const durationData: DurationDataPoint[] = sessions
    .filter((s) => s.started_at && s.ended_at)
    .map((s, i) => {
      const start = new Date(s.started_at).getTime();
      const end = new Date(s.ended_at!).getTime();
      const duration = Math.max(0, (end - start) / 1000);
      return {
        name: s.type === 'worker' ? `W${i + 1}` : `S${i + 1}`,
        duration: parseFloat(duration.toFixed(1)),
        type: s.type,
      };
    });

  // Batch 耗时
  const batchTimes = new Map<number, { start: number; end: number }>();
  for (const s of sessions) {
    if (s.batch_num !== undefined && s.batch_num !== null && s.started_at) {
      const bn = s.batch_num;
      const start = new Date(s.started_at).getTime();
      const end = s.ended_at ? new Date(s.ended_at).getTime() : Date.now();
      const existing = batchTimes.get(bn);
      if (!existing) {
        batchTimes.set(bn, { start, end });
      } else {
        batchTimes.set(bn, {
          start: Math.min(existing.start, start),
          end: Math.max(existing.end, end),
        });
      }
    }
  }
  const batchData = Array.from(batchTimes.entries())
    .sort((a, b) => a[0] - b[0])
    .map(([batch, { start, end }]) => ({
      name: `B${batch}`,
      duration: parseFloat(((end - start) / 1000).toFixed(1)),
    }));

  const hasData = sessions.length > 0;

  return (
    <div className="space-y-6 pb-4">
      {!hasData ? (
        <div className="flex items-center justify-center py-12 text-gray-400">
          <div className="text-center">
            <BarChart3 className="w-8 h-8 mx-auto mb-2 opacity-30" />
            <p className="text-sm">No statistics yet</p>
            <p className="text-xs mt-1">Charts will appear after sessions complete</p>
          </div>
        </div>
      ) : (
        <>
          {/* 摘要卡片 */}
          <div className="grid grid-cols-4 gap-3">
            <SummaryCard
              label="Total Sessions"
              value={String(sessions.length)}
              color="text-indigo-600"
            />
            <SummaryCard
              label="Total Tokens"
              value={formatNumber(
                sessions.reduce(
                  (sum, s) => sum + s.result.tokens_input + s.result.tokens_output,
                  0
                )
              )}
              color="text-blue-600"
            />
            <SummaryCard
              label="Est. Cost"
              value={`$${cumCost.toFixed(2)}`}
              color="text-green-600"
            />
            <SummaryCard
              label="Avg Duration"
              value={
                durationData.length > 0
                  ? `${(
                      durationData.reduce((s, d) => s + d.duration, 0) /
                      durationData.length
                    ).toFixed(0)}s`
                  : '-'
              }
              color="text-orange-600"
            />
          </div>

          {/* Token 用量折线图 */}
          {tokenData.length > 0 && (
            <ChartCard title="Token Usage by Session">
              <ResponsiveContainer width="100%" height={220}>
                <LineChart data={tokenData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                  <XAxis dataKey="name" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} tickFormatter={formatNumber} />
                  <Tooltip formatter={(v: number) => formatNumber(v)} />
                  <Legend iconSize={8} wrapperStyle={{ fontSize: 11 }} />
                  <Line
                    type="monotone"
                    dataKey="input"
                    stroke="#6366f1"
                    strokeWidth={2}
                    dot={{ r: 3 }}
                    name="Input"
                  />
                  <Line
                    type="monotone"
                    dataKey="output"
                    stroke="#f59e0b"
                    strokeWidth={2}
                    dot={{ r: 3 }}
                    name="Output"
                  />
                </LineChart>
              </ResponsiveContainer>
            </ChartCard>
          )}

          {/* 成本累计柱状图 */}
          {costData.length > 0 && (
            <ChartCard title="Cost per Session (Cumulative)">
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={costData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                  <XAxis dataKey="name" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} tickFormatter={(v) => `$${v}`} />
                  <Tooltip formatter={(v: number) => `$${v.toFixed(4)}`} />
                  <Legend iconSize={8} wrapperStyle={{ fontSize: 11 }} />
                  <Bar dataKey="cost" fill="#10b981" name="Session Cost" radius={[3, 3, 0, 0]} />
                  <Line
                    type="monotone"
                    dataKey="cumulative"
                    stroke="#ef4444"
                    strokeWidth={2}
                    name="Cumulative"
                  />
                </BarChart>
              </ResponsiveContainer>
            </ChartCard>
          )}

          {/* Session 执行时长 */}
          {durationData.length > 0 && (
            <ChartCard title="Session Duration (seconds)">
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={durationData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                  <XAxis dataKey="name" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} unit="s" />
                  <Tooltip formatter={(v: number) => `${v}s`} />
                  <Bar dataKey="duration" fill="#8b5cf6" name="Duration" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </ChartCard>
          )}

          {/* Batch 执行耗时 */}
          {batchData.length > 0 && (
            <ChartCard title="Batch Execution Time (seconds)">
              <ResponsiveContainer width="100%" height={180}>
                <BarChart data={batchData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#e5e7eb" />
                  <XAxis dataKey="name" tick={{ fontSize: 11 }} />
                  <YAxis tick={{ fontSize: 11 }} unit="s" />
                  <Tooltip formatter={(v: number) => `${v}s`} />
                  <Bar dataKey="duration" fill="#0ea5e9" name="Duration" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </ChartCard>
          )}
        </>
      )}
    </div>
  );
}

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="bg-white rounded-lg border border-gray-200 p-4">
      <h3 className="text-sm font-semibold text-gray-700 mb-3">{title}</h3>
      {children}
    </div>
  );
}

function SummaryCard({ label, value, color }: { label: string; value: string; color: string }) {
  return (
    <div className="bg-white rounded-lg border border-gray-200 p-3">
      <span className="text-xs text-gray-500">{label}</span>
      <p className={`text-lg font-bold mt-0.5 ${color}`}>{value}</p>
    </div>
  );
}

function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return String(Math.round(n));
}
