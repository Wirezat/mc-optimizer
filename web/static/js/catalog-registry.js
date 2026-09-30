/**
 * Resolves catalog references (mod_id + item_id / fluid_id) to a display name and a texture,
 * and is the single place icon markup is built.
 */
import { apiFetch } from '/static/ui/js/auth.js';
import { esc, t }   from '/static/js/i18n.js';

const _entries  = new Map();  // key → { modID, id, isFluid, name, textureUrl, animation }
const _machines = new Map();  // "mod:machine" → same entry shape
const _tags     = new Map();  // tag name → { total, icons: [entry] }
let   _loaded   = null;       // in-flight or settled load promise

const keyFor = (modID, id, isFluid) => `${isFluid ? 'fluid:' : ''}${modID}:${id}`;

const GLINT_ITEMS = new Set([
    keyFor('minecraft', 'enchanted_golden_apple', false),
    keyFor('minecraft', 'enchanted_book', false),
]);

// Animated textures are vertical strips of frames in one file. These wirezat-ui classes
// present a strip: one holds its first cell, the other plays through them.
const SHEET_STILL_CLASS = 'wui-sheet-first-cell';
const SHEET_PLAY_CLASS  = 'wui-sheet-play';

/* The class and inline custom properties an image needs to show this texture. */
function sheetAttrs(entry) {
    const anim = entry?.animation;
    const frames = Number(anim?.frames);
    const frameMS = Number(anim?.frame_ms);
    // Cells is what the file holds; frames can be fewer. Older payloads carry no cells field.
    const cells = Number(anim?.cells) || frames;
    if (!Number.isFinite(frames) || frames < 2 ||
        !Number.isFinite(frameMS) || frameMS <= 0 ||
        !Number.isFinite(cells) || cells < frames) {
        return { cls: entry?.isFluid ? SHEET_STILL_CLASS : '', style: '' };
    }
    // The animation scrolls to wherever the last played frame sits.
    const end = (frames - 1) / (cells - 1) * 100;
    const style = `--sheet-frames:${frames};--sheet-duration:${frames * frameMS}ms;` +
        `--sheet-end:${end}%;` +
        (anim.ping_pong ? '--sheet-direction:alternate;' : '');
    return { cls: SHEET_PLAY_CLASS, style };
}

/**
 * Fetches the full item and fluid catalog once per page.
 * Input: { saveIDParam }. Output: a promise; repeated calls return the same one.
 */
export function loadCatalog({ saveIDParam = '' } = {}) {
    if (_loaded) return _loaded;

    const fetchJSON = (url, fallback) =>
        apiFetch(url)
            .then(res => (res && res.ok) ? res.json() : fallback)
            .catch(() => fallback);

    _loaded = Promise.all([
        fetchJSON('/api/items?all=true'  + saveIDParam, []),
        fetchJSON('/api/fluids?all=true' + saveIDParam, []),
        fetchJSON('/api/tag-members', {}),
        fetchJSON('/api/machines', []),
    ]).then(([items, fluids, tagMembers, machines]) => {
        for (const it of items ?? []) {
            _entries.set(keyFor(it.mod_id, it.item_id, false), {
                modID:      it.mod_id,
                id:         it.item_id,
                isFluid:    false,
                name:       it.name || it.item_id,
                textureUrl: it.texture_url ?? null,
                animation:  it.animation ?? null,
            });
        }
        for (const fl of fluids ?? []) {
            _entries.set(keyFor(fl.mod_id, fl.fluid_id, true), {
                modID:      fl.mod_id,
                id:         fl.fluid_id,
                isFluid:    true,
                name:       fl.name || fl.fluid_id,
                textureUrl: fl.texture_url ?? null,
                animation:  fl.animation ?? null,
            });
        }
        for (const m of (machines ?? []).flatMap(m => [m, ...(m.variants ?? [])])) {
            _machines.set(`${m.mod_id}:${m.machine_id}`, {
                modID:      m.mod_id,
                id:         m.machine_id,
                isFluid:    false,
                name:       m.name || m.machine_id,
                textureUrl: m.texture_url ?? null,
                animation:  null,
            });
        }
        for (const [name, members] of Object.entries(tagMembers ?? {})) {
            const icons = [];
            for (const m of members) {
                const entry = _entries.get(keyFor(m.mod_id, m.item_id, false));
                if (entry?.textureUrl) icons.push(entry);
            }
            _tags.set(name, { total: members.length, icons });
        }
    });

    return _loaded;
}

/**
 * Looks up a tag.
 * Input: tag name. Output: { total, icons: [entry] } for every member with a texture, or null.
 */
