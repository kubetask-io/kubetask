import React, { useState, useMemo } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useQuery, useMutation } from '@tanstack/react-query';
import api, { CreateTaskRequest, Agent } from '../api/client';

// Check if a namespace matches a glob pattern
function matchGlob(pattern: string, namespace: string): boolean {
  // Convert glob pattern to regex
  const regexPattern = pattern
    .replace(/[.+^${}()|[\]\\]/g, '\\$&') // Escape special regex chars except * and ?
    .replace(/\*/g, '.*') // * matches any string
    .replace(/\?/g, '.'); // ? matches single char
  const regex = new RegExp(`^${regexPattern}$`);
  return regex.test(namespace);
}

// Check if an agent is available for a given namespace
function isAgentAvailableForNamespace(agent: Agent, namespace: string): boolean {
  // If no allowedNamespaces, agent is available to all namespaces
  if (!agent.allowedNamespaces || agent.allowedNamespaces.length === 0) {
    return true;
  }
  // Check if any pattern matches the namespace
  return agent.allowedNamespaces.some((pattern) => matchGlob(pattern, namespace));
}

function TaskCreatePage() {
  const navigate = useNavigate();
  const [namespace, setNamespace] = useState('default');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [selectedAgent, setSelectedAgent] = useState('');

  const { data: namespacesData } = useQuery({
    queryKey: ['namespaces'],
    queryFn: () => api.getNamespaces(),
  });

  const { data: agentsData } = useQuery({
    queryKey: ['agents'],
    queryFn: () => api.listAllAgents(),
  });

  // Filter agents based on allowedNamespaces
  const availableAgents = useMemo(() => {
    if (!agentsData?.agents) return [];
    return agentsData.agents.filter((agent) =>
      isAgentAvailableForNamespace(agent, namespace)
    );
  }, [agentsData?.agents, namespace]);

  // Reset selected agent if it's no longer available for the new namespace
  const handleNamespaceChange = (newNamespace: string) => {
    setNamespace(newNamespace);
    if (selectedAgent) {
      const agent = agentsData?.agents.find(
        (a) => `${a.namespace}/${a.name}` === selectedAgent
      );
      if (agent && !isAgentAvailableForNamespace(agent, newNamespace)) {
        setSelectedAgent('');
      }
    }
  };

  const createMutation = useMutation({
    mutationFn: (task: CreateTaskRequest) => api.createTask(namespace, task),
    onSuccess: (task) => {
      navigate(`/tasks/${task.namespace}/${task.name}`);
    },
  });

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    const task: CreateTaskRequest = {
      description,
    };

    if (name) {
      task.name = name;
    }

    if (selectedAgent) {
      const agent = agentsData?.agents.find(
        (a) => `${a.namespace}/${a.name}` === selectedAgent
      );
      if (agent) {
        task.agentRef = {
          name: agent.name,
          namespace: agent.namespace,
        };
      }
    }

    createMutation.mutate(task);
  };

  return (
    <div>
      <div className="mb-6">
        <Link to="/tasks" className="text-sm text-gray-500 hover:text-gray-700">
          &larr; Back to Tasks
        </Link>
      </div>

      <div className="bg-white shadow-sm rounded-lg overflow-hidden">
        <div className="px-6 py-4 border-b border-gray-200">
          <h2 className="text-xl font-bold text-gray-900">Create Task</h2>
          <p className="text-sm text-gray-500">Create a new AI agent task</p>
        </div>

        <form onSubmit={handleSubmit} className="px-6 py-4 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <label
                htmlFor="namespace"
                className="block text-sm font-medium text-gray-700"
              >
                Namespace
              </label>
              <select
                id="namespace"
                value={namespace}
                onChange={(e) => handleNamespaceChange(e.target.value)}
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary-500 focus:ring-primary-500 sm:text-sm"
              >
                {namespacesData?.namespaces.map((ns) => (
                  <option key={ns} value={ns}>
                    {ns}
                  </option>
                ))}
              </select>
            </div>

            <div>
              <label
                htmlFor="name"
                className="block text-sm font-medium text-gray-700"
              >
                Name (optional)
              </label>
              <input
                type="text"
                id="name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Auto-generated if empty"
                className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary-500 focus:ring-primary-500 sm:text-sm"
              />
            </div>
          </div>

          <div>
            <label
              htmlFor="agent"
              className="block text-sm font-medium text-gray-700"
            >
              Agent
            </label>
            <select
              id="agent"
              value={selectedAgent}
              onChange={(e) => setSelectedAgent(e.target.value)}
              required
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary-500 focus:ring-primary-500 sm:text-sm"
            >
              <option value="" disabled>
                {availableAgents.length === 0
                  ? 'No agents available'
                  : 'Select an agent...'}
              </option>
              {availableAgents.map((agent) => (
                <option
                  key={`${agent.namespace}/${agent.name}`}
                  value={`${agent.namespace}/${agent.name}`}
                >
                  {agent.namespace}/{agent.name}
                </option>
              ))}
            </select>
            <p className="mt-1 text-sm text-gray-500">
              {availableAgents.length === 0
                ? 'No agents available for this namespace. Contact your administrator.'
                : `${availableAgents.length} agent${availableAgents.length !== 1 ? 's' : ''} available for this namespace`}
            </p>
          </div>

          <div>
            <label
              htmlFor="description"
              className="block text-sm font-medium text-gray-700"
            >
              Description / Task Prompt
            </label>
            <textarea
              id="description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={10}
              required
              placeholder="Describe what you want the AI agent to do..."
              className="mt-1 block w-full rounded-md border-gray-300 shadow-sm focus:border-primary-500 focus:ring-primary-500 sm:text-sm font-mono"
            />
            <p className="mt-1 text-sm text-gray-500">
              This will be the main instruction for the AI agent
            </p>
          </div>

          {createMutation.isError && (
            <div className="bg-red-50 border border-red-200 rounded-lg p-4">
              <p className="text-red-800">
                Error: {(createMutation.error as Error).message}
              </p>
            </div>
          )}

          <div className="flex justify-end space-x-4">
            <Link
              to="/tasks"
              className="px-4 py-2 text-sm font-medium text-gray-700 bg-white border border-gray-300 rounded-md hover:bg-gray-50"
            >
              Cancel
            </Link>
            <button
              type="submit"
              disabled={createMutation.isPending || !description || !selectedAgent}
              className="px-4 py-2 text-sm font-medium text-white bg-primary-600 rounded-md hover:bg-primary-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {createMutation.isPending ? 'Creating...' : 'Create Task'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export default TaskCreatePage;
