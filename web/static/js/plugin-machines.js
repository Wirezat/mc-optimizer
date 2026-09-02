// Host-side adapter for a mod's optional `machines` binding: extra chain-table
// columns, and which machine runs a recipe under the mod's own config. Every
// hook call is wrapped per mod; a mod without a binding is simply absent.
import { apiFetch } from '/static/ui/js/auth.js'

// /plugin-assets sits behind the same auth as the rest of the app, so a demo
// (logged-out) page gets 401 here and renders no axes and no columns. That is
// the correct degradation: without a plugin there is nothing to declare.
async function loadOne(modID) {
    const res = await apiFetch(`/plugin-assets/${encodeURIComponent(modID)}/plugin.js`)
    if (!res?.ok) return null
    const plugin = new Function(`${await res.text()}; return plugin;`)()
    return plugin?.machines ?? null
}

// The discover response names a plugin mod for every reached machine, including
// mods that ship none, so a miss is recorded and never retried.
const failedMods = new Set()

// loadMachineHooks resolves the `machines` binding for each mod id, skipping
// every mod that has no plugin, no binding, or a plugin that fails to load.
export async function loadMachineHooks(modIDs) {
    const out = new Map()
    const unique = [...new Set(modIDs)].filter(Boolean).filter(m => !failedMods.has(m))
    const loaded = await Promise.all(unique.map(async modID => {
        try {
            return [modID, await loadOne(modID)]
        } catch (e) {
            console.error(`plugin machines ${modID}:`, e)
            return [modID, null]
        }
    }))
    for (const [modID, machines] of loaded) {
        if (machines) out.set(modID, machines)
        else failedMods.add(modID)
    }
    return out
}

// columnsOf flattens every mod's declared chain-table columns, in mod order.
// One mod's malformed columns list costs only that mod's columns.
export function columnsOf(hooks) {
    const out = []
    for (const [modID, m] of hooks) {
        try {
            for (const col of m.columns ?? []) {
                if (!col?.id) continue
                out.push({ modID, columnID: String(col.id), label: String(col.label ?? col.id) })
            }
        } catch (e) {
            console.error(`plugin machines ${modID}.columns:`, e)
        }
    }
    return out
}

// cellText asks the plugin that owns a machine for that machine's text in one
// column. A cell belonging to another mod's column is empty by design.
export function cellText(hooks, pluginModID, machine, columnID) {
    const m = hooks.get(pluginModID)
    if (!m?.cell) return ''
    try {
        const v = m.cell(machine, columnID)
        return typeof v === 'string' ? v : ''
    } catch (e) {
        console.error(`plugin machines ${pluginModID}.cell:`, e)
        return ''
    }
}

// resolveIndex asks the plugin which candidate runs a recipe under its own
// config. -1 means no opinion, and is also what a bad answer degrades to.
export function resolveIndex(hooks, pluginModID, candidates, config) {
    const m = hooks.get(pluginModID)
    if (!m?.resolve) return -1
    try {
        const i = m.resolve(candidates, config || {})
        return Number.isInteger(i) && i >= 0 && i < candidates.length ? i : -1
    } catch (e) {
        console.error(`plugin machines ${pluginModID}.resolve:`, e)
        return -1
    }
}
