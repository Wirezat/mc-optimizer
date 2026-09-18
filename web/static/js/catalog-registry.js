/* catalog-registry.js
   Resolves catalog references (mod_id + item_id / fluid_id) to a display name
   and a texture, and is the single place icon markup is built.

   This is the app's domain layer: the wirezat-ui components it composes know
   nothing about items, fluids or mods, and everything item-specific lives here.

   Anything that shows a catalog texture calls iconImageHTML — a table cell, an
   infocard header, a 36px crafting square. A flat sprite, an animation strip to
   play, a sheet to hold still and a model the server renders on request all
   look the same to the caller, which is what keeps a new slot from having to
   reimplement a subset of that and drift.
*/

import { apiFetch } from '/static/ui/js/auth.js';
import { esc, t }   from '/static/js/i18n.js';

const _entries  = new Map();  // key → { modID, id, isFluid, name, textureUrl, animation }
const _machines = new Map();  // "mod:machine" → same entry shape
const _tags     = new Map();  // tag name → { total, icons: [entry] }
let   _loaded   = null;       // in-flight or settled load promise

const keyFor = (modID, id, isFluid) => `${isFluid ? 'fluid:' : ''}${modID}:${id}`;

// Items with a baked-in enchantment glint regardless of actual enchantments —
// vanilla hardcodes this per-item rather than exposing it as data, so it's a
// fixed list here rather than something read off the catalog.
const GLINT_ITEMS = new Set([
    keyFor('minecraft', 'enchanted_golden_apple', false),
    keyFor('minecraft', 'enchanted_book', false),
]);

// Animated textures are vertical strips of frames in one file — fluids above
// all. These wirezat-ui classes present a strip: one holds its first cell, the
// other plays through them. Deliberately not exported: iconImageHTML is the one
// way to build an icon, so a new slot cannot grow its own handling and drift.
const SHEET_STILL_CLASS = 'wui-sheet-first-cell';
const SHEET_PLAY_CLASS  = 'wui-sheet-play';

/** The class and inline custom properties an image needs to show this texture. */
function sheetAttrs(entry) {
    // Numbers, not just truthy: these land in a style attribute as CSS custom
    // properties, where escaping would stop a value leaving the attribute but
    // not leaving the declaration. Anything that is not a usable number falls
    // through to the still frame rather than emitting a broken animation.
    const anim = entry?.animation;
    const frames = Number(anim?.frames);
    const frameMS = Number(anim?.frame_ms);
    // Cells is what the file holds; frames can be fewer, since a ping-pong
    // order plays only the way up. Older payloads carry no cells field.
    const cells = Number(anim?.cells) || frames;
    if (!Number.isFinite(frames) || frames < 2 ||
        !Number.isFinite(frameMS) || frameMS <= 0 ||
        !Number.isFinite(cells) || cells < frames) {
        // A strip with no usable animation data would still be squeezed whole
        // into the box, so hold its first cell.
        return { cls: entry?.isFluid ? SHEET_STILL_CLASS : '', style: '' };
    }
    // The animation scrolls to wherever the last played frame sits, which is
    // the end of the strip only when every cell is played. Stepping to 100%
    // over fewer frames than the file holds lands between cells and shows two
    // at once.
    const end = (frames - 1) / (cells - 1) * 100;
    const style = `--sheet-frames:${frames};--sheet-duration:${frames * frameMS}ms;` +
        `--sheet-end:${end}%;` +
        (anim.ping_pong ? '--sheet-direction:alternate;' : '');
    return { cls: SHEET_PLAY_CLASS, style };
}

/**
 * loadCatalog({ saveIDParam })
 * Fetches the full item and fluid catalog once per page. Repeated calls return
 * the same promise. A failed fetch resolves rather than rejecting — callers
 * must still render, just without icons.
 */
