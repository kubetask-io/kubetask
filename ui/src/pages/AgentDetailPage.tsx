import React from 'react';
import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import api from '../api/client';

function AgentDetailPage() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();

  const { data: agent, isLoading, error } = useQuery({
    queryKey: ['agent', namespace, name],
    queryFn: () => api.getAgent(namespace!, name!),
    enabled: !!namespace && !!name,
  });

  if (isLoading) {
    return (
      <div className="text-center py-12">
        <div className="inline-block animate-spin rounded-full h-8 w-8 border-4 border-gray-300 border-t-primary-600"></div>
      </div>
    );
  }

  if (error || !agent) {
    const errorMessage = (error as Error)?.message || 'Not found';
    const isNotFound = errorMessage.includes('not found');
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-6">
        <h3 className="text-lg font-medium text-red-800 mb-2">
          {isNotFound ? 'Agent Not Found' : 'Error Loading Agent'}
        </h3>
        <p className="text-red-700 mb-4">
          {isNotFound
            ? `The agent "${name}" in namespace "${namespace}" does not exist. It may have been deleted.`
            : errorMessage}
        </p>
        <Link
          to="/agents"
          className="inline-flex items-center px-4 py-2 text-sm font-medium text-red-700 bg-red-100 rounded-md hover:bg-red-200"
        >
          &larr; Back to Agents
        </Link>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-6">
        <Link to="/agents" className="text-sm text-gray-500 hover:text-gray-700">
          &larr; Back to Agents
        </Link>
      </div>

      <div className="bg-white shadow-sm rounded-lg overflow-hidden">
        <div className="px-6 py-4 border-b border-gray-200">
          <h2 className="text-xl font-bold text-gray-900">{agent.name}</h2>
          <p className="text-sm text-gray-500">{agent.namespace}</p>
        </div>

        <div className="px-6 py-4 space-y-6">
          {/* Basic Info */}
          <div>
            <h3 className="text-lg font-medium text-gray-900 mb-4">Configuration</h3>
            <div className="grid grid-cols-2 gap-4">
              {agent.executorImage && (
                <div>
                  <dt className="text-sm font-medium text-gray-500">Executor Image</dt>
                  <dd className="mt-1 text-sm text-gray-900 font-mono bg-gray-50 px-2 py-1 rounded">
                    {agent.executorImage}
                  </dd>
                </div>
              )}
              {agent.agentImage && (
                <div>
                  <dt className="text-sm font-medium text-gray-500">Agent Image</dt>
                  <dd className="mt-1 text-sm text-gray-900 font-mono bg-gray-50 px-2 py-1 rounded">
                    {agent.agentImage}
                  </dd>
                </div>
              )}
              {agent.workspaceDir && (
                <div>
                  <dt className="text-sm font-medium text-gray-500">Workspace Directory</dt>
                  <dd className="mt-1 text-sm text-gray-900 font-mono">
                    {agent.workspaceDir}
                  </dd>
                </div>
              )}
              {agent.maxConcurrentTasks && (
                <div>
                  <dt className="text-sm font-medium text-gray-500">Max Concurrent Tasks</dt>
                  <dd className="mt-1 text-sm text-gray-900">{agent.maxConcurrentTasks}</dd>
                </div>
              )}
            </div>
          </div>

          {/* Quota */}
          {agent.quota && (
            <div>
              <h3 className="text-lg font-medium text-gray-900 mb-4">Quota</h3>
              <div className="bg-gray-50 rounded-md p-4">
                <p className="text-sm text-gray-700">
                  Maximum {agent.quota.maxTaskStarts} task starts per{' '}
                  {agent.quota.windowSeconds} seconds
                </p>
              </div>
            </div>
          )}

          {/* Allowed Namespaces */}
          {agent.allowedNamespaces && agent.allowedNamespaces.length > 0 && (
            <div>
              <h3 className="text-lg font-medium text-gray-900 mb-4">
                Allowed Namespaces
              </h3>
              <div className="flex flex-wrap gap-2">
                {agent.allowedNamespaces.map((ns) => (
                  <span
                    key={ns}
                    className="inline-flex items-center px-3 py-1 rounded-full text-sm bg-gray-100 text-gray-800"
                  >
                    {ns}
                  </span>
                ))}
              </div>
            </div>
          )}

          {/* Credentials */}
          {agent.credentials && agent.credentials.length > 0 && (
            <div>
              <h3 className="text-lg font-medium text-gray-900 mb-4">
                Credentials ({agent.credentials.length})
              </h3>
              <div className="space-y-2">
                {agent.credentials.map((cred, idx) => (
                  <div key={idx} className="bg-gray-50 rounded-md p-3">
                    <div className="flex items-center justify-between">
                      <span className="font-medium text-gray-900">{cred.name}</span>
                      <span className="text-xs text-gray-500">
                        Secret: {cred.secretRef}
                      </span>
                    </div>
                    {(cred.env || cred.mountPath) && (
                      <div className="mt-1 text-sm text-gray-600">
                        {cred.env && <span>ENV: {cred.env}</span>}
                        {cred.mountPath && <span>Mount: {cred.mountPath}</span>}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Contexts */}
          {agent.contexts && agent.contexts.length > 0 && (
            <div>
              <h3 className="text-lg font-medium text-gray-900 mb-4">
                Contexts ({agent.contexts.length})
              </h3>
              <div className="space-y-2">
                {agent.contexts.map((ctx, idx) => (
                  <div key={idx} className="bg-gray-50 rounded-md p-3">
                    <div className="flex items-center justify-between">
                      <span className="font-medium text-gray-900">
                        {ctx.name || `Context ${idx + 1}`}
                      </span>
                      <span className="text-xs px-2 py-1 rounded bg-blue-100 text-blue-800">
                        {ctx.type}
                      </span>
                    </div>
                    {ctx.description && (
                      <p className="mt-1 text-sm text-gray-600">{ctx.description}</p>
                    )}
                    {ctx.mountPath && (
                      <p className="mt-1 text-xs text-gray-500 font-mono">
                        Mount: {ctx.mountPath}
                      </p>
                    )}
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Create Task CTA */}
          <div className="pt-6 border-t border-gray-200">
            <Link
              to={`/tasks/create?agent=${agent.namespace}/${agent.name}`}
              className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-primary-600 hover:bg-primary-700"
            >
              Create Task with this Agent
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}

export default AgentDetailPage;
