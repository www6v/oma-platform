# Dynamic Workflows Plugin - Frontend Integration

## 📋 Overview

This plugin integrates the pipy-dynamic-workflows backend into the meta-harness console, providing a complete UI for creating, editing, executing, and monitoring workflows.

## 🎯 Features

### 1. Workflow Quickstart
- **WorkflowQuickstart** - Entry page for describing a process or picking a template and running it

### 2. Workflow Editor
- **WorkflowEditor** - Visual YAML editor with live preview
  - Metadata editing (name, description)
  - YAML syntax highlighting (dark theme)
  - Real-time validation
  - Environment variable management
  - Live preview of workflow structure
  - Step dependencies visualization

### 3. Execution Tracing
- **TraceViewer** - Real-time execution monitoring
  - WebSocket-based live updates
  - Step-by-step trace timeline
  - Input/output inspection
  - Error tracking
  - Execution status badges
  - Cancel running executions

## 📁 File Structure

```
dynamic-workflows/
├── WorkflowQuickstart.tsx # Quickstart page
├── WorkflowEditor.tsx     # YAML editor with preview
├── TraceViewer.tsx        # Real-time execution viewer
├── index.tsx              # Plugin registration
├── styles.css             # Component styles
└── README.md              # This file
```

## 🔌 Plugin Registration

The plugin is registered in `console/src/plugins/registry.ts`:

```typescript
import dynamicWorkflowsPlugin from "./dynamic-workflows";

export const consolePlugins: ConsolePlugin[] = [dynamicWorkflowsPlugin];
```

### Routes

| Path | Component | Description |
|------|-----------|-------------|
| `/workflows` | WorkflowQuickstart | Quickstart page |
| `/workflows/new` | WorkflowEditor | Create new workflow |
| `/workflows/:id` | WorkflowEditor | Edit existing workflow |
| `/workflows/:id/traces/:executionId` | TraceViewer | View execution traces |

### Navigation

Adds a "Workflows" section to the sidebar with:
- **Quickstart** - Link to `/workflows`

## 🎨 Component Details

### WorkflowEditor

**Features:**
- Three-panel layout:
  - Left: Metadata and environment variables
  - Center: YAML editor with dark theme
  - Right: Live preview
- Real-time validation
- Environment variable mounter
- Step dependency visualization
- Save and execute actions

**API Endpoints Used:**
- `GET /api/workflows/:id` - Load workflow
- `PUT /api/workflows/:id` - Save workflow
- `POST /api/workflows/validate` - Validate YAML
- `POST /api/workflows/:id/execute` - Execute workflow

**Components:**
- `EnvVarMounter` - Manage environment variables
- `WorkflowPreview` - Visual workflow structure

### TraceViewer

**Features:**
- Real-time WebSocket updates
- Execution status tracking
- Step-by-step timeline
- Input/output inspection
- Error display
- Duration tracking
- Cancel execution support
- Live connection indicator

**API Endpoints Used:**
- `GET /api/workflows/executions/:id` - Load execution
- `GET /api/workflows/executions/:id/traces` - Load traces
- `POST /api/workflows/executions/:id/cancel` - Cancel execution
- `WS /api/workflows/executions/:id/ws` - Real-time updates

**WebSocket Events:**
- `trace_update` - Step trace updated
- `execution_update` - Execution status changed

## 🎨 Styling

All styles are defined in `styles.css` with:

- **Responsive grid layouts**
- **Dark theme YAML editor**
- **Status badges** (completed, failed, running, pending)
- **Hover effects and transitions**
- **Modal overlays**
- **Timeline visualization**
- **Color-coded status indicators**

## 🔧 Integration Points

### Backend API

The plugin communicates with the pipy-dynamic-workflows backend via:
- REST API for CRUD operations
- WebSocket for real-time updates
- Base URL: `/api/workflows`

### Console Plugin System

Implements the `ConsolePlugin` interface:
```typescript
interface ConsolePlugin {
  id: string;
  routes?: PluginRoute[];
  navGroups?: PluginNavGroup[];
}
```

### React Router

Uses `react-router-dom` for:
- Navigation (`useNavigate`)
- Route parameters (`useParams`)
- Route definitions

## 🚀 Usage

### Accessing Workflows

1. Navigate to **Workflows** in the sidebar
2. Click **Quickstart** to describe a process or pick a template
3. Create a new workflow:
   - **From Description**: Describe what you want in natural language
   - **From Scratch**: Start with an empty YAML template
4. Edit the workflow YAML
5. Click **Execute** to run the workflow
6. View execution traces in real-time

### Creating a Workflow

**From Description:**
1. Click **Create from Description**
2. Enter a description (e.g., "Monitor AI news daily and summarize")
3. Click **Generate**
4. Review and edit the generated YAML
5. Save and execute

**From Scratch:**
1. Click **Create from Scratch**
2. Write YAML manually
3. Use the preview panel to validate structure
4. Save when ready

### Monitoring Execution

1. After executing a workflow, you're redirected to the trace viewer
2. Watch real-time updates via WebSocket
3. View each step's input/output
4. Check for errors
5. Cancel if needed

## 🧪 Testing

To test the integration:

1. **Backend Running**: Ensure pipy-dynamic-workflows is integrated and running
2. **Console Running**: Start the console dev server
3. **Navigate**: Go to `/workflows` in the browser
4. **Create**: Create a test workflow
5. **Execute**: Run the workflow
6. **Monitor**: Watch the trace viewer

## 🐛 Troubleshooting

### Workflow List Not Loading
- Check backend API is accessible at `/api/workflows`
- Verify CORS settings if running on different ports

### WebSocket Connection Failed
- Ensure WebSocket endpoint is available
- Check network tab for connection errors
- Verify protocol (ws/wss) matches deployment

### YAML Validation Errors
- Check YAML syntax
- Verify required fields (name, steps)
- Look at validation error messages

### Execution Not Starting
- Verify workflow is saved
- Check backend logs for errors
- Ensure LLM API is configured (if using LLM steps)

## 📊 Architecture

```
┌─────────────────────────────────────────┐
│         Console (React App)             │
├─────────────────────────────────────────┤
│  Dynamic Workflows Plugin               │
│  ├─ WorkflowQuickstart                  │
│  ├─ WorkflowEditor                      │
│  │   ├─ EnvVarMounter                   │
│  │   └─ WorkflowPreview                 │
│  └─ TraceViewer                         │
└─────────────────────────────────────────┘
           │
           │ REST API + WebSocket
           ▼
┌─────────────────────────────────────────┐
│  pipy-dynamic-workflows (Backend)       │
│  ├─ API Routes (14 endpoints)           │
│  ├─ Executor Engine                     │
│  ├─ Database (SQLite)                   │
│  └─ WebSocket Manager                   │
└─────────────────────────────────────────┘
```

## 🎓 Next Steps

### Immediate
- [ ] Test all components with real data
- [ ] Add loading states and error handling
- [ ] Implement keyboard shortcuts
- [ ] Add undo/redo for YAML editor

### Future Enhancements
- [ ] Visual workflow builder (drag & drop)
- [ ] Workflow templates library
- [ ] Execution history dashboard
- [ ] Workflow sharing and collaboration
- [ ] Advanced YAML autocomplete
- [ ] Step-level debugging
- [ ] Performance metrics visualization

## 📝 License

Part of the meta-harness project.
