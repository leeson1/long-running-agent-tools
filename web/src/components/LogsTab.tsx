import { useEffect, useRef, useState } from 'react';
import { Search, Pause, Play, ChevronDown } from 'lucide-react';
import { useTaskStore } from '../stores/taskStore';
import { useWSStore } from '../stores/wsStore';
import { api, type Session } from '../lib/api';

type LogLevel = 'all' | 'info' | 'warn' | 'error';

interface LogLine {
  timestamp: string;
  level: LogLevel;
  content: string;
  sessionId?: string;
}

/** 从日志文本解析 level */
function parseLevel(line: string): LogLevel {
  const lower = line.toLowerCase();
  if (lower.includes('[error]') || lower.includes('"error"') || lower.includes('error:')) return 'error';
  if (lower.includes('[warn]') || lower.includes('"warn"') || lower.includes('warning:')) return 'warn';
  return 'info';
}

/** level 对应的颜色 */
const LEVEL_COLORS: Record<LogLevel, string> = {
  all: 'text-gray-300',
  info: 'text-gray-300',
  warn: 'text-yellow-400',
  error: 'text-red-400',
};

const LEVEL_BADGE_COLORS: Record<LogLevel, string> = {
  all: 'bg-gray-600 text-gray-200',
  info: 'bg-gray-600 text-gray-200',
  warn: 'bg-yellow-700 text-yellow-200',
  error: 'bg-red-700 text-red-200',
};

