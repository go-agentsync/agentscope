// Boots app.wasm with a real download progress bar.
(async () => {
  const el = (id) => document.getElementById(id);
  const fail = (msg) => {
    el("loading").hidden = true;
    el("err").textContent = "Failed to start agentscope: " + msg;
  };
  const progress = (frac) => {
    const track = el("ptrack"), bar = el("pbar");
    if (frac == null) { track.classList.add("indeterminate"); return; }
    track.classList.remove("indeterminate");
    bar.style.width = Math.max(0, Math.min(1, frac)) * 100 + "%";
  };
  if (typeof WebAssembly !== "object") { fail("WebAssembly is not supported by this browser."); return; }
  try {
    const resp = await fetch("app.wasm");
    if (!resp.ok) throw new Error("HTTP " + resp.status);
    const total = +resp.headers.get("content-length") || 0;
    const reader = resp.body && resp.body.getReader ? resp.body.getReader() : null;
    let bytes;
    if (reader) {
      const chunks = []; let received = 0;
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value); received += value.length;
        progress(total ? received / total : null);
      }
      bytes = new Uint8Array(received); let off = 0;
      for (const c of chunks) { bytes.set(c, off); off += c.length; }
    } else {
      progress(null);
      bytes = new Uint8Array(await resp.arrayBuffer());
    }
    el("plabel").textContent = "Starting...";
    const go = new Go();
    const res = await WebAssembly.instantiate(bytes, go.importObject);
    el("loading").hidden = true;
    el("screen").hidden = false;
    go.run(res.instance);
  } catch (e) {
    fail(e && e.message ? e.message : String(e));
  }
})();
