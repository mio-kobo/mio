// Playground 控制器：每次执行均在全新 Worker 中运行独立的 Go/Wasm 运行时。
// 状态绝不跨请求复用；Stop 或超时直接终止 Worker（即使 VM 处于同步死循环也能立即中断），并随即重建新实例。
export const LIMITS = Object.freeze({
  sourceBytes: 256 * 1024,      // mirrors playground.MaxSourceBytes
  mochiBytes: 4 * 1024 * 1024,  // mirrors playground.MaxMochiBytes
  timeoutMs: 10000,
  initTimeoutMs: 20000,
});

const OPERATIONS = ['run', 'check', 'compile', 'exec'];

export class Playground {
  constructor(onState, onResult, options = {}) {
    this.onState = onState;
    this.onResult = onResult;
    this.workerURL = options.workerURL || './worker.js';
    this.wasmURL = options.wasmURL || './mio.wasm';
    this.timeoutMs = options.timeoutMs || LIMITS.timeoutMs;
    this.instructionLimit = options.instructionLimit || 0;
    this.sequence = 0;
    this.generation = 0;
    this.workersCreated = 0;
    this.worker = null;
    this.ready = false;
    this.active = null;
    // The module is compiled once; every Worker instantiates its own instance from it.
    this.module = WebAssembly.compileStreaming
      ? WebAssembly.compileStreaming(fetch(this.wasmURL)).catch(() => this.compileFallback())
      : this.compileFallback();
    this.reset();
  }

  async compileFallback() {
    const response = await fetch(this.wasmURL);
    if (!response.ok) throw new Error(`Wasm fetch: HTTP ${response.status}`);
    return WebAssembly.compile(await response.arrayBuffer());
  }

  dispose() {
    clearTimeout(this.timer);
    this.generation++;
    if (this.worker) this.worker.terminate();
    this.worker = null;
    this.ready = false;
    this.active = null;
  }

  reset() {
    this.dispose();
    const generation = this.generation;
    this.onState('loading');
    const worker = this.worker = new Worker(this.workerURL);
    this.workersCreated++;
    const id = ++this.sequence;
    const fail = error => {
      if (generation !== this.generation) return;
      const requestId = this.active;
      this.dispose();
      this.onState('error', { error });
      if (requestId !== null) this.onResult(failure(requestId, 'internal_error', error));
    };
    worker.onerror = event => { event.preventDefault(); fail(event.message || 'Worker failed'); };
    worker.onmessageerror = () => fail('Worker message could not be decoded');
    worker.onmessage = ({ data }) => {
      if (generation !== this.generation) return;
      if (data.type === 'ready' && data.id === id && this.active === null) {
        clearTimeout(this.timer);
        this.ready = true;
        this.onState('ready', data);
      } else if (data.type === 'fatal') {
        fail(data.error);
      } else if (data.type === 'result' && data.id === this.active && data.id !== null) {
        clearTimeout(this.timer);
        const result = { ...data.result, id: data.id, elapsedMs: data.elapsedMs };
        this.active = null;
        this.onResult(result);
        this.reset(); // never reuse a runtime across requests
      }
    };
    this.timer = setTimeout(() => fail(`Wasm initialization timed out (${LIMITS.initTimeoutMs / 1000} seconds)`), LIMITS.initTimeoutMs);
    this.module.then(module => {
      if (generation === this.generation) worker.postMessage({ type: 'init', id, module });
    }, error => fail(String(error)));
  }

  // Returns the request id, or 0 when the request was not started (the reason is reported via onResult).
  execute(operation, payload) {
    if (!OPERATIONS.includes(operation)) return 0;
    if (!this.ready || this.active !== null) return 0;
    const request = { operation };
    if (this.instructionLimit) request.instructionLimit = this.instructionLimit;
    if (operation === 'exec') {
      if (!(payload instanceof Uint8Array)) return 0;
      if (payload.length > LIMITS.mochiBytes) {
        this.onResult(failure(0, 'rejected', `Mochi file limit exceeded (${LIMITS.mochiBytes} bytes)`));
        return 0;
      }
      request.mochi = payload;
    } else {
      const source = String(payload ?? '');
      if (new TextEncoder().encode(source).length > LIMITS.sourceBytes) {
        this.onResult(failure(0, 'rejected', `source limit exceeded (${LIMITS.sourceBytes} bytes)`));
        return 0;
      }
      request.source = source;
    }
    const id = this.active = ++this.sequence;
    this.onState('running', { operation });
    this.timer = setTimeout(() => this.stop('timeout'), this.timeoutMs);
    this.worker.postMessage({ type: 'request', id, request });
    return id;
  }

  stop(status = 'cancelled') {
    const id = this.active;
    this.dispose(); // terminate() stops the Worker even inside a synchronous VM loop
    if (id !== null) {
      const message = status === 'timeout'
        ? `execution timed out after ${this.timeoutMs} ms (Worker terminated)`
        : 'execution stopped (Worker terminated)';
      this.onResult(failure(id, status, message));
    }
    this.reset();
  }
}

function failure(id, status, error) {
  return { id, status, exitCode: 1, stdout: '', diagnostics: [], error };
}