export function loadCatalog({ saveIDParam = '' } = {}) {
    if (_loaded) return _loaded;

    // apiFetch resolves with the raw Response (and with null when the session
    // was dropped), so the body has to be parsed here. Anything other than a
    // readable 2xx body degrades to an empty list: no icons, but a live page.
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
        // Their own index, not items — see machineIconTextHTML.
        for (const m of machines ?? []) {
            _machines.set(`${m.mod_id}:${m.machine_id}`, {
                modID:      m.mod_id,
                id:         m.machine_id,
                isFluid:    false,
                name:       m.name || m.machine_id,
                textureUrl: m.texture_url ?? null,
                animation:  null,
            });
        }
        // Tags resolve after the items, since a member is only useful once its
        // texture is known. `total` counts every member so a tag can still say
        // how many items it stands for even where none of them has an icon.
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
 * lookupTag(name) → { total, icons: [entry] } | null
 * A tag stands for any of its members, so callers get every member that has a
 * texture and decide how to present them — cycling, a grid, or just the first.
 */
export function lookupTag(name) {
    return _tags.get(name) ?? null;
}

/**
 * catalogEntries() → Array<{ modID, id, isFluid, name, textureUrl }>
 * Every loaded entry, so callers that need to build their own index (a reverse
 * name lookup, a picker list) can do it without fetching the catalog a second
 * time. Callers impose their own ordering — nothing here guarantees one.
 */
export function catalogEntries() {
    return [..._entries.values()];
}

/** lookupCatalog(modID, id, isFluid) → { name, textureUrl } | null */
export function lookupCatalog(modID, id, isFluid = false) {
    return _entries.get(keyFor(modID, id, isFluid)) ?? null;
}

/** lookupMachine(modID, machineID) → { name, textureUrl } | null */
export function lookupMachine(modID, machineID) {
    return _machines.get(`${modID}:${machineID}`) ?? null;
}

/**
 * entryOf(apiObject, isFluid) → entry
 * Adapts a raw /api/items or /api/fluids row to the shape the icon builders
 * take, for callers that fetch their own paginated list instead of going
 * through loadCatalog. Keeps those pages off the field names.
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
 * iconImageHTML(entry, { cls, placeholder }) → HTML string
 *
 * The single source of icon markup. Every slot that shows a catalog texture —
 * a table cell, an infocard header, a 36px crafting square — goes through here
 * and supplies only the class of the slot it is filling. Whether the texture is
 * a flat sprite, a sheet to hold still, an animation to play, or a model the
 * server renders on request is decided once, here, so a new slot inherits all
 * of it instead of reimplementing a subset.
 *
 * placeholder keeps the slot's box when there is no texture, so labels stay
 * aligned in a list where only some rows have an icon; pass false where an
 * absent icon should take no space, as in an infocard header.
 */
export function iconImageHTML(entry, { cls = '', placeholder = true, dataset = null, hidpiPx = null } = {}) {
    const { cls: sheetCls, style } = sheetAttrs(entry);
    const classes = [cls, sheetCls].filter(Boolean).join(' ');
    const classAttr = classes ? ` class="${esc(classes)}"` : '';

    if (!entry?.textureUrl) {
        return placeholder ? `<span${classAttr}></span>` : '';
    }
    // Extra data-* attributes, for behaviour a slot opts into — a tag cell
    // cycling through its members, say. Values are escaped here so a caller
    // never has to think about it. Names are checked rather than escaped:
    // esc() leaves spaces alone, so a name carrying one would end the attribute
    // and start another. hidpiPx rides along the same way, read by
    // initHiDPIRender (hidpi-render.js); a no-op on a flat texture.
    const data = Object.entries({ ...(hidpiPx ? { 'hidpi-px': hidpiPx } : {}), ...(dataset ?? {}) })
        .map(([k, v]) => {
            if (!/^[a-z][a-z0-9-]*$/.test(k)) {
                throw new Error(`iconImageHTML: unsafe data attribute name ${JSON.stringify(k)}`);
            }
            return ` data-${k}="${esc(v)}"`;
        }).join('');
    const styleAttr = style ? ` style="${esc(style)}"` : '';

    // onerror clears the src rather than hiding the element, so a texture that
    // vanished between catalog load and render degrades to the same empty box
    // an unknown one gets.
    const img = `<img${classAttr}${styleAttr} src="${esc(entry.textureUrl)}"${data}` +
           ` alt="" loading="lazy" decoding="async" onerror="this.removeAttribute('src')">`;

    if (!GLINT_ITEMS.has(keyFor(entry.modID, entry.id, entry.isFluid))) return img;

    // wui-ui's .glint is a plain sweep overlay with no size opinion of its
    // own — sharing the slot's own class on the wrapper (rather than
    // inventing a new one) makes it inherit that slot's sizing exactly,
    // fixed-px or percentage-of-parent alike, with no separate case needed.
    // Only cls, not sheetCls: object-fit/object-position are img-only and
    // meaningless on the wrapper.
    const wrapCls = esc(cls ? `${cls} glint` : 'glint');
    return `<span class="${wrapCls}">${img}</span>`;
}

/**
 * infocardHTML(entry, { title, subtitle, sections, id, cycle }) → HTML string
 * The app's one hover card, for wirezat-ui's infocard.js: the texture on the
 * left, next to it the name above and where it comes from below — a mod, an
 * id, or the tag that put an item in a slot. Every page that shows a catalog
 * texture hands out this same card; extra content a page has to add (an id
 * row, a rate table, a crafting card's amount) goes in through sections.
 *
 * Returns the hidden .infocard-def that infocard.js clones on hover. Place it
 * immediately after its data-infocard-inline trigger, or give it an id and
 * point a data-infocard trigger at it. Call initInfocards() on the container
 * after inserting.
 *
 * cycle: frames ({ src, label }) for a card that stands for several items —
 * the icon steps through them (icon-cycle.js, call initIconCycle() too) and
 * the title follows, naming whichever member is showing.
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
 * iconTextHTML(modID, id, opts) → HTML string
 * Icon plus label, the wirezat-ui icontext component. Falls back to opts.name
 * when the reference is unknown.
 *
 * hoverCard adds the infocard hover (see infocardHTML): the item's name over
 * its mod — opts.subtitle, the same text the row shows — or over its id where
 * the caller gave no mod.
 */
export function iconTextHTML(modID, id, {
    isFluid = false, name = null, subtitle = null,
    size = null, marquee = false, extraClass = '', hoverCard = false,
} = {}) {
    return entryTextHTML(lookupCatalog(modID, id, isFluid), modID, id,
        { name, subtitle, size, marquee, extraClass, hoverCard });
}

/**
 * machineIconTextHTML(modID, machineID, opts) → HTML string
 * The same icontext as iconTextHTML, for a machine. Separate because machines
 * resolve against their own index (see lookupMachine): a machine's texture is
 * the model the mod ships for the block, which the item catalog does not carry
 * even where an item shares its id.
 */
export function machineIconTextHTML(modID, machineID, opts = {}) {
    return entryTextHTML(lookupMachine(modID, machineID), modID, machineID, opts);
}

function entryTextHTML(entry, modID, id, {
    name = null, subtitle = null,
    size = null, marquee = false, extraClass = '', hoverCard = false,
} = {}) {
    const label = entry?.name ?? name ?? id ?? '';

    // extraClass and size come from callers and land inside a quoted attribute,
    // so they are escaped like every other interpolated value.
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
 * tagIconTextHTML(tagRef, opts) → HTML string
 * The icontext component for a tag rather than a single item: any of its
 * members satisfies it, so showing one would claim the slot needs that
 * specific item. The icon cycles through every member that has a texture
 * (JEI-style, via wirezat-ui's icon-cycle.js — call initIconCycle() on the
 * container after inserting this) once there is more than one, and the
 * subtitle gives the count. The hover card (infocardHTML; call initInfocards()
 * too) cycles with it, naming the member on show over the tag it came from.
 * Falls back to the bare tag name when no member has a known texture, and
 * skips the hover then — there is nothing to show.
 */
export function tagIconTextHTML(tagRef, { size = null, extraClass = '', chosenRef = null } = {}) {
    const resolved = lookupTag(tagRef);
    const total = resolved?.total ?? 0;

    // A chain step that resolved this tag to one specific item (chosenRef) names
    // that item, not whichever member happens to be first — and leads the
    // cycle with it, so icon and name start out agreeing.
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
    // Marks the slot as tag-derived even where there is only one known member
    // today — the same total===1 wording recipe-card.js's tagCell already
    // uses, so a tag stays visibly a tag whether or not there is anything to
    // cycle through yet.
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
 * refIconTextHTML(ref, opts) → HTML string
 * The single entry point for an ItemRef-shaped value ({ ModID, ItemID,
 * TagRef, IsFluid }) — the shape the solver hands back everywhere (chain
 * items, IO profiles, tag resolutions). Branches to tagIconTextHTML or
 * iconTextHTML so a page holding ItemRefs never has to special-case TagRef
 * itself; a page holding a bare (modID, itemID) pair still calls iconTextHTML
 * directly, same as always.
 *
 * opts.resolvedTag is a ChainItem's ResolvedTag ("#tag_name", matching
 * TagResolutions' key format): set when ref is the concrete item a tag
 * resolved to rather than a fixed recipe reference. The slot still renders
 * via tagIconTextHTML — any of the tag's members would have satisfied it —
 * naming ref as the chosen one instead of defaulting to the tag's first icon.
 */
export function refIconTextHTML(ref, opts = {}) {
    if (ref?.TagRef) return tagIconTextHTML(ref.TagRef, opts);
    if (opts.resolvedTag) {
        const tagName = opts.resolvedTag.replace(/^#/, '');
        return tagIconTextHTML(tagName, { ...opts, chosenRef: ref });
    }
    // hoverCard: a plain item gets the same card its tag-shaped neighbours do,
    // so every icon in a chain answers the same hover instead of some rows
    // falling back to the browser's own title tooltip.
    return iconTextHTML(ref?.ModID, ref?.ItemID, { ...opts, isFluid: ref?.IsFluid, hoverCard: true });
}
