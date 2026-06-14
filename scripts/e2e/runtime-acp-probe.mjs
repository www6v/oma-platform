#!/usr/bin/env node
/**
 * Harness-side probe: attach to RuntimeRoom and drive session.start + prompt.
 * Used by smoke-runtime-e2e.sh to validate daemon → claude-acp without
 * requiring acp-proxy harness in oma-server session machine.
 */
import WebSocket from 'ws'

const platformUrl = process.env.PLATFORM_URL ?? 'http://127.0.0.1:8787'
const internalSecret = process.env.OMA_INTERNAL_SECRET ?? 'dev-internal-secret'
const runtimeId = process.env.RUNTIME_ID
const sessionId = process.env.SESSION_ID ?? `sess-acp-${Date.now()}`
const tenantId = process.env.TENANT_ID ?? 'default'
const agentId = process.env.ACP_AGENT_ID ?? 'claude-acp'
const promptText = process.env.ACP_PROMPT ?? 'Reply with exactly one word: PONG'
const timeoutMs = Number(process.env.ACP_TIMEOUT_MS ?? '120000')

if (!runtimeId) {
  console.error('RUNTIME_ID required')
  process.exit(2)
}

const wsBase = platformUrl.replace(/^http(s?):\/\//, 'ws$1://').replace(/\/$/, '')
const url = `${wsBase}/v1/internal/runtimes/${runtimeId}/attach-harness`

function waitForMessage(ws, predicate, label) {
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(`timeout waiting for ${label} (${timeoutMs}ms)`))
    }, timeoutMs)

    const onMessage = (raw) => {
      let msg
      try {
        msg = JSON.parse(String(raw))
      } catch {
        return
      }
      if (predicate(msg)) {
        clearTimeout(timer)
        ws.off('message', onMessage)
        resolve(msg)
      }
    }
    ws.on('message', onMessage)
  })
}

const ws = new WebSocket(url, {
  headers: {
    'x-internal-secret': internalSecret,
    'x-session-id': sessionId,
    'x-harness-tenant': tenantId
  }
})

ws.on('error', (err) => {
  console.error('harness ws error:', err.message)
  process.exit(1)
})

ws.on('open', async () => {
  try {
    const attached = await waitForMessage(
      ws,
      (m) => m.type === 'attached',
      'attached'
    )
    console.log('attached:', JSON.stringify(attached))
    if (!attached.daemon_online) {
      throw new Error('daemon offline — start `oma bridge daemon` first')
    }

    ws.send(JSON.stringify({
      type: 'session.start',
      session_id: sessionId,
      tenant_id: tenantId,
      agent_id: agentId,
      turn_id: 'turn-probe-1'
    }))

    const ready = await waitForMessage(
      ws,
      (m) => m.type === 'session.ready' || m.type === 'session.error',
      'session.ready or session.error'
    )
    console.log('start result:', JSON.stringify(ready))
    if (ready.type === 'session.error') {
      throw new Error(ready.message ?? 'session.start failed')
    }

    ws.send(JSON.stringify({
      type: 'session.prompt',
      session_id: sessionId,
      tenant_id: tenantId,
      turn_id: 'turn-probe-1',
      text: promptText
    }))

    const terminal = await waitForMessage(
      ws,
      (m) => m.type === 'session.complete' || m.type === 'session.error',
      'session.complete or session.error'
    )
    console.log('turn result:', JSON.stringify(terminal))
    if (terminal.type === 'session.error') {
      throw new Error(terminal.message ?? 'session.prompt failed')
    }

    console.log('OK: ACP turn completed via runtime relay')
    ws.close()
    process.exit(0)
  } catch (err) {
    console.error('FAIL:', err.message)
    ws.close()
    process.exit(1)
  }
})
