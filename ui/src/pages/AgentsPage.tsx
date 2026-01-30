import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import api from '../api/client';

function AgentsPage() {
  const [selectedNamespace, setSelectedNamespace] = useState<string>('');

  const { data: namespacesData } = useQuery({
    queryKey: ['namespaces'],
    queryFn: () => api.getNamespaces(),
  });

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['agents', selectedNamespace],
    queryFn: () =>
      selectedNamespace
        ? api.listAgents(selectedNamespace)
        : api.listAllAgents(),
  });

  return (
    <div>
      <div className="sm:flex sm:items-center sm:justify-between mb-6">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Agents</h2>
          <p className="mt-1 text-sm text-gray-500">
            Browse available AI agents for task execution
          </p>
        </div>
        <div className="mt-4 sm:mt-0">
          <select
            value={selectedNamespace}
            onChange={(e) => setSelectedNamespace(e.target.value)}
            className="block w-full sm:w-48 rounded-md border-gray-300 shadow-sm focus:border-primary-500 focus:ring-primary-500 sm:text-sm"
          >
            <option value="">All Namespaces</option>
            {namespacesData?.namespaces.map((ns) => (
              <option key={ns} value={ns}>
                {ns}
              </option>
            ))}
          </select>
        </div>
      </div>

      {isLoading ? (
        <div className="text-center py-12">
          <div className="inline-block animate-spin rounded-full h-8 w-8 border-4 border-gray-300 border-t-primary-600"></div>
          <p className="mt-2 text-sm text-gray-500">Loading agents...</p>
        </div>
      ) : error ? (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <p className="text-red-800">Error loading agents: {(error as Error).message}</p>
          <button
            onClick={() => refetch()}
            className="mt-2 text-sm text-red-600 hover:text-red-800"
          >
            Retry
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {data?.agents.length === 0 ? (
            <div className="col-span-full text-center py-12 text-gray-500">
              No agents found. Agents are created by platform administrators.
            </div>
          ) : (
            data?.agents.map((agent) => (
              <Link
                key={`${agent.namespace}/${agent.name}`}
                to={`/agents/${agent.namespace}/${agent.name}`}
                className="bg-white shadow-sm rounded-lg overflow-hidden hover:shadow-md transition-shadow"
              >
                <div className="p-6">
                  <div className="flex items-start justify-between">
                    <div>
                      <h3 className="text-lg font-medium text-gray-900">
                        {agent.name}
                      </h3>
                      <p className="text-sm text-gray-500">{agent.namespace}</p>
                    </div>
                    {agent.maxConcurrentTasks && (
                      <span className="inline-flex items-center px-2 py-1 rounded text-xs font-medium bg-blue-100 text-blue-800">
                        Max {agent.maxConcurrentTasks}
                      </span>
                    )}
                  </div>

                  <div className="mt-4 space-y-2">
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">Contexts</span>
                      <span className="text-gray-900">{agent.contextsCount}</span>
                    </div>
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">Credentials</span>
                      <span className="text-gray-900">{agent.credentialsCount}</span>
                    </div>
                    {agent.workspaceDir && (
                      <div className="flex justify-between text-sm">
                        <span className="text-gray-500">Workspace</span>
                        <span className="text-gray-900 font-mono text-xs">
                          {agent.workspaceDir}
                        </span>
                      </div>
                    )}
                  </div>

                  {agent.allowedNamespaces && agent.allowedNamespaces.length > 0 && (
                    <div className="mt-4 pt-4 border-t border-gray-100">
                      <p className="text-xs text-gray-500 mb-1">Allowed namespaces:</p>
                      <div className="flex flex-wrap gap-1">
                        {agent.allowedNamespaces.slice(0, 3).map((ns) => (
                          <span
                            key={ns}
                            className="inline-flex items-center px-2 py-0.5 rounded text-xs bg-gray-100 text-gray-700"
                          >
                            {ns}
                          </span>
                        ))}
                        {agent.allowedNamespaces.length > 3 && (
                          <span className="text-xs text-gray-500">
                            +{agent.allowedNamespaces.length - 3} more
                          </span>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </Link>
            ))
          )}
        </div>
      )}
    </div>
  );
}

export default AgentsPage;
