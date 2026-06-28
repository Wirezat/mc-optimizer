export * from '/static/ui/js/i18n.js';

export function esc(s) {
  return String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
}

let _mi = {};

export async function loadMI(lang) {
  const base = '/static/locales/';
  const res  = await fetch(base + 'mi_' + lang + '.json').catch(() => null);
  _mi = (res && res.ok) ? await res.json() : {};
}

export function item(modId, itemId) {
  for (const k of [`item.${modId}.${itemId}`, `block.${modId}.${itemId}`, `fluid.${modId}.${itemId}`]) {
    if (_mi[k]) return _mi[k];
  }
  return String(itemId).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}

export function machine(modId, machineId) {
  const k = `machine.${modId}.${machineId}`;
  if (_mi[k]) return _mi[k];
  const bk = `block.${modId}.${machineId}`;
  if (_mi[bk]) return _mi[bk];
  return String(machineId).replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase());
}
