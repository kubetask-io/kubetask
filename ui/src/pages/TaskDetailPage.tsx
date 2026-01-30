import React from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import api from '../api/client';
import StatusBadge from '../components/StatusBadge';
import LogViewer from '../components/LogViewer';

function TaskDetailPage() {
  const { namespace, name } = useParams<{ namespace: string; name: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  const deleteMutation = useMutation({
    mutationFn: () => api.deleteTask(namespace!, name!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tasks'] });
      navigate('/tasks');
    },
  });

  const { data: task, isLoading, error } = useQuery({
    queryKey: ['task', namespace, name],
    queryFn: () => api.getTask(namespace!, name!),
    refetchInterval: deleteMutation.isPending ? false : 3000,
    enabled: !!namespace && !!name && !deleteMutation.isSuccess,
  });

  const stopMutation = useMutation({
    mutationFn: () => api.stopTask(namespace!, name!),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['task', namespace, name] });
    },
  });

  if (isLoading) {
    return (
      <div className="text-center py-12">
        <div className="inline-block animate-spin rounded-full h-8 w-8 border-4 border-gray-300 border-t-primary-600"></div>
      </div>
    );
  }

  // If delete is in progress or succeeded, don't show error - navigation will happen
  if (deleteMutation.isPending || deleteMutation.isSuccess) {
    return (
      <div className="text-center py-12">
        <div className="inline-block animate-spin rounded-full h-8 w-8 border-4 border-gray-300 border-t-primary-600"></div>
        <p className="mt-4 text-gray-500">Deleting task...</p>
      </div>
    );
  }

  if (error || !task) {
    const errorMessage = (error as Error)?.message || 'Not found';
    const isNotFound = errorMessage.includes('not found');
    return (
      <div className="bg-red-50 border border-red-200 rounded-lg p-6">
        <h3 className="text-lg font-medium text-red-800 mb-2">
          {isNotFound ? 'Task Not Found' : 'Error Loading Task'}
        </h3>
        <p className="text-red-700 mb-4">
          {isNotFound
            ? `The task "${name}" in namespace "${namespace}" does not exist. It may have been deleted.`
            : errorMessage}
        </p>
        <Link
          to="/tasks"
          className="inline-flex items-center px-4 py-2 text-sm font-medium text-red-700 bg-red-100 rounded-md hover:bg-red-200"
        >
          &larr; Back to Tasks
        </Link>
      </div>
    );
  }

  return (
    <div>
      <div className="mb-6">
        <Link to="/tasks" className="text-sm text-gray-500 hover:text-gray-700">
          &larr; Back to Tasks
        </Link>
      </div>

      <div className="bg-white shadow-sm rounded-lg overflow-hidden">
        <div className="px-6 py-4 border-b border-gray-200">
          <div className="flex items-center justify-between">
            <div>
              <h2 className="text-xl font-bold text-gray-900">{task.name}</h2>
              <p className="text-sm text-gray-500">{task.namespace}</p>
            </div>
            <div className="flex items-center space-x-4">
              <StatusBadge phase={task.phase || 'Pending'} />
              {task.phase === 'Running' && (
                <button
                  onClick={() => stopMutation.mutate()}
                  disabled={stopMutation.isPending}
                  className="px-3 py-1 text-sm font-medium text-yellow-700 bg-yellow-100 rounded-md hover:bg-yellow-200"
                >
                  {stopMutation.isPending ? 'Stopping...' : 'Stop'}
                </button>
              )}
              <button
                onClick={() => {
                  if (confirm('Are you sure you want to delete this task?')) {
                    deleteMutation.mutate();
                  }
                }}
                disabled={deleteMutation.isPending}
                className="px-3 py-1 text-sm font-medium text-red-700 bg-red-100 rounded-md hover:bg-red-200"
              >
                {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </div>

        <div className="px-6 py-4 space-y-4">
          <div className="grid grid-cols-2 gap-4">
            <div>
              <dt className="text-sm font-medium text-gray-500">Agent</dt>
              <dd className="mt-1 text-sm text-gray-900">
                {task.agentRef ? (
                  <Link
                    to={`/agents/${task.agentRef.namespace || task.namespace}/${task.agentRef.name}`}
                    className="text-primary-600 hover:text-primary-800"
                  >
                    {task.agentRef.name}
                  </Link>
                ) : (
                  'default'
                )}
              </dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Duration</dt>
              <dd className="mt-1 text-sm text-gray-900">{task.duration || '-'}</dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Start Time</dt>
              <dd className="mt-1 text-sm text-gray-900">
                {task.startTime ? new Date(task.startTime).toLocaleString() : '-'}
              </dd>
            </div>
            <div>
              <dt className="text-sm font-medium text-gray-500">Completion Time</dt>
              <dd className="mt-1 text-sm text-gray-900">
                {task.completionTime ? new Date(task.completionTime).toLocaleString() : '-'}
              </dd>
            </div>
            {task.podName && (
              <div>
                <dt className="text-sm font-medium text-gray-500">Pod</dt>
                <dd className="mt-1 text-sm text-gray-900">
                  {task.podNamespace}/{task.podName}
                </dd>
              </div>
            )}
          </div>

          {task.description && (
            <div>
              <dt className="text-sm font-medium text-gray-500 mb-2">Description</dt>
              <dd className="bg-gray-50 rounded-md p-4">
                <pre className="text-sm text-gray-900 whitespace-pre-wrap">{task.description}</pre>
              </dd>
            </div>
          )}

          {task.conditions && task.conditions.length > 0 && (
            <div>
              <dt className="text-sm font-medium text-gray-500 mb-2">Conditions</dt>
              <dd className="space-y-2">
                {task.conditions.map((condition, idx) => (
                  <div key={idx} className="bg-gray-50 rounded-md p-3">
                    <div className="flex items-center justify-between">
                      <span className="font-medium text-gray-900">{condition.type}</span>
                      <span
                        className={`text-xs px-2 py-1 rounded ${
                          condition.status === 'True'
                            ? 'bg-green-100 text-green-800'
                            : 'bg-gray-100 text-gray-800'
                        }`}
                      >
                        {condition.status}
                      </span>
                    </div>
                    {condition.reason && (
                      <p className="text-sm text-gray-600 mt-1">Reason: {condition.reason}</p>
                    )}
                    {condition.message && (
                      <p className="text-sm text-gray-500 mt-1">{condition.message}</p>
                    )}
                  </div>
                ))}
              </dd>
            </div>
          )}
        </div>
      </div>

      {/* Log Viewer - show when task has a pod */}
      {(task.phase === 'Running' || task.phase === 'Completed' || task.phase === 'Failed') && (
        <div className="mt-6">
          <LogViewer
            namespace={namespace!}
            taskName={name!}
            podName={task.podName}
            isRunning={task.phase === 'Running'}
          />
        </div>
      )}
    </div>
  );
}

export default TaskDetailPage;
