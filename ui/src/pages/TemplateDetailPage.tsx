import { useParams, Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import api from '../api/client';

function TemplateDetailPage() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();

  const { data: template, isLoading, error } = useQuery({
    queryKey: ['tasktemplate', namespace, name],
    queryFn: () => api.getTaskTemplate(namespace!, name!),
    enabled: !!namespace && !!name,
  });

  if (isLoading) {
    return (
      <div className="text-center py-12">
        <div className="inline-block animate-spin rounded-full h-8 w-8 border-4 border-gray-300 border-t-primary-600"></div>
      </div>
    );
  }

  if (error || !template) {
    const errorMessage = (error as Error)?.message || 'Not found';
    const isNotFound = errorMessage.includes('not found');
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-6">
        <h3 className="text-lg font-medium text-red-800 mb-2">
          {isNotFound ? 'Template Not Found' : 'Error Loading Template'}
        </h3>
        <p className="text-red-700 mb-4">
          {isNotFound
            ? `The template "${name}" in namespace "${namespace}" does not exist. It may have been deleted.`
            : errorMessage}
        </p>
        <Link
          to="/templates"
          className="inline-flex items-center px-4 py-2 text-sm font-medium text-red-700 bg-red-100 rounded-md hover:bg-red-200"
        >
          &larr; Back to Templates
        </Link>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-6">
        <Link to="/templates" className="text-sm text-gray-500 hover:text-gray-700">
          &larr; Back to Templates
        </Link>
      </div>

      <div className="bg-white shadow-sm rounded-lg overflow-hidden">
        <div className="px-6 py-4 border-b border-gray-200">
          <h2 className="text-xl font-bold text-gray-900">{template.name}</h2>
          <p className="text-sm text-gray-500">{template.namespace}</p>
        </div>

        <div className="px-6 py-4 space-y-6">
          {/* Description */}
          {template.description && (
            <div>
              <h3 className="text-lg font-medium text-gray-900 mb-2">Description</h3>
              <div className="bg-gray-50 rounded-md p-4">
                <pre className="text-sm text-gray-700 whitespace-pre-wrap font-mono">
                  {template.description}
                </pre>
              </div>
            </div>
          )}

          {/* Agent Reference */}
          {template.agentRef && (
            <div>
              <h3 className="text-lg font-medium text-gray-900 mb-2">Agent</h3>
              <Link
                to={`/agents/${template.agentRef.namespace || template.namespace}/${template.agentRef.name}`}
                className="inline-flex items-center px-3 py-2 bg-gray-50 rounded-md text-sm text-primary-600 hover:text-primary-700 hover:bg-gray-100"
              >
                {template.agentRef.namespace
                  ? `${template.agentRef.namespace}/${template.agentRef.name}`
                  : template.agentRef.name}
                <span className="ml-2">&rarr;</span>
              </Link>
            </div>
          )}

          {/* Contexts */}
          {template.contexts && template.contexts.length > 0 && (
            <div>
              <h3 className="text-lg font-medium text-gray-900 mb-4">
                Contexts ({template.contexts.length})
              </h3>
              <div className="space-y-2">
                {template.contexts.map((ctx, idx) => (
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
              to={`/tasks/create?template=${template.namespace}/${template.name}`}
              className="inline-flex items-center px-4 py-2 border border-transparent text-sm font-medium rounded-md shadow-sm text-white bg-primary-600 hover:bg-primary-700"
            >
              Create Task from Template
            </Link>
          </div>
        </div>
      </div>
    </div>
  );
}

export default TemplateDetailPage;
