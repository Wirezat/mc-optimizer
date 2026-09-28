// Host-side adapter for a mod's optional `machines` binding: extra chain-table columns and
// their per-machine text. Every hook call is wrapped per mod; a mod without a binding is simply
// absent.
import { apiFetch } from '/static/ui/js/auth.js'

async function loadOne(modID) {
    const res = await apiFetch(`/plugin-assets/${encodeURIComponent(modID)}/plugin.js`)
    if (!res?.ok) return null
    const plugin = new Function(`${await res.text()}; return plugin;`)()
    return plugin?.machines ?? null
}

const failedMods = new Set()

// loadMachineHooks resolves the `machines` binding for each mod id, skipping every mod that has
// no plugin, no binding, or a plugin that fails to load.
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

// columnsOf collects every mod's declared chain-table columns, in mod order. Mods that declare
// the same column id share one column.
export function columnsOf(hooks) {
    const byID = new Map()
    for (const [modID, m] of hooks) {
        try {
            for (const col of m.columns ?? []) {
                if (!col?.id) continue
                const columnID = String(col.id)
                const shared = byID.get(columnID)
                if (shared) { shared.mods.push(modID); continue }
                byID.set(columnID, { columnID, label: String(col.label ?? columnID), mods: [modID] })
            }
        } catch (e) {
            console.error(`plugin machines ${modID}.columns:`, e)
        }
    }
    return [...byID.values()]
}

// cellText asks the plugin that owns a machine for that machine's text in one column; variant
// is the chosen { id, label } or null before a solve. Returns empty for a cell belonging to
// another mod's column.
export function cellText(hooks, pluginModID, machine, columnID, variant = null) {
    const m = hooks.get(pluginModID)
    if (!m?.cell) return ''
    try {
        const v = m.cell(machine, columnID, variant)
        return typeof v === 'string' ? v : ''
    } catch (e) {
        console.error(`plugin machines ${pluginModID}.cell:`, e)
        return ''
    }
}
