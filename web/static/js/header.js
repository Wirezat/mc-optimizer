import { init as _wuiInit } from '/static/ui/js/header.js';
import { getLang, setLang, t } from './i18n.js';

// The asset server's own item paths — flat sprites straight from a mod's
// textures, blocks through the renderer. A missing one clears its src and
// leaves the empty box an unknown item's icon gets.
const sprite = (path) =>
  `<img src="/assets/${path}.png" alt="" loading="lazy" decoding="async" onerror="this.removeAttribute('src')">`;

const NAV_LINKS = () => [
  { section: t('nav.my_work') },
  { href: '/saves', icon: sprite('render/minecraft/item/lectern'), label: t('nav.saves'), activeFor: ['/saves/', '/factories/', '/production-lines/'] },
  { section: t('nav.catalog') },
  { href: '/catalog/mods',     icon: sprite('render/minecraft/item/bookshelf'),     label: t('nav.mods') },
  { href: '/catalog/items',    icon: sprite('render/minecraft/item/barrel'),        label: t('nav.items') },
  { href: '/catalog/fluids',   icon: sprite('minecraft/textures/item/water_bucket'), label: t('nav.fluids') },
  { href: '/catalog/machines', icon: sprite('render/minecraft/item/furnace'),       label: t('nav.machines') },
  { href: '/catalog/trades',   icon: sprite('minecraft/textures/item/emerald'),     label: t('nav.trades') },
];

const ADMIN_SECTIONS = () => [
  `<a class="dropdown-item" href="/admin/mods"><span class="icon">📥</span> ${t('header.admin_mods')}</a>`,
  `<a class="dropdown-item" href="/admin/settings"><span class="icon">⚙️</span> ${t('header.admin_settings')}</a>`,
];

const LABELS = () => ({
  personal: t('header.personal'),
  logout:   t('header.logout'),
  admin:    t('header.role_admin'),
  user:     t('header.role_user'),
  demo:     t('header.demo'),
});

export function init({ publicPage = false } = {}) {
  return _wuiInit({ navLinks: NAV_LINKS(), adminSections: ADMIN_SECTIONS(), labels: LABELS(), getLang, setLang, publicPage });
}
