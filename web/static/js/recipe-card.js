/* recipe-card.js
   Renders a recipe's crafting-grid card from the recipe-card JSON shape
   GET /api/items/{mod}/{item}/recipes and GET /api/fluids/{mod}/{fluid}/recipes
   return (see internal/recipecard.Card for the exact shape).
   Used by catalog-items.html and catalog-fluids.html's side panel.
*/
import { t, esc } from '/static/js/i18n.js';
import { lookupCatalog, lookupTag, iconImageHTML } from '/static/js/catalog-registry.js';

const TAG_CARD_ICONS = 12;
const TAG_CARD_COLS = 6;

function cellParts(modID, id, isFluid) {
  const entry = lookupCatalog(modID, id, isFluid);
  const name = entry?.name || id || '?';
  return {
    entry,
    name,
    label: entry?.textureUrl
      ? iconImageHTML(entry, { cls: 'crafting-cell-icon', hidpiPx: 36 })
      : `<span class="crafting-cell-text">${esc(name)}</span>`,
  };
}

// The member grid, using the infocard's own grid slot.
function tagCardGrid(icons, total) {
  if (icons.length < 2) return '';
  const shown = icons.slice(0, TAG_CARD_ICONS);
  const cells = shown.map(e =>
    `<div class="infocard-grid-cell" data-label="${esc(e.name)}">` +
    iconImageHTML(e, { placeholder: false, hidpiPx: 36 }) + '</div>').join('');
  const more = total > shown.length
    ? `<div class="infocard-section td-muted">+${total - shown.length}</div>`
    : '';
  return `<div class="infocard-grid" data-cols="${TAG_CARD_COLS}">${cells}</div>${more}`;
}

// A tag slot accepts any of its members, so showing one of them would claim
// the recipe needs that particular item. It cycles instead, the way JEI
// does, and the hover card lays the whole set out at once.
function tagCell(tagName) {
  const resolved = lookupTag(tagName);
  const icons = resolved?.icons ?? [];
  const total = resolved?.total ?? 0;
  const first = icons[0] ?? null;
  return {
    name: '#' + tagName,
    entry: first,
    ref: total === 0 ? ''
       : total === 1 ? t('catalog.recipes.card.tag_members_one')
       : t('catalog.recipes.card.tag_members').replace('{n}', total),
    label: first
      ? iconImageHTML(first, {
          cls: 'crafting-cell-icon',
          hidpiPx: 36,
          dataset: icons.length > 1
            ? { 'wui-cycle': JSON.stringify(icons.map(i => ({ src: i.textureUrl }))) }
            : null,
        })
      : `<span class="crafting-cell-text">#${esc(tagName)}</span>`,
    extra: tagCardGrid(icons, total),
  };
}

// count is the bare number drawn in the corner; amount carries the unit and
// stays in the hover card, where there is room for it.
function craftCell({ label, name, ref = '', entry = null,
                     count = '', amount = '', nonConsuming = false, extra = '', style = '' }) {
  const countMark = count ? `<span class="crafting-cell-count">${esc(count)}</span>` : '';
  const ncMark = nonConsuming ? `<span class="nc-badge">↺</span>` : '';
  const icon = iconImageHTML(entry, { cls: 'infocard-icon', placeholder: false, hidpiPx: 28 });
  const amtRow = amount
    ? `<div class="infocard-section"><div class="infocard-row">` +
      `<span class="infocard-key">${esc(t('catalog.recipes.card.amount'))}</span>` +
      `<span class="infocard-val">${esc(amount)}</span></div></div>`
    : '';
  const ncRow = nonConsuming
    ? `<div class="infocard-section">${esc(t('recipe.non_consuming_title'))}</div>`
    : '';
  const sub = ref ? `<span class="infocard-subtitle">${esc(ref)}</span>` : '';
  const styleAttr = style ? ` style="${esc(style)}"` : '';
  return `<div class="crafting-cell${nonConsuming ? ' crafting-cell--nc' : ''}"${styleAttr} data-infocard-inline>${label}${countMark}${ncMark}</div>
    <div class="infocard-def" hidden>
      <div class="infocard-header">${icon}
        <div class="infocard-heading">
          <span class="infocard-title">${esc(name)}</span>
          ${sub}
        </div>
      </div>${amtRow}${ncRow}${extra}
    </div>`;
}

