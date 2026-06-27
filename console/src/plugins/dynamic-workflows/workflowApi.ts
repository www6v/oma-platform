import { getActiveTenantId } from '../../lib/api';

/** Headers required by the workflow harness tenant guard. */
export function workflowRequestHeaders(
  extraHeaders?: Record<string, string>,
): Record<string, string> {
  const headers: Record<string, string> = { ...extraHeaders };
  const activeTenant = getActiveTenantId();
  if (activeTenant) {
    headers['x-active-tenant'] = activeTenant;
  }
  return headers;
}

export function workflowFetch(
  input: string,
  init?: RequestInit,
): Promise<Response> {
  const extra =
    init?.headers && typeof init.headers === 'object' &&
    !Array.isArray(init.headers) && !(init.headers instanceof Headers)
      ? init.headers as Record<string, string>
      : undefined;
  return fetch(input, {
    ...init,
    headers: workflowRequestHeaders(extra),
  });
}
