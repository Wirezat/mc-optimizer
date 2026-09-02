// Host-owned modal chrome for a mod's configuration UI. The plugin's own
// source (fetched from /plugin-assets, evaluated the same way goja evaluates
// it server-side) owns everything inside the body; the host never inspects it.
import { t } from './i18n.js'
import { apiFetch } from '/static/ui/js/auth.js'

// Package classes from wirezat-ui/components/modal.css (the dynamically
// created backdrop variant, not the static [data-wui-modal] one) — no
// page-local replacement styles.
const MODAL_BACKDROP_CLASS = 'modal-backdrop'
const MODAL_CARD_CLASS = 'modal-card'
const MODAL_HEADER_CLASS = 'modal-header'
const MODAL_TITLE_CLASS = 'modal-title'
const MODAL_CLOSE_CLASS = 'modal-close'
// .wui-guest is the package's contract for a container whose content is
// authored by someone who does not know the package.
const MODAL_BODY_CLASS = 'modal-body wui-guest'

let active = null

function close() {
    if (active?.plugin?.wizard?.unmount) {
        try { active.plugin.wizard.unmount(active.body) } catch (e) { console.error(e) }
    }
    active?.backdrop.remove()
    active = null
    document.removeEventListener('keydown', onEscape)
}

function onEscape(e) { if (e.key === 'Escape') close() }

// Both endpoints sit behind the app's auth, so this goes through apiFetch.
async function loadPlugin(modId) {
    const res = await apiFetch(`/plugin-assets/${encodeURIComponent(modId)}/plugin.js`)
    if (!res?.ok) throw new Error(`plugin asset ${modId}: ${res?.status}`)
    return new Function(`${await res.text()}; return plugin;`)()
}

// openPluginWizard shows the configuration UI of one mod. The caller owns the
// config: it passes the current one in and receives the new one through onSave,
// which may be async and whose rejection is shown in the modal.
export async function openPluginWizard(modId, displayName, { config = {}, onSave } = {}) {
    close()

    const backdrop = document.createElement('div')
    backdrop.className = MODAL_BACKDROP_CLASS
    backdrop.addEventListener('click', e => { if (e.target === backdrop) close() })

    const card = document.createElement('div')
    card.className = MODAL_CARD_CLASS
    backdrop.appendChild(card)

    const header = document.createElement('div')
    header.className = MODAL_HEADER_CLASS
    const title = document.createElement('span')
    title.className = MODAL_TITLE_CLASS
    title.textContent = displayName // technical mod name, not translated
    // Host-owned save-error feedback, shown next to the title so it survives
    // whatever the plugin does to its own body — .status-msg is the package
    // class already used for inline form/action feedback elsewhere.
    const status = document.createElement('span')
    status.className = 'status-msg'
    const closeBtn = document.createElement('button')
    closeBtn.type = 'button'
    closeBtn.className = MODAL_CLOSE_CLASS
    closeBtn.textContent = '×'
    closeBtn.addEventListener('click', close)
    header.append(title, status, closeBtn)
    card.appendChild(header)

    const body = document.createElement('div')
    body.className = MODAL_BODY_CLASS
    body.textContent = t('plugin.wizard.loading')
    card.appendChild(body)

    document.body.appendChild(backdrop)
    document.addEventListener('keydown', onEscape)
    // Compare against ctx, never re-read `active`: a later openPluginWizard call
    // may swap it out at any await below.
    const ctx = { backdrop, body, status, plugin: null }
    active = ctx

    async function saveConfig(cfg) {
        try {
            await onSave?.(cfg)
            if (active === ctx) { ctx.status.className = 'status-msg'; ctx.status.textContent = '' }
        } catch (e) {
            console.error(e)
            if (active === ctx) { ctx.status.className = 'status-msg err'; ctx.status.textContent = t('plugin.wizard.save_error') }
            throw e
        }
    }

    try {
        const plugin = await loadPlugin(modId)
        if (active !== ctx) return
        ctx.plugin = plugin
        ctx.body.textContent = ''
        plugin.wizard.mount(ctx.body, { config, saveConfig, close })
    } catch (e) {
        console.error(e)
        if (active === ctx) ctx.body.textContent = t('plugin.wizard.load_error')
    }
}

export function closePluginWizard() { close() }
