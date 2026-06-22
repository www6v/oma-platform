# Execute Button Bug Fix - oma-platform Console

## 问题描述

点击 workflow 的 "Execute" 按钮后,页面跳转到错误的 URL:
```
http://localhost:5174/workflows/{workflow_id}/traces/undefined
```

`execution_id` 显示为 `undefined`,导致 trace 页面无法加载。

## 根本原因

前端代码在调用 `/api/workflows/{id}/execute` API 时存在以下问题:

1. **缺少 Content-Type header**: FastAPI 需要 `Content-Type: application/json` 才能正确解析请求体
2. **缺少请求体**: 即使 `ExecuteWorkflowRequest` 的所有字段都是可选的,POST 请求仍需要发送空 JSON body `{}`
3. **没有错误处理**: 如果 API 返回错误,前端仍然尝试访问 `data.execution_id`,导致 `undefined`

### 原始代码

```typescript
const handleExecute = async (id: string) => {
  const res = await fetch(`/api/workflows/${id}/execute`, { method: 'POST' });
  const data = await res.json();
  navigate(`/workflows/${id}/traces/${data.execution_id}`);
};
```

问题:
- ❌ 没有 `Content-Type: application/json` header
- ❌ 没有发送 body
- ❌ 没有检查 `res.ok`
- ❌ 没有验证 `data.execution_id` 存在
- ❌ 没有错误处理

## 修复的文件

### 1. oma-platform console 插件组件

**文件**: `/Users/t-wangwei07/Downloads/workspacePy/mycode/oma/oma-platform/console/src/plugins/dynamic-workflows/WorkflowList.tsx`

修复内容:
```typescript
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
```

### 2. WorkflowEditor.tsx

**文件**: `/Users/t-wangwei07/Downloads/workspacePy/mycode/oma/oma-platform/console/src/plugins/dynamic-workflows/WorkflowEditor.tsx`

同样的修复应用到 `handleExecute` 函数。

### 3. piPy-dynamic-workflows 包组件(已同步修复)

**文件**:
- `/Users/t-wangwei07/Downloads/workspacePy/mycode/oma/piPy-dynamic-workflows/pipy_dynamic_workflows/components/WorkflowList.tsx`
- `/Users/t-wangwei07/Downloads/workspacePy/mycode/oma/piPy-dynamic-workflows/pipy_dynamic_workflows/components/WorkflowEditor.tsx`
- `/Users/t-wangwei07/Downloads/workspacePy/mycode/oma/piPy-dynamic-workflows/pipy_dynamic_workflows/components/TraceViewer.tsx`

## 修复说明

### 1. 添加 Content-Type header
```typescript
headers: { 'Content-Type': 'application/json' }
```
FastAPI 使用 Pydantic model 解析请求体,需要正确的 Content-Type。

### 2. 发送空 JSON body
```typescript
body: JSON.stringify({})
```
即使所有字段都是可选的,POST 请求仍需要有效的 JSON body。

### 3. 检查响应状态
```typescript
if (!res.ok) {
  const error = await res.json();
  throw new Error(error.detail || 'Execution failed');
}
```
捕获 HTTP 错误(4xx, 5xx)并显示详细错误信息。

### 4. 验证必要字段
```typescript
if (!data.execution_id) {
  throw new Error('No execution_id in response');
}
```
确保响应包含必需的字段,避免跳转到 `undefined` URL。

### 5. 错误处理
```typescript
try {
  // ... 执行逻辑
} catch (err) {
  console.error('Execute failed:', err);
  alert(`Failed to execute workflow: ${err.message}`);
}
```
捕获所有异常并显示用户友好的错误提示。

## 测试验证

### 测试步骤

1. 重启前端开发服务器:
   ```bash
   cd /Users/t-wangwei07/Downloads/workspacePy/mycode/oma/oma-platform/console
   npm run dev
   ```

