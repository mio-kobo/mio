/* 经典 Worker 宿主：为单次请求运行独立的 Go/Wasm（Web 参考 VM）实例。
   由 Go 配套的 wasm_exec.js 提供 js/wasm ABI；Mio VM 本身不向 JS 暴露桥接，
   所有宿主 MMA 函数均为安全失败的桩实现，用户代码仅可输出 stdout。 */
'use strict';
let execute = null;

function toBase64(bytes) {
  let binary = '';
  for (let i = 0; i < bytes.length; i += 0x8000) binary += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
  return btoa(binary);
}

function fromBase64(text) {
  const binary = atob(text);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
  return bytes;
}

// Defense in depth: after start-up nothing in this Worker needs network or script loading.
function closeCapabilities() {
  for (const name of ['fetch', 'XMLHttpRequest', 'WebSocket', 'EventSource', 'importScripts', 'indexedDB', 'caches', 'BroadcastChannel']) {
    try { Object.defineProperty(self, name, { value: undefined, configurable: false, writable: false }); } catch (_) { /* ignore */ }
  }
}

self.onmessage = async ({ data }) => {
  const { id, type } = data;
  try {
    if (type === 'init') {
      if (execute) throw new Error('Worker is already initialized');
      const start = performance.now();
      importScripts('./wasm_exec.js');
      const go = new Go();
      const instance = await WebAssembly.instantiate(data.module, go.importObject);
      go.run(instance).then(
        () => self.postMessage({ type: 'fatal', id, error: 'Go runtime exited' }),
        error => self.postMessage({ type: 'fatal', id, error: String(error) }));
      execute = self.mioExecute;
      if (typeof execute !== 'function') throw new Error('Mio bridge unavailable');
      delete self.mioExecute;
      closeCapabilities();
      self.postMessage({ type: 'ready', id, initMs: performance.now() - start });
    } else if (type === 'request') {
      if (!execute) throw new Error('Worker is not initialized');
      const request = { ...data.request };
      if (request.mochi) request.mochi = toBase64(request.mochi);
      const start = performance.now();
      const answer = await new Promise((resolve, reject) => {
        const refusal = execute(JSON.stringify(request), resolve);
        if (refusal) reject(new Error(refusal));
      });
      const result = JSON.parse(answer);
      if (result.mochi) result.mochi = fromBase64(result.mochi);
      self.postMessage({ type: 'result', id, result, elapsedMs: performance.now() - start });
    } else {
      throw new Error('Unsupported Worker message');
    }
  } catch (error) {
    self.postMessage({ type: 'fatal', id, error: String(error) });
  }
};
