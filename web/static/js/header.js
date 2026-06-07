/* header.js — shared header with user dropdown */
const Header = (() => {
  let _user = null;

  async function init() {
    _buildSidebar();
    try {
      _user = await Auth.getUser();
      if (!_user) return;
    } catch (_) { return; }

    _renderTrigger();
    _buildDropdown();
    _wireDropdown();
    Auth.applyRoles();
  }

  function _buildSidebar() {
    const nav = document.querySelector('nav.sidebar');
    if (!nav) return;
    const p = window.location.pathname;
    const active = href => {
      if (href === '/saves')
        return p === '/saves' || p.startsWith('/saves/') || p.startsWith('/factories/') || p.startsWith('/production-lines/');
      return p === href;
    };
    const link = (href, icon, label) =>
      `<a class="nav-link${active(href) ? ' active' : ''}" href="${href}"><i class="nav-icon">${icon}</i> ${label}</a>`;
    nav.innerHTML =
      `<span class="nav-section">My Work</span>` +
      link('/saves', '💾', 'Saves') +
      `<span class="nav-section">Catalog</span>` +
      link('/catalog/items', '📦', 'Items &amp; Fluids') +
      link('/catalog/recipes', '📋', 'Recipes') +
      link('/catalog/mods', '🔧', 'Mods');
  }

  function _initial(name) {
    return (name || '?').charAt(0).toUpperCase();
  }

  function _renderTrigger() {
    const trigger = document.getElementById('user-menu-trigger');
    if (!trigger) return;
    trigger.querySelector('.avatar').textContent = _initial(_user.username);
    trigger.querySelector('.username').textContent = _user.username;
  }

  function _buildDropdown() {
    const dropdown = document.getElementById('user-dropdown');
    if (!dropdown) return;

    const sections = [];

    sections.push(`
      <div class="user-dropdown-section">
        <a class="dropdown-item" href="/personal">
          <span class="icon">👤</span> Personal
        </a>
      </div>`);

    if (_user.is_admin) {
      sections.push(`
        <div class="user-dropdown-section">
          <a class="dropdown-item" href="/catalog/mods">
            <span class="icon">🗂️</span> Mod-Katalog
          </a>
          <a class="dropdown-item" href="/catalog/recipes">
            <span class="icon">📋</span> Rezept-Katalog
          </a>
        </div>`);
    }

    sections.push(`
      <div class="user-dropdown-section">
        <button class="dropdown-item danger" id="logout-btn">
          <span class="icon">🚪</span> Abmelden
        </button>
      </div>`);

    dropdown.innerHTML = `
      <div class="user-dropdown-header">
        <div class="user-dropdown-name">${_esc(_user.username)}</div>
        <div class="user-dropdown-role">${_user.is_admin ? 'Administrator' : 'Benutzer'}</div>
      </div>
      ${sections.join('')}`;

    document.getElementById('logout-btn')?.addEventListener('click', () => Auth.logout());
  }

  function _wireDropdown() {
    const menu = document.getElementById('user-menu');
    if (!menu) return;

    document.getElementById('user-menu-trigger')?.addEventListener('click', e => {
      e.stopPropagation();
      if (menu.hasAttribute('data-open')) {
        _closeDropdown();
      } else {
        menu.setAttribute('data-open', '');
      }
    });

    document.addEventListener('click', e => {
      if (!menu.contains(e.target)) _closeDropdown();
    });

    document.addEventListener('keydown', e => {
      if (e.key === 'Escape') _closeDropdown();
    });
  }

  function _closeDropdown() {
    document.getElementById('user-menu')?.removeAttribute('data-open');
  }

  function _esc(str) {
    return str.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
  }

  return { init };
})();
