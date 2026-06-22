// WorkflowEditor.tsx - Dynamic Workflows plugin component
import React, { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router';

interface Workflow {
  id: string;
  name: string;
  description: string;
  yaml: string;
  parsed_spec: any;
  env_var_refs: Array<{name: string; secret: boolean}>;
  is_draft: boolean;
  created_at: string;
  updated_at: string;
}

export default function WorkflowEditor() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const [workflow, setWorkflow] = useState<Workflow | null>(null);
  const [yamlContent, setYamlContent] = useState('');
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [validationErrors, setValidationErrors] = useState<string[]>([]);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (id) {
      fetch(`/api/workflows/${id}`)
        .then(res => res.json())
        .then(data => {
          setWorkflow(data);
          setYamlContent(data.yaml);
          setName(data.name);
          setDescription(data.description);
        })
        .catch(err => console.error('Failed to load workflow:', err));
    }
  }, [id]);

  const validateYaml = async (yaml: string) => {
    try {
      const res = await fetch('/api/workflows/validate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ yaml }),
      });
      const data = await res.json();
      setValidationErrors(data.errors || []);
    } catch (err) {
      console.error('Validation failed:', err);
    }
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const res = await fetch(`/api/workflows/${id}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name, description, yaml: yamlContent }),
      });
      if (res.ok) {
        const updated = await res.json();
        setWorkflow(updated);
        alert('Workflow saved!');
      } else {
        const error = await res.json();
        alert(`Failed to save: ${error.detail}`);
      }
    } catch (err) {
      console.error('Save failed:', err);
      alert('Failed to save workflow');
    } finally {
      setSaving(false);
    }
  };

  const handleExecute = async () => {
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

  if (!workflow) return <div>Loading workflow...</div>;

  return (
    <div className="workflow-editor-page">
      <div className="editor-header">
        <button onClick={() => navigate('/workflows')}>← Back</button>
        <div className="header-actions">
          <button onClick={handleSave} disabled={saving}>
            {saving ? 'Saving...' : 'Save'}
          </button>
          <button onClick={handleExecute} className="primary">
            Execute
          </button>
        </div>
      </div>

      <div className="editor-layout">
        <div className="metadata-panel">
          <div className="form-group">
            <label>Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="form-group">
            <label>Description</label>
            <textarea
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
            />
          </div>
          <div className="env-vars-section">
            <h3>Environment Variables</h3>
            <EnvVarMounter
              envVars={workflow.env_var_refs}
              onChange={(envVars) => {}}
            />
          </div>
        </div>

        <div className="yaml-editor-panel">
          <textarea
            value={yamlContent}
            onChange={(e) => {
              setYamlContent(e.target.value);
              validateYaml(e.target.value);
            }}
            className="yaml-textarea"
            spellCheck={false}
          />
        </div>

        <div className="preview-panel">
          <h3>Preview</h3>
          {validationErrors.length > 0 && (
            <div className="validation-errors">
              <h4>Validation Errors:</h4>
              <ul>
                {validationErrors.map((error, i) => (
                  <li key={i} className="error">{error}</li>
                ))}
              </ul>
            </div>
          )}
          <WorkflowPreview spec={workflow.parsed_spec} />
        </div>
      </div>
    </div>
  );
}

function EnvVarMounter({ envVars, onChange }) {
  const [vars, setVars] = useState(envVars || []);

  const addVar = () => {
    const newVars = [...vars, { name: '', secret: false }];
    setVars(newVars);
    onChange(newVars);
  };

  const removeVar = (index) => {
    const newVars = vars.filter((_, i) => i !== index);
    setVars(newVars);
    onChange(newVars);
  };

  const updateVar = (index, field, value) => {
    const newVars = vars.map((v, i) =>
      i === index ? { ...v, [field]: value } : v
    );
    setVars(newVars);
    onChange(newVars);
  };

  return (
    <div className="env-var-mounter">
      {vars.map((envVar, index) => (
        <div key={index} className="env-var-row">
          <input
            type="text"
            placeholder="Variable name"
            value={envVar.name}
            onChange={(e) => updateVar(index, 'name', e.target.value)}
          />
          <label>
            <input
              type="checkbox"
              checked={envVar.secret}
              onChange={(e) => updateVar(index, 'secret', e.target.checked)}
            />
            Secret
          </label>
          <button onClick={() => removeVar(index)}>×</button>
        </div>
      ))}
      <button onClick={addVar} className="add-button">
        + Add Variable
      </button>
    </div>
  );
}

function WorkflowPreview({ spec }) {
  if (!spec) return null;

  return (
    <div className="workflow-preview">
      <div className="preview-section">
        <h4>Steps ({spec.steps?.length || 0})</h4>
        <div className="steps-list">
          {spec.steps?.map((step, i) => (
            <div key={i} className="step-item">
              <span className="step-index">{i + 1}</span>
              <span className="step-name">{step.name}</span>
              <span className="step-action">{step.action}</span>
              {step.depends_on && (
                <span className="step-deps">
                  depends: {step.depends_on.join(', ')}
                </span>
              )}
            </div>
          ))}
        </div>
      </div>

      {spec.inputs && spec.inputs.length > 0 && (
        <div className="preview-section">
          <h4>Inputs ({spec.inputs.length})</h4>
          <ul>
            {spec.inputs.map((input, i) => (
              <li key={i}>
                <strong>{input.name}</strong> ({input.type})
                {input.required && <span className="required">*</span>}
                {input.description && <span>: {input.description}</span>}
              </li>
            ))}
          </ul>
        </div>
      )}

      {spec.tools && spec.tools.length > 0 && (
        <div className="preview-section">
          <h4>Tools</h4>
          <div className="tools-list">
            {spec.tools.map((tool, i) => (
              <span key={i} className="tool-badge">{tool}</span>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}
