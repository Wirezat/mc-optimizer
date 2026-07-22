/* Shared mod-selection checklist for the save-scoped "active mods" feature.
   Renders into a container element and reports back which mods are checked;
   the caller decides when/where to persist the selection (PUT .../active-mods).
*/
import { apiFetch } from '/static/ui/js/auth.js';
import { t, esc } from './i18n.js';

// containerEl: element that receives the checklist markup.
// selectedIds: mod_id[] that should start checked. Omitted (undefined) means
// "no preference given" — everything starts checked, for the "new save"
// flow where a fresh world should work immediately. Pass an explicit array
// (possibly empty) to reflect a real stored selection, e.g. in the settings
// modal, where an empty array genuinely means "nothing active yet".
// Returns { getSelected(): string[] } reading the live DOM state.
export async function renderModPicker(containerEl, selectedIds) {
  const res = await apiFetch('/api/mods');
  const mods = res && res.ok ? (await res.json()).filter(m => m.recipe_count > 0) : [];
  const selected = selectedIds ? new Set(selectedIds) : null;
  const isChecked = modID => selected === null || selected.has(modID);

  containerEl.innerHTML = `
    <div class="check-all-row">
      <span>${esc(t('common.select_all'))}</span>
      <input type="checkbox" id="mp-select-all" ${mods.every(m => isChecked(m.mod_id)) ? 'checked' : ''} />
    </div>
    <div class="mod-checklist">
      ${mods.map(m => `
        <label class="check-item mod-check-row">
          <input type="checkbox" value="${esc(m.mod_id)}" ${isChecked(m.mod_id) ? 'checked' : ''} />
          <span class="mod-check-name">${esc(m.name)}</span>
          <span class="mod-check-count">${m.recipe_count}</span>
        </label>
      `).join('')}
    </div>`;

  const boxes = () => [...containerEl.querySelectorAll('.mod-checklist input')];
  containerEl.querySelector('#mp-select-all').addEventListener('change', e => {
    boxes().forEach(b => { b.checked = e.target.checked; });
  });

  return { getSelected: () => boxes().filter(b => b.checked).map(b => b.value) };
}
