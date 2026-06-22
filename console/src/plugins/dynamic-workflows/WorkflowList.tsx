// WorkflowList.tsx - Dynamic Workflows plugin component
import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';

interface Workflow {
  id: string;
  name: string;
  description: string;
  is_draft: boolean;
  created_at: string;
  updated_at: string;
}

export default function WorkflowList() {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [loading, setLoading] = useState(true);
  const [showGenerator, setShowGenerator] = useState(false);
  const navigate = useNavigate();

  useEffect(() => {
    fetch('/api/workflows')
      .then(res => res.json())
      .then(data => {
        setWorkflows(data);
        setLoading(false);
      })
      .catch(err => {
        console.error('Failed to load workflows:', err);
        setLoading(false);
      });
  }, []);

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this workflow?')) return;
    await fetch(`/api/workflows/${id}`, { method: 'DELETE' });
    setWorkflows(workflows.filter(w => w.id !== id));
  };

  const handleExecute = async (id: string) => {
    try {
      const res = await fetch(`/api/workflows/${id}/execute`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });

      if (!res.ok) {
        const error = await res.json();
        throw new Error(error.detail || 'Execution failed');
      }

      const data = await res.json();

      if (!data.execution_id) {
        throw new Error('No execution_id in response');
      }

      navigate(`/workflows/${id}/traces/${data.execution_id}`);
    } catch (err) {
      console.error('Execute failed:', err);
      alert(`Failed to execute workflow: ${err.message}`);
    }
  };

  if (loading) return <div>Loading workflows...</div>;

  return (
    <div className="workflow-list-page">
      <div className="page-header">
        <h1>My Workflows</h1>
        <div className="header-actions">
          <button onClick={() => setShowGenerator(true)}>
            Create from Description
          </button>
          <button onClick={() => navigate('/workflows/new')}>
            Create from Scratch
          </button>
        </div>
      </div>

      {workflows.length === 0 ? (
        <div className="empty-state">
          <p>No workflows yet.</p>
          <p>Create your first workflow from a natural language description!</p>
        </div>
      ) : (
        <div className="workflow-grid">
          {workflows.map(workflow => (
            <div key={workflow.id} className="workflow-card">
              <div className="card-header">
                <h3>{workflow.name}</h3>
                {workflow.is_draft && <span className="draft-badge">Draft</span>}
              </div>
              <p className="description">{workflow.description}</p>
              <div className="card-footer">
                <span className="date">
                  Updated: {new Date(workflow.updated_at).toLocaleDateString()}
                </span>
                <div className="actions">
                  <button onClick={() => navigate(`/workflows/${workflow.id}`)}>
                    Edit
                  </button>
                  <button onClick={() => handleExecute(workflow.id)}>
                    Execute
                  </button>
                  <button onClick={() => handleDelete(workflow.id)} className="danger">
                    Delete
                  </button>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {showGenerator && (
        <WorkflowGeneratorModal
          onClose={() => setShowGenerator(false)}
          onCreated={(workflow) => {
            setWorkflows([...workflows, workflow]);
            setShowGenerator(false);
          }}
        />
      )}
    </div>
  );
}

function WorkflowGeneratorModal({ onClose, onCreated }) {
  const [prompt, setPrompt] = useState('');
  const [generating, setGenerating] = useState(false);
  const [error, setError] = useState('');

  const handleGenerate = async () => {
    setGenerating(true);
    setError('');
    try {
      const res = await fetch('/api/workflows/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt }),
      });
      if (!res.ok) throw new Error('Generation failed');
      const data = await res.json();

      // Create the workflow
      const createRes = await fetch('/api/workflows', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: data.parsed.name || 'Untitled Workflow',
          description: data.parsed.description || '',
          yaml: data.yaml,
        }),
      });
      if (!createRes.ok) throw new Error('Failed to create workflow');
      const workflow = await createRes.json();
      onCreated(workflow);
    } catch (err) {
      setError(err.message);
    } finally {
      setGenerating(false);
    }
  };

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content" onClick={e => e.stopPropagation()}>
        <h2>Create Workflow from Description</h2>
        <p>Describe what you want the workflow to do:</p>
        <textarea
          value={prompt}
          onChange={(e) => setPrompt(e.target.value)}
          placeholder="e.g., Monitor AI research news daily, summarize top 5 papers, and post to Slack"
          rows={5}
        />
        {error && <div className="error-message">{error}</div>}
        <div className="modal-actions">
          <button onClick={onClose}>Cancel</button>
          <button
            onClick={handleGenerate}
            disabled={!prompt || generating}
            className="primary"
          >
            {generating ? 'Generating...' : 'Generate'}
          </button>
        </div>
      </div>
    </div>
  );
}
