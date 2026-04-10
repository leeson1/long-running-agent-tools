import { useState } from 'react';
import {
  Layout, Globe, Smartphone, Server,
  ChevronRight, ChevronDown, FileText,
} from 'lucide-react';

interface TemplateInfo {
  id: string;
  name: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
  category: string;
  features: string[];
  promptPreview: string;
}

const BUILTIN_TEMPLATES: TemplateInfo[] = [
  {
    id: 'default',
    name: 'Default',
    description: 'General-purpose template for any project type. Includes basic scaffolding and common development patterns.',
    icon: Layout,
    category: 'General',
    features: [
      'Auto-detect project structure',
      'Standard feature decomposition',
      'Basic test generation',
      'Git workflow integration',
    ],
    promptPreview: `You are an expert software developer. Analyze the project and decompose it into features.
Each feature should be independently implementable and testable.
Output a feature_list.json with feature definitions, dependencies, and implementation steps.`,
  },
  {
    id: 'web-app',
    name: 'Web App',
    description: 'Optimized for React/Vue/Angular web applications with component-based architecture.',
    icon: Globe,
    category: 'Frontend',
    features: [
      'Component-based feature decomposition',
      'Route-aware planning',
      'State management patterns',
      'API integration scaffolding',
      'Responsive design considerations',
    ],
    promptPreview: `You are an expert web developer specializing in modern frontend frameworks.
Analyze the web application and decompose it into UI components and features.
Consider: routing, state management, API calls, responsive design, accessibility.
Each feature maps to a component or page with its dependencies clearly defined.`,
  },
  {
    id: 'mobile-app',
    name: 'Mobile App',
    description: 'For React Native or Flutter mobile applications with cross-platform considerations.',
    icon: Smartphone,
    category: 'Mobile',
    features: [
      'Screen-based decomposition',
      'Navigation flow awareness',
      'Platform-specific considerations',
      'Offline-first patterns',
      'Native module integration',
    ],
    promptPreview: `You are an expert mobile developer specializing in cross-platform frameworks.
Analyze the mobile application and decompose it into screens and shared components.
Consider: navigation flows, platform differences, offline support, native APIs.
Each feature maps to a screen or shared module with clear dependency chains.`,
  },
  {
    id: 'api',
    name: 'API Server',
    description: 'For REST/GraphQL backend services with database integration and middleware patterns.',
    icon: Server,
    category: 'Backend',
    features: [
      'Endpoint-based decomposition',
      'Database schema planning',
      'Middleware chain awareness',
      'Authentication/authorization patterns',
      'API documentation generation',
    ],
    promptPreview: `You are an expert backend developer specializing in API design and implementation.
Analyze the API server and decompose it into endpoints, middleware, and data models.
Consider: RESTful design, database relationships, auth flows, error handling, validation.
Each feature maps to an API endpoint or shared service with defined data contracts.`,
  },
];

export function TemplatesPage() {
  const [expandedId, setExpandedId] = useState<string | null>(null);

  const toggleExpand = (id: string) => {
    setExpandedId(expandedId === id ? null : id);
  };

  return (
    <div className="max-w-3xl mx-auto p-6 space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-gray-800">Templates</h1>
        <p className="text-sm text-gray-500 mt-1">
          Built-in templates for different project types. Custom templates can be added to ~/.agent-forge/templates/
        </p>
      </div>

      <div className="space-y-3">
        {BUILTIN_TEMPLATES.map((template) => {
          const Icon = template.icon;
          const isExpanded = expandedId === template.id;

          return (
            <div
              key={template.id}
              className="bg-white rounded-xl border border-gray-200 overflow-hidden"
            >
              {/* 卡片头 */}
              <button
                onClick={() => toggleExpand(template.id)}
                className="w-full text-left p-4 hover:bg-gray-50 transition"
              >
                <div className="flex items-start gap-3">
                  <div className="p-2 bg-indigo-50 rounded-lg shrink-0">
                    <Icon className="w-5 h-5 text-indigo-600" />
                  </div>
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="text-base font-semibold text-gray-800">{template.name}</h3>
                      <span className="text-xs bg-gray-100 text-gray-500 px-2 py-0.5 rounded-full">
                        {template.category}
                      </span>
                      <code className="text-xs text-gray-400 font-mono ml-auto">{template.id}</code>
                    </div>
                    <p className="text-sm text-gray-600 mt-1">{template.description}</p>
                    <div className="flex flex-wrap gap-1.5 mt-2">
                      {template.features.map((feat) => (
                        <span
                          key={feat}
                          className="text-xs bg-indigo-50 text-indigo-600 px-2 py-0.5 rounded-full"
                        >
                          {feat}
                        </span>
                      ))}
                    </div>
                  </div>
                  <div className="shrink-0 mt-1">
                    {isExpanded ? (
                      <ChevronDown className="w-4 h-4 text-gray-400" />
                    ) : (
                      <ChevronRight className="w-4 h-4 text-gray-400" />
                    )}
                  </div>
                </div>
              </button>

              {/* 展开详情 */}
              {isExpanded && (
                <div className="border-t border-gray-200 p-4 bg-gray-50">
                  <div className="flex items-center gap-2 mb-2">
                    <FileText className="w-4 h-4 text-gray-500" />
                    <span className="text-sm font-medium text-gray-700">Prompt Preview</span>
                  </div>
                  <pre className="p-3 bg-gray-900 text-gray-200 rounded-lg text-xs font-mono overflow-x-auto whitespace-pre-wrap leading-relaxed">
                    {template.promptPreview}
                  </pre>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}
