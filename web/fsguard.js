// Defensive Go-wasm fs guard: wasm_exec.js installs its fallback only when
// globalThis.fs is absent. Element ids in index.html avoid "fs"/"process"/"path".
(() => {
  const g = globalThis;
  if (!g.fs || typeof g.fs.writeSync !== "function") {
    let buf = ""; const dec = new TextDecoder("utf-8");
    g.fs = {
      constants: { O_WRONLY: -1, O_RDWR: -1, O_CREAT: -1, O_TRUNC: -1, O_APPEND: -1, O_EXCL: -1, O_DIRECTORY: -1 },
      writeSync(fd, b) { buf += dec.decode(b); const nl = buf.lastIndexOf("\n"); if (nl !== -1) { console.log(buf.substring(0, nl)); buf = buf.substring(nl + 1); } return b.length; },
      write(fd, b, off, len, pos, cb) { const n = this.writeSync(fd, b.subarray(off, off + len)); cb(null, n); },
    };
  }
})();
