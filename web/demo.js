const status = document.getElementById('status');
function fail(error) {
  status.hidden = false;
  status.textContent = `Unable to start the demo: ${error.message || error}. Reload to retry.`;
  console.error(error);
}
async function main() {
  const base = new URL('./', location.href);
  const { BoobaTerminal } = await import(new URL('booba/booba.js', base).href);
  const go = new Go();
  const response = await fetch(new URL('app.wasm', base));
  if (!response.ok) throw new Error(`WASM download returned ${response.status}`);
  const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
  go.run(instance).catch(fail);
  const terminal = new BoobaTerminal('terminal-container');
  await terminal.init();
  terminal.connectWasm();
  terminal.focus();
  status.hidden = true;
}
main().catch(fail);