export function lookupTag(name) {
    return _tags.get(name) ?? null;
}

/**
 * Every loaded entry, in no guaranteed order.
 * Output: Array<{ modID, id, isFluid, name, textureUrl }>.
 */
export function catalogEntries() {
    return [..._entries.values()];
}

/* lookupCatalog(modID, id, isFluid) → { name, textureUrl } | null */
export function lookupCatalog(modID, id, isFluid = false) {
    return _entries.get(keyFor(modID, id, isFluid)) ?? null;
}

/* lookupMachine(modID, machineID) → { name, textureUrl } | null */
export function lookupMachine(modID, machineID) {
    return _machines.get(`${modID}:${machineID}`) ?? null;
}

/**
 * Adapts a raw /api/items or /api/fluids row to the shape the icon builders take.
 * Input: the API object and isFluid. Output: an entry.
 */
export function entryOf(row, isFluid = false) {
    if (!row) return null;
    return {
        modID:      row.mod_id,
        id:         isFluid ? row.fluid_id : row.item_id,
        isFluid,
        name:       row.name || (isFluid ? row.fluid_id : row.item_id),
        textureUrl: row.texture_url ?? null,
        animation:  row.animation ?? null,
    };
}

/**
 * The single source of icon markup; a slot supplies only its own class.
 * Input: entry, { cls, placeholder }. Output: HTML string.
 */
export function iconImageHTML(entry, { cls = '', placeholder = true, dataset = null, hidpiPx = null } = {}) {
    const { cls: sheetCls, style } = sheetAttrs(entry);
    const classes = [cls, sheetCls].filter(Boolean).join(' ');
    const classAttr = classes ? ` class="${esc(classes)}"` : '';

    if (!entry?.textureUrl) {
        return placeholder ? `<span${classAttr}></span>` : '';
    }
    // Extra data-* attributes for behaviour a slot opts into. hidpiPx is read by
    // initHiDPIRender (hidpi-render.js).
    const data = Object.entries({ ...(hidpiPx ? { 'hidpi-px': hidpiPx } : {}), ...(dataset ?? {}) })
        .map(([k, v]) => {
            if (!/^[a-z][a-z0-9-]*$/.test(k)) {
                throw new Error(`iconImageHTML: unsafe data attribute name ${JSON.stringify(k)}`);
            }
            return ` data-${k}="${esc(v)}"`;
        }).join('');
    const styleAttr = style ? ` style="${esc(style)}"` : '';

    const img = `<img${classAttr}${styleAttr} src="${esc(entry.textureUrl)}"${data}` +
           ` alt="" loading="lazy" decoding="async" onerror="this.removeAttribute('src')">`;

    if (!GLINT_ITEMS.has(keyFor(entry.modID, entry.id, entry.isFluid))) return img;

    const wrapCls = esc(cls ? `${cls} glint` : 'glint');
    return `<span class="${wrapCls}">${img}</span>`;
}

/**
 * The app's one hover card for wirezat-ui's infocard.js: texture left, name and origin right.
 * Input: entry, { title, subtitle, sections, id, cycle }. Output: HTML string.
 */
export function infocardHTML(entry, { title, subtitle = '', sections = '', id = '', cycle = null } = {}) {
    const icon = iconImageHTML(entry, {
        cls: 'infocard-icon', placeholder: false, hidpiPx: 28,
        dataset: cycle ? { 'wui-cycle': JSON.stringify(cycle) } : null,
    });
    const sub = subtitle ? `<span class="infocard-subtitle">${esc(subtitle)}</span>` : '';
    return `<div${id ? ` id="${esc(id)}"` : ''} class="infocard-def" hidden>
      <div class="infocard-header">
        ${icon}
        <div class="infocard-heading">
          <span class="infocard-title"${cycle ? ' data-wui-cycle-label' : ''}>${esc(title)}</span>
          ${sub}
        </div>
      </div>${sections}
    </div>`;
}

/**
 * Icon plus label, the wirezat-ui icontext component.
 * Input: modID, id, opts. Output: HTML string; falls back to opts.name for an unknown ref.
 */
export function iconTextHTML(modID, id, {
    isFluid = false, name = null, subtitle = null,
    size = null, marquee = false, extraClass = '', hoverCard = false,
} = {}) {
    return entryTextHTML(lookupCatalog(modID, id, isFluid), modID, id,
        { name, subtitle, size, marquee, extraClass, hoverCard });
}

/**
 * The same icontext as iconTextHTML, for a machine.
 * Input: modID, machineID, opts. Output: HTML string.
 */
