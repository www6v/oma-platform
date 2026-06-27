import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { http, HttpResponse } from 'msw';
import { MemoryRouter, Route, Routes } from 'react-router';
import { describe, expect, it } from 'vitest';
import { server } from '../../mocks/server';
import WorkflowQuickstart from './WorkflowQuickstart';

const mockTemplates = [
  {
    id: 'deep-research',
    name: 'deep_research',
    description: 'Multi-stage research workflow',
    tags: ['research'],
    category: 'research',
    step_count: 3,
  },
  {
    id: 'content-pipeline',
    name: 'content_pipeline',
    description: 'Fan out topics through stages',
    tags: ['pipeline'],
    category: 'content',
    step_count: 1,
  },
];

const mockGenerated = {
  yaml: 'name: demo_flow\ndescription: "demo"\nsteps:\n  - name: step1\n    action: llm_execute\n    params:\n      prompt: "hi"\n',
  parsed: {
    name: 'demo_flow',
    description: 'demo',
    steps: [{ name: 'step1', action: 'llm_execute', params: { prompt: 'hi' } }],
  },
  metadata: { source: 'template', model_used: 'template' },
};

const mockTemplateDetail = {
  id: 'deep-research',
  name: 'deep_research',
  description: 'Multi-stage research workflow',
  tags: ['research'],
  category: 'research',
  step_count: 3,
  yaml: 'name: deep_research\ndescription: "research"\nsteps:\n  - name: scan\n    action: web_search\n    params:\n      query: "ai"\n',
  parsed: {
    name: 'deep_research',
    description: 'research',
    steps: [{ name: 'scan', action: 'web_search', params: { query: 'ai' } }],
  },
};

function renderQuickstart(initialPath = '/workflows') {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <Routes>
        <Route path="/workflows" element={<WorkflowQuickstart />} />
        <Route
          path="/workflows/:id/traces/:executionId"
          element={<div data-testid="trace-page">Trace viewer</div>}
        />
      </Routes>
    </MemoryRouter>,
  );
}

describe('WorkflowQuickstart', () => {
  it('loads and renders bundled templates', async () => {
    server.use(
      http.get('/api/workflows/templates', () => HttpResponse.json(mockTemplates)),
    );

    renderQuickstart();

    expect(await screen.findByText('deep_research')).toBeInTheDocument();
    expect(screen.getByText('content_pipeline')).toBeInTheDocument();
    expect(screen.getByText('Multi-stage research workflow')).toBeInTheDocument();
  });

  it('generates YAML from natural language prompt', async () => {
    server.use(
      http.get('/api/workflows/templates', () => HttpResponse.json(mockTemplates)),
      http.post('/api/workflows/generate', async ({ request }) => {
        const body = await request.json() as { prompt?: string };
        expect(body.prompt).toContain('summarize papers');
        return HttpResponse.json(mockGenerated);
      }),
    );

    renderQuickstart();
    await screen.findByText('deep_research');

    await userEvent.type(
      screen.getByPlaceholderText(/Search for AI papers/i),
      'Search and summarize papers',
    );
    await userEvent.click(screen.getByRole('button', { name: 'Generate YAML' }));

    await waitFor(() => {
      expect(screen.getByDisplayValue(/name: demo_flow/)).toBeInTheDocument();
    });
    expect(screen.getByText('source: template')).toBeInTheDocument();
  });

  it('loads YAML when a template card is selected', async () => {
    server.use(
      http.get('/api/workflows/templates', () => HttpResponse.json(mockTemplates)),
      http.get('/api/workflows/templates/deep-research', () =>
        HttpResponse.json(mockTemplateDetail),
      ),
    );

    renderQuickstart();
    await screen.findByText('deep_research');

    await userEvent.click(
      screen.getByRole('button', { name: /deep_research/i }),
    );

    await waitFor(() => {
      expect(screen.getByDisplayValue(/name: deep_research/)).toBeInTheDocument();
    });
    expect(screen.getByText('source: template')).toBeInTheDocument();
  });

  it('creates workflow and navigates to trace viewer on run', async () => {
    server.use(
      http.get('/api/workflows/templates', () => HttpResponse.json(mockTemplates)),
      http.get('/api/workflows/templates/content-pipeline', () =>
        HttpResponse.json({
          ...mockTemplateDetail,
          id: 'content-pipeline',
          name: 'content_pipeline',
          yaml: mockGenerated.yaml,
          parsed: mockGenerated.parsed,
        }),
      ),
      http.post('/api/workflows', async () =>
        HttpResponse.json({
          id: 'wf-123',
          name: 'demo_flow',
          description: 'demo',
          yaml: mockGenerated.yaml,
          parsed_spec: mockGenerated.parsed,
          env_var_refs: [],
          is_draft: false,
          created_at: '2026-01-01T00:00:00Z',
          updated_at: '2026-01-01T00:00:00Z',
        }),
      ),
      http.post('/api/workflows/wf-123/execute', () =>
        HttpResponse.json({ execution_id: 'exec-456', status: 'running' }),
      ),
    );

    renderQuickstart();
    await screen.findByText('content_pipeline');

    await userEvent.click(
      screen.getByRole('button', { name: /content_pipeline/i }),
    );

    await waitFor(() => {
      expect(screen.getByDisplayValue(/name: demo_flow/)).toBeInTheDocument();
    });

    await userEvent.click(screen.getByRole('button', { name: 'Run Workflow' }));

    expect(await screen.findByTestId('trace-page')).toBeInTheDocument();
  });
});
