// WorkflowQuickstart.tsx - Split-panel create flow (Phase C)
import React, { useEffect, useState } from 'react';
import { useNavigate } from 'react-router';
import { workflowFetch } from './workflowApi';

interface TemplateSummary {
  id: string;
  name: string;
  description: string;
  tags: string[];
  category: string;
  step_count: number;
}

interface GenerateMetadata {
  source?: string;
  model_used?: string;
}

export default function WorkflowQuickstart() {
  const navigate = useNavigate();
  const [prompt, setPrompt] = useState('');
  const [templates, setTemplates] = useState<TemplateSummary[]>([]);
  const [selectedTemplate, setSelectedTemplate] = useState<string | null>(null);
  const [yamlPreview, setYamlPreview] = useState('');
  const [parsedName, setParsedName] = useState('');
  const [parsedDescription, setParsedDescription] = useState('');
  const [metadata, setMetadata] = useState<GenerateMetadata | null>(null);
  const [loadingTemplates, setLoadingTemplates] = useState(true);
  const [generating, setGenerating] = useState(false);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    workflowFetch('/api/workflows/templates')
      .then((res) => {
        if (!res.ok) {
          throw new Error('Failed to load templates');
        }
        return res.json();
      })
      .then((data) => {
        setTemplates(data);
        setLoadingTemplates(false);
      })
      .catch((err) => {
        console.error(err);
        setLoadingTemplates(false);
      });
  }, []);

  const handleGenerate = async () => {
    if (!prompt.trim()) {
      return;
    }
    setGenerating(true);
    setError('');
    setSelectedTemplate(null);
    try {
      const res = await workflowFetch('/api/workflows/generate', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ prompt }),
      });
      if (!res.ok) {
        const detail = await res.json();
        throw new Error(
          typeof detail.detail === 'string'
            ? detail.detail
            : JSON.stringify(detail.detail),
        );
      }
      const data = await res.json();
      setYamlPreview(data.yaml || '');
      setParsedName(data.parsed?.name || 'generated_workflow');
      setParsedDescription(data.parsed?.description || prompt);
      setMetadata(data.metadata || null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Generation failed');
    } finally {
      setGenerating(false);
    }
  };

  const handleUseTemplate = async (templateId: string) => {
    setError('');
    setSelectedTemplate(templateId);
    try {
      const res = await workflowFetch(`/api/workflows/templates/${templateId}`);
      if (!res.ok) {
        throw new Error('Failed to load template');
      }
      const data = await res.json();
      setYamlPreview(data.yaml || '');
      setParsedName(data.parsed?.name || data.name);
      setParsedDescription(data.parsed?.description || data.description);
      setMetadata({ source: 'template', model_used: templateId });
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Template load failed');
    }
  };

  const handleRunWorkflow = async () => {
    if (!yamlPreview.trim()) {
      setError('Generate or select a workflow first');
      return;
    }
    setRunning(true);
    setError('');
    try {
      const createRes = await workflowFetch('/api/workflows', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: parsedName || 'untitled_workflow',
          description: parsedDescription || '',
          yaml: yamlPreview,
        }),
      });
      if (!createRes.ok) {
        const detail = await createRes.json();
        throw new Error(
          typeof detail.detail === 'string'
            ? detail.detail
            : JSON.stringify(detail.detail),
        );
      }
      const workflow = await createRes.json();
      const execRes = await workflowFetch(`/api/workflows/${workflow.id}/execute`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      });
      if (!execRes.ok) {
        const detail = await execRes.json();
        throw new Error(
          typeof detail.detail === 'string'
            ? detail.detail
            : JSON.stringify(detail.detail),
        );
      }
      const execData = await execRes.json();
      navigate(`/workflows/${workflow.id}/traces/${execData.execution_id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Run failed');
    } finally {
      setRunning(false);
    }
  };

  const handleOpenEditor = async () => {
    if (!yamlPreview.trim()) {
      setError('Generate or select a workflow first');
      return;
    }
    setRunning(true);
    setError('');
    try {
      const createRes = await workflowFetch('/api/workflows', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: parsedName || 'untitled_workflow',
          description: parsedDescription || '',
          yaml: yamlPreview,
        }),
      });
      if (!createRes.ok) {
        const detail = await createRes.json();
        throw new Error(
          typeof detail.detail === 'string'
            ? detail.detail
            : JSON.stringify(detail.detail),
        );
      }
      const workflow = await createRes.json();
      navigate(`/workflows/${workflow.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Save failed');
    } finally {
      setRunning(false);
    }
  };

  return (
    <div className="quickstart-page">
      <div className="quickstart-header">
        <div>
          <h1>Workflow Quickstart</h1>
          <p>Describe your process or pick a template, then run it.</p>
        </div>
        <div className="header-actions">
          <button onClick={() => navigate('/workflows/all')}>
            All Workflows
          </button>
          <button onClick={() => navigate('/workflows/new')}>
            Blank Editor
          </button>
        </div>
      </div>

      <div className="quickstart-layout">
        <div className="quickstart-left">
          <h2>Describe your workflow</h2>
          <textarea
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
            placeholder="e.g., Search for AI papers, summarize top findings, and draft a report"
            rows={8}
          />
          <button
            className="primary"
            onClick={handleGenerate}
            disabled={!prompt.trim() || generating}
          >
            {generating ? 'Generating...' : 'Generate YAML'}
          </button>

          <div className="template-section">
            <h3>Templates</h3>
            {loadingTemplates ? (
              <p className="muted">Loading templates...</p>
            ) : (
              <div className="template-grid">
                {templates.map((template) => (
                  <button
                    key={template.id}
                    type="button"
                    className={
                      selectedTemplate === template.id
                        ? 'template-card selected'
                        : 'template-card'
                    }
                    onClick={() => handleUseTemplate(template.id)}
                  >
                    <strong>{template.name}</strong>
                    <span>{template.description}</span>
                    <small>{template.step_count} steps</small>
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>

        <div className="quickstart-right">
          <div className="preview-toolbar">
            <h2>YAML Preview</h2>
            {metadata?.source && (
              <span className="source-badge">source: {metadata.source}</span>
            )}
          </div>
          {error && <div className="error-message">{error}</div>}
          <textarea
            className="yaml-textarea quickstart-yaml"
            value={yamlPreview}
            onChange={(e) => setYamlPreview(e.target.value)}
            placeholder="Generated workflow YAML will appear here..."
            spellCheck={false}
          />
          <div className="quickstart-actions">
            <button
              onClick={handleOpenEditor}
              disabled={!yamlPreview || running}
            >
              Save &amp; Edit
            </button>
            <button
              className="primary"
              onClick={handleRunWorkflow}
              disabled={!yamlPreview || running}
            >
              {running ? 'Running...' : 'Run Workflow'}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