export function machineIconTextHTML(modID, machineID, opts = {}) {
    return entryTextHTML(lookupMachine(modID, machineID), modID, machineID, opts);
}

function entryTextHTML(entry, modID, id, {
    name = null, subtitle = null,
    size = null, marquee = false, extraClass = '', hoverCard = false,
} = {}) {
    const label = entry?.name ?? name ?? id ?? '';

    const rootCls = esc(['icontext', size ? `icontext-${size}` : '', extraClass]
        .filter(Boolean).join(' '));

    // Matches --icontext-icon-size per size variant (components/icontext.css).
    const hidpiPx = size === 'sm' ? 16 : size === 'lg' ? 28 : 20;
    const icon = iconImageHTML(entry, { cls: 'icontext-icon', hidpiPx });

    const textCls  = marquee ? 'icontext-text cell-clamp cell-clamp--scroll' : 'icontext-text';
    const textBody = marquee
        ? `<span class="cell-clamp-inner">${esc(label)}</span>`
        : esc(label);

    const sub = subtitle
        ? `<span class="icontext-subtitle">${esc(subtitle)}</span>`
        : '';

    const body = `<span class="${rootCls}"${hoverCard ? ' data-infocard-inline' : ''}>` +
           `${icon}<span class="icontext-body">` +
           `<span class="${textCls}">${textBody}</span>${sub}</span></span>`;
    if (!hoverCard) return body;
    return body + infocardHTML(entry, {
        title: label,
        subtitle: subtitle ?? `${modID ?? ''}:${id ?? ''}`,
    });
}

/**
 * Icontext for a tag: the icon cycles through every member with a texture and the subtitle
 * gives the count. Call initIconCycle() and initInfocards() on the container after inserting.
 */
export function tagIconTextHTML(tagRef, { size = null, extraClass = '', chosenRef = null } = {}) {
    const resolved = lookupTag(tagRef);
    const total = resolved?.total ?? 0;

    // chosenRef names the item a chain step resolved this tag to, and leads the cycle.
    const chosenEntry = chosenRef ? lookupCatalog(chosenRef.ModID, chosenRef.ItemID, chosenRef.IsFluid) : null;
    const icons = [...(resolved?.icons ?? [])];
    const chosenAt = chosenEntry ? icons.indexOf(chosenEntry) : -1;
    if (chosenAt > 0) icons.unshift(...icons.splice(chosenAt, 1));
    const first = icons[0] ?? null;
    const frames = icons.length > 1 ? icons.map(i => ({ src: i.textureUrl, label: i.name })) : null;

    const rootCls = esc(['icontext', size ? `icontext-${size}` : '', extraClass]
        .filter(Boolean).join(' '));
    const hidpiPx = size === 'sm' ? 16 : size === 'lg' ? 28 : 20;
    const icon = iconImageHTML(first, {
        cls: 'icontext-icon',
        hidpiPx,
        dataset: frames ? { 'wui-cycle': JSON.stringify(frames.map(f => ({ src: f.src }))) } : null,
    });

    const label = chosenEntry?.name ?? first?.name ?? '#' + tagRef;
    const sub = total === 0 ? ''
        : total === 1 ? t('catalog.recipes.card.tag_members_one')
        : t('catalog.recipes.card.tag_members').replace('{n}', total);

    const body = `<span class="${rootCls}"${first ? ' data-infocard-inline' : ''}>${icon}` +
           `<span class="icontext-body"><span class="icontext-text">${esc(label)}</span>` +
           (sub ? `<span class="icontext-subtitle">${esc(sub)}</span>` : '') +
           `</span></span>`;
    if (!first) return body;
    return body + infocardHTML(first, { title: label, subtitle: '#' + tagRef, cycle: frames });
}

/**
 * The single entry point for an ItemRef-shaped value as the solver returns it.
 * Input: ref ({ ModID, ItemID, TagRef, IsFluid }), opts. Output: HTML string.
 */
export function refIconTextHTML(ref, opts = {}) {
    if (ref?.TagRef) return tagIconTextHTML(ref.TagRef, opts);
    if (opts.resolvedTag) {
        const tagName = opts.resolvedTag.replace(/^#/, '');
        return tagIconTextHTML(tagName, { ...opts, chosenRef: ref });
    }
    // hoverCard: a plain item gets the same card its tag-shaped neighbours do, so every icon in
    // a chain answers the same hover instead of some rows falling back to the browser's own
    // title tooltip.
    return iconTextHTML(ref?.ModID, ref?.ItemID, { ...opts, isFluid: ref?.IsFluid, hoverCard: true });
}
