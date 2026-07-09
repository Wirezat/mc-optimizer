/* list-view.js — shared tiles/table view-toggle helper for list pages
   (save.html factories list, saves.html saves list, and any future
   analogous list page).

   Wraps the wirezat-ui `container` component's view-picker plus the
   tiles/table rendering + delete-button wiring that both pages need,
   so each page only supplies its per-item tile markup and table
   column definitions instead of re-implementing the toggle/dispatch
   boilerplate.

   Usage:
     import { createListView } from '/static/js/list-view.js'

     const view = createListView({
       containerEl: document.getElementById('factories-container'),
       emptyEl:     document.getElementById('empty-state'),
       storageKey:  'view:save-factories',
       renderTile:  (f) => `<a class="tile" href="/factories/${esc(f.id)}">...</a>`,
       tableCols:   [ { key: 'name', label: t('common.name'), ... }, ... ],
       getId:       (row) => row.id,
       onDelete:    (id, btn) => deleteFactory(id, btn),
     })

     view.render(items)
*/

import { createContainer } from '/static/ui/js/components/container.js';
import { renderTable } from '/static/ui/js/components/table.js';
import { initTileScroll } from '/static/ui/js/tile-scroll.js';
import { t } from '/static/js/i18n.js';

export function createListView({
  containerEl,
  emptyEl,
  storageKey,
  renderTile,
  tableCols,
  getId,
  onDelete,
}) {
  let _items = [];

  const container = createContainer({
    views: [
      { key: 'tiles', icon: '▦', label: t('common.view_tiles') },
      { key: 'table', icon: '☰', label: t('common.view_table') },
    ],
    storageKey,
    onViewChange: (v) => { _view = v; render(_items); },
  });
  let _view = container.activeView;
  containerEl.appendChild(container.el);

  function render(items) {
    _items = items;
    const empty = (!items || items.length === 0);
    container.el.style.display = empty ? 'none' : '';
    emptyEl.style.display = empty ? '' : 'none';
    if (empty) return;

    if (_view === 'table') renderTableView(items);
    else renderTilesView(items);
  }

  function renderTilesView(items) {
    const grid = document.createElement('div');
    grid.className = 'tile-grid';
    grid.innerHTML = items.map(renderTile).join('');

    container.setContent(grid);
    grid.querySelectorAll('.tile-action').forEach(btn => {
      btn.addEventListener('click', e => {
        e.preventDefault();
        onDelete(btn.dataset.id, btn);
      });
    });
    initTileScroll(grid);
  }

  function renderTableView(items) {
    const table = renderTable(document.createElement('div'), {
      cols: tableCols,
      rows: items,
      onAction: (row) => onDelete(getId(row)),
      actionLabel: '🗑',
    });

    container.setContent(table);
  }

  return { render };
}