// One recipe-card I/O entry ({item_mod_id,item_id}, {fluid_mod_id,fluid_id}
// or {tag_name}, plus amount/x/y) → its cell's inline style + craftCell HTML.
function ioCellHTML(io, style) {
  const isFluid = io.fluid_id != null;
  const count = isFluid ? (io.amount_mb ? String(io.amount_mb) : '')
    : (io.amount > 1 ? String(io.amount) : '');
  const amount = isFluid ? (io.amount_mb ? io.amount_mb + 'mB' : '')
    : (io.amount > 1 ? io.amount + '×' : '');
  if (io.tag_name) {
    return craftCell({ ...tagCell(io.tag_name), count, amount, nonConsuming: io.non_consuming, style });
  }
  const ref = isFluid
    ? (io.fluid_mod_id + ':' + io.fluid_id)
    : ((io.item_mod_id || '') + ':' + (io.item_id || ''));
  const parts = isFluid
    ? cellParts(io.fluid_mod_id, io.fluid_id, true)
    : cellParts(io.item_mod_id, io.item_id, false);
  return craftCell({ ...parts, ref, count, amount, nonConsuming: io.non_consuming, style });
}

// items: recipe-card "inputs"/"outputs"/"fluid_inputs"/"fluid_outputs" arrays
// (or a concatenation of an item + fluid array, for one combined grid).
function ioGrid(items) {
  if (!items.length) return `<div class="crafting-cell empty" style="width:36px;height:36px;"></div>`;

  if (items.every(io => io.x != null && io.y != null)) {
    const xs = items.map(io => io.x), ys = items.map(io => io.y);
    const minX = Math.min(...xs), minY = Math.min(...ys);
    const cols = Math.max(...xs) - minX + 1;
    const rows = Math.max(...ys) - minY + 1;
    const style = `grid-template-columns:repeat(${cols},36px);grid-template-rows:repeat(${rows},36px);`;
    const cells = items.map(io =>
      ioCellHTML(io, `grid-column:${io.x - minX + 1};grid-row:${io.y - minY + 1};`)).join('');
    return `<div class="crafting-grid" style="${style}">${cells}</div>`;
  }

  const cols = Math.max(1, Math.ceil(Math.sqrt(items.length)));
  const rows = Math.ceil(items.length / cols);
  const total = cols * rows;
  const style = `grid-template-columns:repeat(${cols},36px);grid-template-rows:repeat(${rows},36px);`;
  let cells = '';
  for (let i = 0; i < total; i++) {
    cells += i >= items.length ? `<div class="crafting-cell empty"></div>` : ioCellHTML(items[i]);
  }
  return `<div class="crafting-grid" style="${style}">${cells}</div>`;
}

function fmt0(v, suffix) {
  return (v == null || v === 0) ? '—' : (v + (suffix || ''));
}

/**
 * recipeCardHTML(recipe, machineName) → HTML string
 * recipe: one object from the recipe-card API shape.
 * machineName: pre-resolved display name for recipe.machine_id (callers
 * build this from GET /api/machines — this module has no way to resolve it
 * itself, see Task 4/5's machineNames map), or omitted to fall back to the
 * raw machine_id.
 */
export function recipeCardHTML(recipe, machineName) {
  const machineUrl = `/catalog/machines?mod=${encodeURIComponent(recipe.machine_mod_id)}&machine=${encodeURIComponent(recipe.machine_id)}`;
  const inputs = [...recipe.inputs, ...recipe.fluid_inputs];
  const outputs = [...recipe.outputs, ...recipe.fluid_outputs];
  return `<div class="recipe-card">
    <div class="recipe-card-meta">
      <a href="${esc(machineUrl)}" class="catalog-link">${esc(machineName || recipe.machine_id)}</a>
      <span class="td-muted">${fmt0(recipe.duration_ticks, 't')} · ${fmt0(recipe.eu_per_tick)} EU/t</span>
    </div>
    <div class="crafting-layout">
      ${ioGrid(inputs)}
      <div class="crafting-arrow">→</div>
      ${ioGrid(outputs)}
    </div>
  </div>`;
}

/**
 * recipeListHTML(recipes, machineNameFor) → HTML string
 * recipes: array from GET /api/items/{mod}/{item}/recipes (or the fluid
 * equivalent). machineNameFor(recipe): optional function returning a
 * display name for one recipe's machine (see recipeCardHTML).
 */
export function recipeListHTML(recipes, machineNameFor) {
  if (!recipes.length) return `<span class="td-muted" data-i18n="catalog.empty.no_recipes"></span>`;
  return recipes.map(r => recipeCardHTML(r, machineNameFor ? machineNameFor(r) : undefined)).join('');
}
