import { init as _wuiInit } from '/static/ui/js/header.js';
import { getLang, setLang, t } from './i18n.js';

const NAV_LINKS = () => [
  { section: t('nav.my_work') },
  { href: '/saves', icon: '💾', label: t('nav.saves'), activeFor: ['/saves/', '/factories/', '/production-lines/'] },
  { section: t('nav.catalog') },
  { href: '/catalog/mods',     icon: '🧱', label: t('nav.mods') },
  { href: '/catalog/items',    icon: '📦', label: t('nav.items') },
  { href: '/catalog/fluids',   icon: '💧', label: t('nav.fluids') },
  { href: '/catalog/machines', icon: '🔩', label: t('nav.machines') },
  { href: '/catalog/recipes',  icon: '📋', label: t('nav.recipes') },
  { href: '/catalog/trades',   icon: '🏪', label: t('nav.trades') },
];

const ADMIN_SECTIONS = () => [
  `<a class="dropdown-item" href="/admin/import"><span class="icon">📥</span> ${t('header.admin_import')}</a>`,
  `<a class="dropdown-item" href="/admin/settings"><span class="icon">⚙️</span> ${t('header.admin_settings')}</a>`,
];

const LABELS = () => ({
  personal: t('header.personal'),
  logout:   t('header.logout'),
  admin:    t('header.role_admin'),
  user:     t('header.role_user'),
});

export function init() {
  return _wuiInit({ navLinks: NAV_LINKS(), adminSections: ADMIN_SECTIONS(), labels: LABELS(), getLang, setLang });
}