2. 访问 workflow 列表页面 (http://localhost:5174/workflows)

3. 点击任意 workflow 的 "Execute" 按钮

4. 验证:
   - ✅ 不再跳转到 `undefined` URL
   - ✅ 成功时跳转到 `/workflows/{id}/traces/{execution_id}`(真实的 execution_id)
   - ✅ 失败时显示错误提示框,停留在当前页面

### 预期行为

**成功执行**:
- API 返回 `{ execution_id: "abc-123-...", status: "running" }`
- 前端跳转到 `/workflows/{workflow_id}/traces/abc-123-...`
- Trace 页面加载并显示执行状态

**执行失败**:
- API 返回错误(如 404 Workflow not found)
- 前端显示 alert: "Failed to execute workflow: Workflow not found"
- 停留在当前页面

## 技术细节

### FastAPI 请求解析

```python
class ExecuteWorkflowRequest(BaseModel):
    env_vars: Optional[Dict[str, str]] = None
    session_id: Optional[str] = None

@workflow_router.post("/{workflow_id}/execute")
async def execute_workflow(
    workflow_id: str, 
    request: ExecuteWorkflowRequest
):
    ...
```

即使所有字段都是 `Optional`,FastAPI 仍然需要:
1. `Content-Type: application/json` header
2. 有效的 JSON body (可以是 `{}`)

否则会返回 `422 Unprocessable Entity` 错误。

### API 响应格式

**成功响应** (200 OK):
```json
{
  "execution_id": "fc79abea-4c30-4896-86b7-efda0f19cb16",
  "status": "running"
}
```

**错误响应** (404 Not Found):
```json
{
  "detail": "Workflow not found"
}
```

## 相关文件

### 后端 API
- `pipy_dynamic_workflows/api/routes.py` - Execute endpoint (line 152-196)
- `pipy_dynamic_workflows/lib/executor.py` - WorkflowExecutor
- `pipy_dynamic_workflows/lib/database.py` - Database operations

### 前端组件 (oma-platform console)
- `console/src/plugins/dynamic-workflows/WorkflowList.tsx` - 列表页 Execute 按钮
- `console/src/plugins/dynamic-workflows/WorkflowEditor.tsx` - 编辑页 Execute 按钮
- `console/src/plugins/dynamic-workflows/TraceViewer.tsx` - Trace 查看页面

### 前端组件 (piPy-dynamic-workflows 包)
- `piPy-dynamic-workflows/pipy_dynamic_workflows/components/WorkflowList.tsx`
- `piPy-dynamic-workflows/pipy_dynamic_workflows/components/WorkflowEditor.tsx`
- `piPy-dynamic-workflows/pipy_dynamic_workflows/components/TraceViewer.tsx`

## 后续优化建议

1. **统一 API 客户端**: 创建统一的 API 客户端库,封装错误处理逻辑
2. **类型安全**: 使用 TypeScript 接口定义 API 响应
3. **Toast 通知**: 用 toast 替代 alert,提供更好的用户体验
4. **加载状态**: 添加 loading indicator,防止重复点击

### 示例: 统一 API 客户端

```typescript
// api/client.ts
interface ExecuteResponse {
  execution_id: string;
  status: 'running' | 'completed' | 'failed';
}

export async function executeWorkflow(workflowId: string): Promise<string> {
  const res = await fetch(`/api/workflows/${workflowId}/execute`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({}),
  });

  if (!res.ok) {
    const error = await res.json();
    throw new Error(error.detail || 'Execution failed');
  }

  const data: ExecuteResponse = await res.json();

  if (!data.execution_id) {
    throw new Error('No execution_id in response');
  }

  return data.execution_id;
}

// components/WorkflowList.tsx
const handleExecute = async (id: string) => {
  try {
    const executionId = await executeWorkflow(id);
    navigate(`/workflows/${id}/traces/${executionId}`);
  } catch (err) {
    alert(`Failed to execute: ${err.message}`);
  }
};
```

## 总结

✅ 修复了 3 个前端组件中的 Execute 按钮
✅ 添加了完整的错误处理和响应验证
✅ 确保正确发送 Content-Type header 和请求体
✅ 提供了用户友好的错误提示

现在点击 Execute 按钮应该能正确跳转到 trace 页面,或者在失败时显示错误信息。
