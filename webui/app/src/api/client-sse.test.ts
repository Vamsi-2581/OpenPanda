// SPDX-License-Identifier: AGPL-3.0-or-later

import assert from 'node:assert/strict'
import { test } from 'node:test'
import { api, ApiError, askSessionStream, openSSE, subscribeEvents } from './client.ts'

const SSE_HEADERS = { 'Content-Type': 'text/event-stream' }

/** A body that never yields and never ends: the far end of a socket that is
 *  gone without anyone being told. `read()` on this parks forever, which is
 *  exactly the failure the browser never reports. */
function silentStream(): ReadableStream<Uint8Array> {
  return new ReadableStream<Uint8Array>({
    start() {
      /* nothing is ever enqueued */
    },
  })
}

function chunkedStream(chunks: string[], gapMs = 0): ReadableStream<Uint8Array> {
  const enc = new TextEncoder()
  let i = 0
  return new ReadableStream<Uint8Array>({
    async pull(controller) {
      if (gapMs > 0) await new Promise((r) => setTimeout(r, gapMs))
      if (i < chunks.length) {
        controller.enqueue(enc.encode(chunks[i]!))
        i++
      } else {
        controller.close()
      }
    },
  })
}

async function waitFor(cond: () => boolean, what: string, ms = 2000): Promise<void> {
  const deadline = Date.now() + ms
  while (!cond()) {
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${what}`)
    await new Promise((r) => setTimeout(r, 5))
  }
}

test('idle watchdog rejects a stream that goes silent', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = (async () =>
    new Response(silentStream(), { status: 200, headers: SSE_HEADERS })) as any
  try {
    await assert.rejects(
      () => openSSE('/api/events', { idleTimeoutMs: 25, onEvent: () => {} }),
      (err: unknown) => {
        assert(err instanceof ApiError)
        assert.equal(err.status, 0)
        assert.match(err.message, /stalled/)
        return true
      },
    )
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('the watchdog is re-armed by every chunk, so a paced stream survives it', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = (async () =>
    new Response(chunkedStream([': a\n\n', ': b\n\n', ': c\n\n'], 15), {
      status: 200,
      headers: SSE_HEADERS,
    })) as any
  try {
    // Three 15ms gaps against a 40ms window: only a watchdog that resets on
    // arrival keeps this stream alive past its first window.
    await openSSE('/api/events', { idleTimeoutMs: 40, onEvent: () => {} })
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('onActivity fires for every chunk, heartbeat comments included', async () => {
  const originalFetch = globalThis.fetch
  globalThis.fetch = (async () =>
    new Response(chunkedStream([': heartbeat\n\n', 'event: change\ndata: tasks fp1\n\n']), {
      status: 200,
      headers: SSE_HEADERS,
    })) as any
  let activity = 0
  const changes: string[] = []
  try {
    await openSSE('/api/events', {
      onActivity: () => activity++,
      onEvent: (event) => changes.push(event),
    })
    assert.equal(activity, 2, 'a comment-only frame is still a live byte')
    assert.deepEqual(changes, ['change'])
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('a lost link is re-dialled the moment the browser reports it is back', async () => {
  const originalFetch = globalThis.fetch
  const win = new EventTarget()
  const doc = Object.assign(new EventTarget(), { visibilityState: 'visible' })
  ;(globalThis as any).window = win
  ;(globalThis as any).document = doc

  let calls = 0
  const kinds: string[][] = []
  globalThis.fetch = (async () => {
    calls++
    const body =
      calls === 1
        ? silentStream()
        : chunkedStream(['event: change\ndata: tasks fp1\n\n'])
    return new Response(body, { status: 200, headers: SSE_HEADERS })
  }) as any

  const ctrl = new AbortController()
  try {
    const done = subscribeEvents({
      trace: true,
      signal: ctrl.signal,
      onChange: (ev) => kinds.push(ev.kinds),
    })
    await waitFor(() => calls === 1, 'the first connection')
    win.dispatchEvent(new Event('online'))
    await waitFor(() => calls === 2, 'the forced redial')
    // nth > 1, so the fresh link announces itself as a recovery: that is what
    // makes every subscriber re-fetch the state it missed while the socket
    // was a corpse.
    await waitFor(() => kinds.some((k) => k.includes('reconnect')), 'the reconnect notice')
    ctrl.abort()
    await done
  } finally {
    globalThis.fetch = originalFetch
    delete (globalThis as any).window
    delete (globalThis as any).document
  }
})

test('the ask stream names a dispatched task the moment its row exists', async () => {
  const originalFetch = globalThis.fetch
  const frames = [
    'event: operation\ndata: {"operation_id":"op-1"}\n\n',
    'event: task\ndata: {"task_id":"t-42"}\n\n',
    'event: result\ndata: {"kind":"task","task_id":"t-42"}\n\n',
  ]
  globalThis.fetch = (async () =>
    new Response(chunkedStream(frames), { status: 200, headers: SSE_HEADERS })) as any
  const seen: string[] = []
  try {
    await askSessionStream('s1', 'hi', false, {
      onReasoning: () => {},
      onDelta: () => {},
      onStatus: () => {},
      onOperation: (id) => seen.push(`op:${id}`),
      onTask: (id) => seen.push(`task:${id}`),
      onResult: (r) => seen.push(`result:${r.task_id ?? ''}`),
      onError: () => {},
    })
    // Order matters: the task frame has to arrive before the result, because
    // the result only lands once the task has already settled.
    assert.deepEqual(seen, ['op:op-1', 'task:t-42', 'result:t-42'])
  } finally {
    globalThis.fetch = originalFetch
  }
})

test('the brief read asks for the light projection', async () => {
  const originalFetch = globalThis.fetch
  const urls: string[] = []
  globalThis.fetch = (async (url: unknown) => {
    urls.push(String(url))
    return new Response(JSON.stringify({ id: 't-1', state: 'running' }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    })
  }) as any
  try {
    const task = await api.taskBrief('t-1')
    assert.equal(task.id, 't-1')
    // A missing ?brief=1 would silently fall back to the full read, so the
    // URL is the contract worth pinning.
    assert.deepEqual(urls, ['/api/tasks/t-1?brief=1'])
  } finally {
    globalThis.fetch = originalFetch
  }
})
