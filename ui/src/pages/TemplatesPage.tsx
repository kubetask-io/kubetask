import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import api from '../api/client';

function TemplatesPage() {
  const [selectedNamespace, setSelectedNamespace] = useState<string>('');

  const { data: namespacesData } = useQuery({
    queryKey: ['namespaces'],
    queryFn: () => api.getNamespaces(),
  });

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: ['tasktemplates', selectedNamespace],
    queryFn: () =>
      selectedNamespace
        ? api.listTaskTemplates(selectedNamespace)
        : api.listAllTaskTemplates(),
  });

  return (
    <div>
      <div className="sm:flex sm:items-center sm:justify-between mb-6">
        <div>
          <h2 className="text-2xl font-bold text-gray-900">Templates</h2>
          <p className="mt-1 text-sm text-gray-500">
            Reusable task templates for common workflows
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
          <p className="mt-2 text-sm text-gray-500">Loading templates...</p>
        </div>
      ) : error ? (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <p className="text-red-800">Error loading templates: {(error as Error).message}</p>
          <button
            onClick={() => refetch()}
            className="mt-2 text-sm text-red-600 hover:text-red-800"
          >
            Retry
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {data?.templates.length === 0 ? (
            <div className="col-span-full text-center py-12 text-gray-500">
              No templates found.
            </div>
          ) : (
            data?.templates.map((template) => (
              <Link
                key={`${template.namespace}/${template.name}`}
                to={`/templates/${template.namespace}/${template.name}`}
                className="bg-white shadow-sm rounded-lg overflow-hidden hover:shadow-md transition-shadow"
              >
                <div className="p-6">
                  <div className="flex items-start justify-between">
                    <div>
                      <h3 className="text-lg font-medium text-gray-900">
                        {template.name}
                      </h3>
                      <p className="text-sm text-gray-500">{template.namespace}</p>
                    </div>
                    {template.contextsCount > 0 && (
                      <span className="inline-flex items-center px-2 py-1 rounded text-xs font-medium bg-blue-100 text-blue-800">
                        {template.contextsCount} context{template.contextsCount !== 1 ? 's' : ''}
                      </span>
                    )}
                  </div>

                  {template.description && (
                    <p className="mt-3 text-sm text-gray-600 line-clamp-2">
                      {template.description}
                    </p>
                  )}

                  <div className="mt-4 space-y-2">
                    {template.agentRef && (
                      <div className="flex justify-between text-sm">
                        <span className="text-gray-500">Agent</span>
                        <span className="text-gray-900">
                          {template.agentRef.namespace
                            ? `${template.agentRef.namespace}/${template.agentRef.name}`
                            : template.agentRef.name}
                        </span>
                      </div>
                    )}
                    <div className="flex justify-between text-sm">
                      <span className="text-gray-500">Created</span>
                      <span className="text-gray-900">
                        {new Date(template.createdAt).toLocaleDateString()}
                      </span>
                    </div>
                  </div>
                </div>
              </Link>
            ))
          )}
        </div>
      )}
    </div>
  );
}

export default TemplatesPage;
