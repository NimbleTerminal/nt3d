const status = document.getElementById('status');
function fail(error) {
  status.hidden = false;
  status.textContent = `Unable to start the demo: ${error.message || error}. Reload to retry.`;
  console.error(error);
}
async function main() {
  const base = new URL('./', location.href);
  const { BoobaTerminal, installActionBar } = await import(new URL('booba/booba.js', base).href);
  const go = new Go();
  const response = await fetch(new URL('app.wasm', base));
  if (!response.ok) throw new Error(`WASM download returned ${response.status}`);
  const { instance } = await WebAssembly.instantiate(await response.arrayBuffer(), go.importObject);
  go.run(instance).catch(fail);
  // The demo publishes its keys as Terminal Actions (OSC 7501); the bar
  // shows them as touch controls that send the keys back.
  const terminal = new BoobaTerminal('terminal-container', { allowOSC7501Actions: true });
  await terminal.init();
  const bar = document.getElementById('actions');
  if (bar) {
    // The demos read ordinary VT keyboard input.
    terminal.setActionKeyEncoder({
      canEncode: gesture => terminal.encodeKey(gesture) !== null,
      encode: gesture => terminal.encodeKey(gesture),
    });
    // The fit addon skips a fit while it is still settling a resize, as at
    // startup when the bar first appears, so fit again once that has passed.
    const fit = () => terminal.fitAddon?.fit();
    installActionBar(terminal.actions, { parent: bar, openKeyboard: () => terminal.focus(), onLayout: () => { fit(); setTimeout(fit, 150); } });
  }
  terminal.connectWasm();
  terminal.focus();
  status.hidden = true;
}
main().catch(fail);
