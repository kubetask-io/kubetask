import React, { useEffect, useRef, useState } from 'react';
import api, { LogEvent } from '../api/client';

interface LogViewerProps {
  namespace: string;
  taskName: string;
  podName?: string;
  isRunning: boolean;
}

function LogViewer({ namespace, taskName, podName, isRunning }: LogViewerProps) {
  const [logs, setLogs] = useState<string[]>([]);
  const [status, setStatus] = useState<string>('Connecting...');
  const [error, setError] = useState<string | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const logContainerRef = useRef<HTMLDivElement>(null);
  const eventSourceRef = useRef<EventSource | null>(null);

  useEffect(() => {
    if (!podName) {
      setStatus('Waiting for pod...');
      return;
    }

    // Close existing connection
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const url = api.getTaskLogsUrl(namespace, taskName);
    const eventSource = new EventSource(url);
    eventSourceRef.current = eventSource;

    eventSource.onopen = () => {
      setIsConnected(true);
      setError(null);
      setStatus('Connected');
    };

    eventSource.onmessage = (event) => {
      try {
        const data: LogEvent = JSON.parse(event.data);

        switch (data.type) {
          case 'status':
            setStatus(`Pod: ${data.podPhase || data.phase}`);
            break;
          case 'log':
            if (data.content) {
              setLogs((prev) => [...prev, data.content!]);
            }
            break;
          case 'error':
            setError(data.message || 'Unknown error');
            break;
          case 'complete':
            setStatus(`Completed (${data.phase})`);
            setIsConnected(false);
            eventSource.close();
            break;
        }
      } catch (e) {
        console.error('Failed to parse log event:', e);
      }
    };

    eventSource.onerror = () => {
      setIsConnected(false);
      if (isRunning) {
        setStatus('Connection lost, reconnecting...');
        // EventSource will auto-reconnect
      } else {
        setStatus('Stream ended');
        eventSource.close();
      }
    };

    return () => {
      eventSource.close();
    };
  }, [namespace, taskName, podName, isRunning]);

  // Auto-scroll to bottom when new logs arrive
  useEffect(() => {
    if (logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [logs]);

  return (
    <div className="bg-gray-900 rounded-lg overflow-hidden">
      {/* Header */}
      <div className="px-4 py-2 bg-gray-800 flex items-center justify-between">
        <div className="flex items-center space-x-2">
          <span className="text-sm font-medium text-gray-300">Logs</span>
          <span
            className={`inline-block w-2 h-2 rounded-full ${
              isConnected ? 'bg-green-500' : 'bg-gray-500'
            }`}
          />
        </div>
        <span className="text-xs text-gray-400">{status}</span>
      </div>

      {/* Error message */}
      {error && (
        <div className="px-4 py-2 bg-red-900/50 text-red-300 text-sm">{error}</div>
      )}

      {/* Log content */}
      <div
        ref={logContainerRef}
        className="p-4 h-96 overflow-y-auto font-mono text-sm text-gray-100 whitespace-pre-wrap"
      >
        {logs.length === 0 ? (
          <span className="text-gray-500">
            {podName ? 'Waiting for logs...' : 'Pod not yet created'}
          </span>
        ) : (
          logs.map((line, index) => (
            <div key={index} className="hover:bg-gray-800/50">
              {line}
            </div>
          ))
        )}
      </div>

      {/* Footer */}
      <div className="px-4 py-2 bg-gray-800 flex items-center justify-between">
        <span className="text-xs text-gray-500">{logs.length} lines</span>
        <button
          onClick={() => setLogs([])}
          className="text-xs text-gray-400 hover:text-gray-200"
        >
          Clear
        </button>
      </div>
    </div>
  );
}

export default LogViewer;