export function LogsTab() {
  const { tasks, activeTaskId } = useTaskStore();
  const { events } = useWSStore();

  const [sessions, setSessions] = useState<Session[]>([]);
  const [selectedSession, setSelectedSession] = useState<string>('live');
  const [logLines, setLogLines] = useState<LogLine[]>([]);
  const [levelFilter, setLevelFilter] = useState<LogLevel>('all');
  const [searchTerm, setSearchTerm] = useState('');
  const [autoScroll, setAutoScroll] = useState(true);
  const [showSessionSelect, setShowSessionSelect] = useState(false);
  const [loading, setLoading] = useState(false);

  const containerRef = useRef<HTMLDivElement>(null);
  const bottomRef = useRef<HTMLDivElement>(null);

  const task = tasks.find((t) => t.id === activeTaskId);

  // 加载 Session 列表
  useEffect(() => {
    if (!activeTaskId) return;
    api.listSessions(activeTaskId).then(setSessions).catch(() => setSessions([]));
  }, [activeTaskId]);

  // 实时日志（来自 WS log 事件）
  useEffect(() => {
    if (selectedSession !== 'live' || !activeTaskId) return;

    const logEvents = events.filter(
      (e) => e.task_id === activeTaskId && (e.type === 'log' || e.type === 'agent_message')
    );

    const lines: LogLine[] = logEvents.map((e) => {
      const content = (e.data?.content as string) || (e.data?.message as string) || JSON.stringify(e.data);
      return {
        timestamp: e.timestamp,
        level: (e.data?.level as LogLevel) || parseLevel(content),
        content,
        sessionId: e.session_id,
      };
    });

    setLogLines(lines);
  }, [events, activeTaskId, selectedSession]);

  // 加载历史 Session 日志
  useEffect(() => {
    if (selectedSession === 'live' || !activeTaskId) return;
    setLoading(true);
    api
      .getLogs(activeTaskId, selectedSession)
      .then((data) => {
        let rawLines: string[];
        if (Array.isArray(data)) {
          rawLines = data;
        } else if (typeof data === 'object' && 'content' in data) {
          rawLines = (data as { content: string }).content.split('\n');
        } else {
          rawLines = [];
        }

        const lines: LogLine[] = rawLines
          .filter((l) => l.trim())
          .map((l, i) => ({
            timestamp: new Date().toISOString(),
            level: parseLevel(l),
            content: l,
            sessionId: selectedSession,
          }));

        setLogLines(lines);
      })
      .catch(() => setLogLines([]))
      .finally(() => setLoading(false));
  }, [selectedSession, activeTaskId]);

  // 自动滚动
  useEffect(() => {
    if (autoScroll && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' });
    }
  }, [logLines.length, autoScroll]);

  // 检测手动滚动
  const handleScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
    if (!atBottom && autoScroll) setAutoScroll(false);
  };

  if (!task || !activeTaskId) return null;

  // 过滤日志
  const filteredLines = logLines.filter((line) => {
    if (levelFilter !== 'all' && line.level !== levelFilter) return false;
    if (searchTerm) {
      return line.content.toLowerCase().includes(searchTerm.toLowerCase());
    }
    return true;
  });

  return (
    <div className="flex flex-col h-full">
      {/* 工具栏 */}
      <div className="flex items-center gap-2 p-2 bg-gray-800 border-b border-gray-700 flex-wrap">
        {/* Session 选择器 */}
        <div className="relative">
          <button
            onClick={() => setShowSessionSelect(!showSessionSelect)}
            className="flex items-center gap-1 px-2.5 py-1.5 bg-gray-700 text-gray-200 rounded text-xs hover:bg-gray-600 transition"
          >
            {selectedSession === 'live' ? '🔴 Live' : `Session: ${selectedSession.slice(0, 12)}...`}
            <ChevronDown className="w-3 h-3" />
          </button>
          {showSessionSelect && (
            <div className="absolute top-full left-0 mt-1 bg-gray-800 border border-gray-600 rounded-lg shadow-xl z-20 max-h-48 overflow-y-auto min-w-[180px]">
              <button
                onClick={() => { setSelectedSession('live'); setShowSessionSelect(false); }}
                className={`w-full text-left px-3 py-1.5 text-xs hover:bg-gray-700 ${
                  selectedSession === 'live' ? 'text-green-400' : 'text-gray-300'
                }`}
              >
                🔴 Live Stream
              </button>
              {sessions.map((s) => (
                <button
                  key={s.id}
                  onClick={() => { setSelectedSession(s.id); setShowSessionSelect(false); }}
                  className={`w-full text-left px-3 py-1.5 text-xs hover:bg-gray-700 ${
                    selectedSession === s.id ? 'text-green-400' : 'text-gray-300'
                  }`}
                >
                  {s.id.slice(0, 16)} <span className="text-gray-500">({s.type})</span>
                </button>
              ))}
            </div>
          )}
        </div>

        {/* 日志级别过滤 */}
        <div className="flex items-center gap-1">
          {(['all', 'info', 'warn', 'error'] as LogLevel[]).map((level) => (
            <button
              key={level}
              onClick={() => setLevelFilter(level)}
              className={`px-2 py-1 rounded text-xs font-medium transition ${
                levelFilter === level
                  ? 'bg-indigo-600 text-white'
                  : 'bg-gray-700 text-gray-300 hover:bg-gray-600'
              }`}
            >
              {level.toUpperCase()}
            </button>
          ))}
        </div>

        {/* 搜索框 */}
        <div className="flex-1 min-w-[150px] relative">
          <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-gray-500" />
          <input
            value={searchTerm}
            onChange={(e) => setSearchTerm(e.target.value)}
            placeholder="Search logs..."
            className="w-full pl-7 pr-2 py-1.5 bg-gray-700 text-gray-200 rounded text-xs border-0 focus:ring-1 focus:ring-indigo-500 placeholder-gray-500"
          />
        </div>

        {/* 自动滚动切换 */}
        <button
          onClick={() => setAutoScroll(!autoScroll)}
          className={`flex items-center gap-1 px-2 py-1.5 rounded text-xs transition ${
            autoScroll
              ? 'bg-green-700 text-green-200'
              : 'bg-gray-700 text-gray-400 hover:bg-gray-600'
          }`}
        >
          {autoScroll ? (
            <><Pause className="w-3 h-3" /> Auto</>
          ) : (
            <><Play className="w-3 h-3" /> Paused</>
          )}
        </button>

        {/* 行数 */}
        <span className="text-xs text-gray-500">
          {filteredLines.length} lines
        </span>
      </div>

      {/* 日志内容 */}
      <div
        ref={containerRef}
        onScroll={handleScroll}
        className="flex-1 overflow-auto bg-gray-900 font-mono text-xs"
      >
        {loading ? (
          <div className="flex items-center justify-center h-full text-gray-500">
            Loading logs...
          </div>
        ) : filteredLines.length === 0 ? (
          <div className="flex items-center justify-center h-full text-gray-600">
            {logLines.length === 0 ? 'No logs yet' : 'No logs match your filter'}
          </div>
        ) : (
          <div className="p-2 space-y-0.5">
            {filteredLines.map((line, i) => (
              <div key={i} className="flex gap-2 leading-relaxed hover:bg-gray-800/50 px-1 rounded">
                <span className="text-gray-600 shrink-0 select-none w-8 text-right">
                  {i + 1}
                </span>
                {line.level !== 'info' && (
                  <span className={`shrink-0 px-1 rounded text-[10px] font-bold uppercase ${LEVEL_BADGE_COLORS[line.level]}`}>
                    {line.level}
                  </span>
                )}
                <span className={`break-all ${LEVEL_COLORS[line.level]}`}>
                  {line.content}
                </span>
              </div>
            ))}
            <div ref={bottomRef} />
          </div>
        )}
      </div>
    </div>
  );
}
