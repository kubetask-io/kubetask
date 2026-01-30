// API Client for KubeOpenCode

const API_BASE = '/api/v1';

export interface AgentReference {
  name: string;
  namespace?: string;
}

export interface Condition {
  type: string;
  status: string;
  reason?: string;
  message?: string;
}

export interface Task {
  name: string;
  namespace: string;
  phase: string;
  description?: string;
  agentRef?: AgentReference;
  podName?: string;
  podNamespace?: string;
  startTime?: string;
  completionTime?: string;
  duration?: string;
  createdAt: string;
  conditions?: Condition[];
}

export interface TaskListResponse {
  tasks: Task[];
  total: number;
}

export interface TaskTemplateReference {
  name: string;
  namespace?: string;
}

export interface CreateTaskRequest {
  name?: string;
  description?: string;
  agentRef?: AgentReference;
  taskTemplateRef?: TaskTemplateReference;
}

export interface ContextItem {
  name?: string;
  description?: string;
  type: string;
  mountPath?: string;
}

export interface CredentialInfo {
  name: string;
  secretRef: string;
  mountPath?: string;
  env?: string;
}

export interface QuotaInfo {
  maxTaskStarts?: number;
  windowSeconds?: number;
}

export interface Agent {
  name: string;
  namespace: string;
  executorImage?: string;
  agentImage?: string;
  workspaceDir?: string;
  contextsCount: number;
  credentialsCount: number;
  maxConcurrentTasks?: number;
  quota?: QuotaInfo;
  allowedNamespaces?: string[];
  credentials?: CredentialInfo[];
  contexts?: ContextItem[];
  createdAt: string;
}

export interface AgentListResponse {
  agents: Agent[];
  total: number;
}

export interface TaskTemplate {
  name: string;
  namespace: string;
  description?: string;
  agentRef?: AgentReference;
  contextsCount: number;
  contexts?: ContextItem[];
  createdAt: string;
}

export interface TaskTemplateListResponse {
  templates: TaskTemplate[];
  total: number;
}

export interface ServerInfo {
  version: string;
}

export interface NamespaceList {
  namespaces: string[];
}

// Log streaming event types
export interface LogEvent {
  type: 'status' | 'log' | 'error' | 'complete';
  phase?: string;
  podPhase?: string;
  content?: string;
  message?: string;
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: 'Unknown error' }));
    throw new Error(error.message || error.error || `HTTP ${response.status}`);
  }

  return response.json();
}

export const api = {
  // Info
  getInfo: () => request<ServerInfo>('/info'),
  getNamespaces: () => request<NamespaceList>('/namespaces'),

  // Tasks
  listTasks: (namespace: string) =>
    request<TaskListResponse>(`/namespaces/${namespace}/tasks`),

  getTask: (namespace: string, name: string) =>
    request<Task>(`/namespaces/${namespace}/tasks/${name}`),

  createTask: (namespace: string, task: CreateTaskRequest) =>
    request<Task>(`/namespaces/${namespace}/tasks`, {
      method: 'POST',
      body: JSON.stringify(task),
    }),

  deleteTask: (namespace: string, name: string) =>
    request<void>(`/namespaces/${namespace}/tasks/${name}`, {
      method: 'DELETE',
    }),

  stopTask: (namespace: string, name: string) =>
    request<Task>(`/namespaces/${namespace}/tasks/${name}/stop`, {
      method: 'POST',
    }),

  // Log streaming - returns an EventSource for SSE
  getTaskLogsUrl: (namespace: string, name: string, container?: string) => {
    const params = new URLSearchParams();
    if (container) params.set('container', container);
    const queryString = params.toString();
    return `${API_BASE}/namespaces/${namespace}/tasks/${name}/logs${queryString ? `?${queryString}` : ''}`;
  },

  // Agents
  listAllAgents: () => request<AgentListResponse>('/agents'),

  listAgents: (namespace: string) =>
    request<AgentListResponse>(`/namespaces/${namespace}/agents`),

  getAgent: (namespace: string, name: string) =>
    request<Agent>(`/namespaces/${namespace}/agents/${name}`),

  // TaskTemplates
  listAllTaskTemplates: () =>
    request<TaskTemplateListResponse>('/tasktemplates'),

  listTaskTemplates: (namespace: string) =>
    request<TaskTemplateListResponse>(`/namespaces/${namespace}/tasktemplates`),

  getTaskTemplate: (namespace: string, name: string) =>
    request<TaskTemplate>(`/namespaces/${namespace}/tasktemplates/${name}`),
};

export default api;
