import React from 'react';

interface StatusBadgeProps {
  phase: string;
}

function StatusBadge({ phase }: StatusBadgeProps) {
  const getStatusClass = (phase: string) => {
    switch (phase.toLowerCase()) {
      case 'pending':
        return 'bg-gray-100 text-gray-800';
      case 'queued':
        return 'bg-yellow-100 text-yellow-800';
      case 'running':
        return 'bg-blue-100 text-blue-800';
      case 'completed':
        return 'bg-green-100 text-green-800';
      case 'failed':
        return 'bg-red-100 text-red-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  };

  return (
    <span
      className={`inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium ${getStatusClass(
        phase
      )}`}
    >
      {phase}
    </span>
  );
}

export default StatusBadge;
