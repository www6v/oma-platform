// TraceViewer.tsx - Dynamic Workflows plugin component
import React, { useEffect, useState, useRef } from 'react';
import { useParams, useNavigate } from 'react-router';

interface Trace {
  id: string;
  step_name: string;
  status: string;
  input_data: any;
  output_data: any;
  error: string | null;
  started_at: string;
  completed_at: string | null;
  duration_ms: number | null;
}

interface Execution {
  id: string;
  workflow_id: string;
  status: string;
  started_at: string;
  completed_at: string | null;
  traces: Trace[];
}

export default function TraceViewer() {
  const { id: workflowId, executionId } = useParams<{ id: string; executionId: string }>();
  const navigate = useNavigate();
  const [execution, setExecution] = useState<Execution | null>(null);
  const [traces, setTraces] = useState<Trace[]>([]);
  const [wsConnected, setWsConnected] = useState(false);
  const wsRef = useRef<WebSocket | null>(null);

  useEffect(() => {
    // Load execution data
    fetch(`/api/workflows/executions/${executionId}`)
      .then(res => res.json())
      .then(data => {
        setExecution(data);
      })
      .catch(err => console.error('Failed to load execution:', err));

    // Load traces
    fetch(`/api/workflows/executions/${executionId}/traces`)
      .then(res => res.json())
      .then(data => {
        setTraces(data);
      })
      .catch(err => console.error('Failed to load traces:', err));

    // Connect to WebSocket for real-time updates
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/workflows/executions/${executionId}/ws`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setWsConnected(true);
      console.log('WebSocket connected');
    };

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);

        if (message.type === 'trace_update') {
          // Update or add trace
          setTraces(prev => {
            const existing = prev.findIndex(t => t.id === message.trace.id);
            if (existing >= 0) {
              const updated = [...prev];
              updated[existing] = message.trace;
              return updated;
            } else {
              return [...prev, message.trace];
            }
          });
        } else if (message.type === 'execution_update') {
          setExecution(prev => prev ? { ...prev, ...message.execution } : null);
        }
      } catch (err) {
        console.error('Failed to parse WebSocket message:', err);
      }
    };

    ws.onerror = (error) => {
      console.error('WebSocket error:', error);
    };

    ws.onclose = () => {
      setWsConnected(false);
      console.log('WebSocket disconnected');
    };

    // Cleanup on unmount
    return () => {
      if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) {
        wsRef.current.close();
      }
    };
  }, [executionId]);

  const getStatusColor = (status: string) => {
    switch (status) {
      case 'completed':
        return '#10b981';
      case 'failed':
        return '#ef4444';
      case 'running':
        return '#3b82f6';
      case 'pending':
        return '#6b7280';
      default:
        return '#6b7280';
    }
  };

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'completed':
        return '✓';
      case 'failed':
        return '✗';
      case 'running':
        return '⋯';
      case 'pending':
        return '○';
      default:
        return '?';
    }
  };

  const handleCancel = async () => {
    if (!confirm('Cancel this execution?')) return;

    await fetch(`/api/workflows/executions/${executionId}/cancel`, {
      method: 'POST',
    });

    navigate(`/workflows/${workflowId}`);
  };

  if (!execution) {
    return <div>Loading execution...</div>;
  }

  return (
    <div className="trace-viewer-page">
      <div className="viewer-header">
        <button onClick={() => navigate(`/workflows/${workflowId}`)}>← Back</button>
        <h2>Execution Trace</h2>
        <div className="header-status">
          <span className={`status-badge ${execution.status}`}>
            {execution.status}
          </span>
          {wsConnected && (
            <span className="ws-indicator connected" title="Live updates">
              ● Live
            </span>
          )}
        </div>
      </div>

      <div className="execution-info">
        <div className="info-row">
          <strong>Execution ID:</strong>
          <span>{execution.id}</span>
        </div>
        <div className="info-row">
          <strong>Started:</strong>
          <span>{new Date(execution.started_at).toLocaleString()}</span>
        </div>
        {execution.completed_at && (
          <div className="info-row">
            <strong>Completed:</strong>
            <span>{new Date(execution.completed_at).toLocaleString()}</span>
          </div>
        )}
        {execution.status === 'running' && (
          <button onClick={handleCancel} className="danger">
            Cancel Execution
          </button>
        )}
      </div>

      <div className="traces-timeline">
        <h3>Steps ({traces.length})</h3>
        {traces.length === 0 ? (
          <div className="empty-traces">No traces yet...</div>
        ) : (
          <div className="traces-list">
            {traces.map((trace, index) => (
              <div key={trace.id} className={`trace-item ${trace.status}`}>
                <div className="trace-header">
                  <div className="trace-indicator">
                    <span className="trace-number">{index + 1}</span>
                    <span
                      className="status-icon"
                      style={{ color: getStatusColor(trace.status) }}
                    >
                      {getStatusIcon(trace.status)}
                    </span>
                  </div>
                  <div className="trace-info">
                    <h4>{trace.step_name}</h4>
                    <span className={`status-text ${trace.status}`}>
                      {trace.status}
                    </span>
                  </div>
                  {trace.duration_ms !== null && (
                    <div className="trace-duration">
                      {(trace.duration_ms / 1000).toFixed(2)}s
                    </div>
                  )}
                </div>

                {trace.input_data && (
                  <div className="trace-section input">
                    <strong>Input:</strong>
                    <pre>{JSON.stringify(trace.input_data, null, 2)}</pre>
                  </div>
                )}

                {trace.output_data && (
                  <div className="trace-section output">
                    <strong>Output:</strong>
                    <pre>{JSON.stringify(trace.output_data, null, 2)}</pre>
                  </div>
                )}

                {trace.error && (
                  <div className="trace-section error">
                    <strong>Error:</strong>
                    <pre>{trace.error}</pre>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}
