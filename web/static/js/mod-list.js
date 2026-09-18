/* mod-list.js
   The installed-mod table, grouped by the ecosystem its machines declare.
   Shared so the admin Mods page and the catalog show the same list; only the
   delete column differs.

   renderModList(container, mods, { deletable, onDelete })
     mods: /api/mods rows. Call applyI18n() on the container afterwards.
*/

import { esc, t } from '/static/js/i18n.js';
import { initCollapsibleGroups } from '/static/ui/js/table-features.js';

const LINKS = [
    { field: 'url_source',   key: 'catalog.mods.detail.source' },
    { field: 'url_modrinth', key: 'catalog.mods.detail.modrinth' },
    { field: 'url_wiki',     key: 'catalog.mods.detail.wiki' },
    { field: 'url_issues',   key: 'catalog.mods.detail.issues' },
    { field: 'url_discord',  key: 'catalog.mods.detail.discord' },
];

/** groupByEcosystem(mods) → [[ecosystem, mods], …], a mod repeated per ecosystem it serves.
    Mods without machines are left out: they are in the catalog only because
    something else references them. */
function groupByEcosystem(mods) {
    const groups = new Map();
    for (const m of mods) {
        for (const key of m.ecosystems ?? []) {
            if (!groups.has(key)) groups.set(key, []);
            groups.get(key).push(m);
        }
    }
    return [...groups.entries()].sort((a, b) => a[0].localeCompare(b[0]));
}

function bodyHTML(m) {
    const desc = m.description
        ? `<p class="mod-description">${esc(m.description)}</p>`
        : `<p class="mod-description" style="color:var(--text-faint)">${esc(t('catalog.mods.detail.no_description'))}</p>`;

    const links = LINKS.filter(d => m[d.field]).map(d =>
        `<a href="${esc(m[d.field])}" class="btn btn-sm btn-ghost" target="_blank" rel="noopener">${esc(t(d.key))} ↗</a>`).join('');

    const meta = [
        m.author  ? ['catalog.mods.detail.author',  m.author]  : null,
        m.license ? ['catalog.mods.detail.license', m.license] : null,
    ].filter(Boolean).map(([key, val]) =>
        `<div class="detail-row"><span class="detail-key">${esc(t(key))}</span>` +
        `<span class="detail-val">${esc(val)}</span></div>`).join('');

    const badge = (path, key, n) =>
        `<a href="${esc(path)}" class="badge badge-neutral">${esc(t(key).replace('{n}', n))}</a>`;
    const id = encodeURIComponent(m.mod_id);

    return `<div class="mod-detail-layout">
        <div>${desc}${links ? `<div class="mod-ext-links">${links}</div>` : ''}</div>
        ${meta ? `<div class="detail-grid">${meta}</div>` : ''}
      </div>
      <div class="mod-catalog-badges">
        ${badge(`/catalog/items?mod=${id}`, 'catalog.mods.detail.items', m.item_count)}
        <a href="/catalog/machines?mod=${id}" class="badge badge-neutral">${esc(t('catalog.mods.expand.machines'))} →</a>
        ${badge(`/catalog/items?producedByMod=${id}`, 'catalog.mods.detail.recipes', m.recipe_count)}
      </div>`;
}

function rowsHTML(m, rowID, deletable) {
    const del = deletable
        ? `<td><button class="btn btn-icon btn-sm btn-icon-color" style="--_icon-color:var(--danger)"
             data-mod-delete="${esc(m.mod_id)}" title="${esc(t('common.delete'))}">✕</button></td>`
        : '';
    const recipes = m.recipe_count
        ? `<a class="recipe-count-link metric" href="/catalog/items?producedByMod=${encodeURIComponent(m.mod_id)}">${m.recipe_count}</a>`
        : '<span class="td-muted">—</span>';

    return `<tr class="mod-row" data-expand="${esc(rowID)}">
        <td><span class="table-expand-trigger">▸</span></td>
        <td>${esc(m.name || m.mod_id)}</td>
        <td class="td-mono td-muted">${esc(m.mod_id)}</td>
        <td class="td-num"><span class="metric">${m.item_count}</span></td>
        <td class="td-num">${recipes}</td>
        ${del}
      </tr>
      <tr class="table-expand-row" id="${esc(rowID)}">
        <td colspan="${deletable ? 6 : 5}"><div class="table-expand-body">${bodyHTML(m)}</div></td>
      </tr>`;
}

export function renderModList(container, mods, { deletable = false, onDelete = null } = {}) {
    if (!mods.length) {
        container.innerHTML = `<div class="empty-state">${esc(t('catalog.empty.no_mods'))}</div>`;
        return;
    }

    const groups = groupByEcosystem(mods);
    const cols = deletable ? 6 : 5;
    const body = groups.map(([ecosystem, groupMods]) => {
        const label = `<span class="td-mono">${esc(ecosystem)}</span>`;
        const head = `<tr class="table-group-hd collapsible">
            <td colspan="${cols}"><span class="group-hd-inner"><span class="group-hd-arrow"></span>
              ${label}<span class="td-muted">${groupMods.length}</span></span></td>
          </tr>`;
        return head + groupMods.map(m => rowsHTML(m, `mod-${ecosystem}-${m.mod_id}`, deletable)).join('');
    }).join('');

    container.innerHTML = `<table class="table">
        <thead><tr>
          <th style="width:24px"></th>
          <th data-i18n="catalog.mods.col.name"></th>
          <th data-i18n="catalog.mods.col.mod_id"></th>
          <th class="td-num" data-i18n="catalog.mods.col.items"></th>
          <th class="td-num" data-i18n="catalog.mods.col.recipes"></th>
          ${deletable ? '<th style="width:1%"></th>' : ''}
        </tr></thead>
        <tbody>${body}</tbody>
      </table>`;

    const table = container.querySelector('table');
    initCollapsibleGroups(table);

    table.addEventListener('click', e => {
        const delBtn = e.target.closest('[data-mod-delete]');
        if (delBtn) {
            e.stopPropagation();
            onDelete?.(delBtn.dataset.modDelete);
            return;
        }
        const row = e.target.closest('tr.mod-row');
        if (!row) return;
        const open = document.getElementById(row.dataset.expand)?.classList.toggle('open');
        row.querySelector('.table-expand-trigger')?.classList.toggle('open', open);
    });
}
