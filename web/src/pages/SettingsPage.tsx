import { useState } from 'react';
import { Save, Bell, Terminal, DollarSign, RefreshCw } from 'lucide-react';

interface Settings {
  webhookUrl: string;
  notifyOnComplete: boolean;
  notifyOnFailure: boolean;
  notifyOnConflict: boolean;
  costAlertThreshold: number;
  claudeCliPath: string;
  maxRetries: number;
  defaultTimeout: string;
}

const DEFAULT_SETTINGS: Settings = {
  webhookUrl: '',
  notifyOnComplete: true,
  notifyOnFailure: true,
  notifyOnConflict: true,
  costAlertThreshold: 10.0,
  claudeCliPath: 'claude',
  maxRetries: 3,
  defaultTimeout: '30m',
};

export function SettingsPage() {
  const [settings, setSettings] = useState<Settings>(() => {
    try {
      const saved = localStorage.getItem('agentforge-settings');
      return saved ? { ...DEFAULT_SETTINGS, ...JSON.parse(saved) } : DEFAULT_SETTINGS;
    } catch {
      return DEFAULT_SETTINGS;
    }
  });
  const [saved, setSaved] = useState(false);

  const handleSave = () => {
    localStorage.setItem('agentforge-settings', JSON.stringify(settings));
    setSaved(true);
    setTimeout(() => setSaved(false), 2000);
  };

  const handleReset = () => {
    setSettings(DEFAULT_SETTINGS);
    localStorage.removeItem('agentforge-settings');
  };

  const update = <K extends keyof Settings>(key: K, value: Settings[K]) => {
    setSettings((s) => ({ ...s, [key]: value }));
  };

  return (
    <div className="max-w-2xl mx-auto p-6 space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-gray-800">Settings</h1>
          <p className="text-sm text-gray-500 mt-1">Configure AgentForge behavior</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={handleReset}
            className="flex items-center gap-1 px-3 py-2 text-sm text-gray-600 hover:text-gray-800 transition"
          >
            <RefreshCw className="w-4 h-4" /> Reset
          </button>
          <button
            onClick={handleSave}
            className="flex items-center gap-1 px-4 py-2 bg-indigo-600 text-white rounded-lg text-sm hover:bg-indigo-700 transition"
          >
            <Save className="w-4 h-4" /> {saved ? 'Saved!' : 'Save'}
          </button>
        </div>
      </div>

      {/* Notifications */}
      <Section icon={Bell} title="Notifications">
        <FormField label="Webhook URL" description="Receive task events via webhook">
          <input
            value={settings.webhookUrl}
            onChange={(e) => update('webhookUrl', e.target.value)}
            placeholder="https://hooks.example.com/agentforge"
            className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
          />
        </FormField>

        <div className="space-y-2">
          <ToggleField
            label="Notify on task completion"
            checked={settings.notifyOnComplete}
            onChange={(v) => update('notifyOnComplete', v)}
          />
          <ToggleField
            label="Notify on task failure"
            checked={settings.notifyOnFailure}
            onChange={(v) => update('notifyOnFailure', v)}
          />
          <ToggleField
            label="Notify on merge conflicts"
            checked={settings.notifyOnConflict}
            onChange={(v) => update('notifyOnConflict', v)}
          />
        </div>
      </Section>

      {/* Cost Alerts */}
      <Section icon={DollarSign} title="Cost Alerts">
        <FormField label="Cost alert threshold ($)" description="Alert when task cost exceeds this amount">
          <input
            type="number"
            step="0.5"
            min={0}
            value={settings.costAlertThreshold}
            onChange={(e) => update('costAlertThreshold', parseFloat(e.target.value) || 0)}
            className="w-48 px-3 py-2 border border-gray-300 rounded-lg text-sm focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
          />
        </FormField>
      </Section>

      {/* CLI Configuration */}
      <Section icon={Terminal} title="CLI Configuration">
        <FormField label="Claude Code CLI path" description="Path to the claude command">
          <input
            value={settings.claudeCliPath}
            onChange={(e) => update('claudeCliPath', e.target.value)}
            placeholder="claude"
            className="w-full px-3 py-2 border border-gray-300 rounded-lg text-sm font-mono focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
          />
        </FormField>

        <FormField label="Max retries" description="Maximum retry attempts for failed sessions">
          <input
            type="number"
            min={0}
            max={10}
            value={settings.maxRetries}
            onChange={(e) => update('maxRetries', parseInt(e.target.value) || 0)}
            className="w-32 px-3 py-2 border border-gray-300 rounded-lg text-sm focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
          />
        </FormField>

        <FormField label="Default session timeout" description="Default timeout for Claude sessions">
          <input
            value={settings.defaultTimeout}
            onChange={(e) => update('defaultTimeout', e.target.value)}
            placeholder="30m"
            className="w-32 px-3 py-2 border border-gray-300 rounded-lg text-sm focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
          />
        </FormField>
      </Section>
    </div>
  );
}

function Section({
  icon: Icon,
  title,
  children,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <div className="bg-white rounded-xl border border-gray-200 overflow-hidden">
      <div className="flex items-center gap-2 px-4 py-3 border-b border-gray-200 bg-gray-50">
        <Icon className="w-4 h-4 text-gray-500" />
        <h2 className="text-sm font-semibold text-gray-700">{title}</h2>
      </div>
      <div className="p-4 space-y-4">{children}</div>
    </div>
  );
}

function FormField({
  label,
  description,
  children,
}: {
  label: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div>
      <label className="block text-sm font-medium text-gray-700">{label}</label>
      {description && <p className="text-xs text-gray-500 mb-1">{description}</p>}
      <div className="mt-1">{children}</div>
    </div>
  );
}

function ToggleField({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="flex items-center gap-3 cursor-pointer">
      <button
        type="button"
        onClick={() => onChange(!checked)}
        className={`relative w-9 h-5 rounded-full transition-colors ${
          checked ? 'bg-indigo-600' : 'bg-gray-300'
        }`}
      >
        <span
          className={`absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-white shadow transition-transform ${
            checked ? 'translate-x-4' : ''
          }`}
        />
      </button>
      <span className="text-sm text-gray-700">{label}</span>
    </label>
  );
}
