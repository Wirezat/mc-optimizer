// The mod's upgrade items: display name, EU/t added per installed slot, and the
// stack size that caps how many fit.
var UPGRADES = [
  { ref: "modern_industrialization:basic_upgrade",           name: "Basic Upgrade",           bonus: 2,         max: 64 },
  { ref: "modern_industrialization:advanced_upgrade",        name: "Advanced Upgrade",        bonus: 16,        max: 64 },
  { ref: "modern_industrialization:turbo_upgrade",           name: "Turbo Upgrade",           bonus: 64,        max: 64 },
  { ref: "modern_industrialization:highly_advanced_upgrade", name: "Highly Advanced Upgrade", bonus: 512,       max: 64 },
  { ref: "modern_industrialization:quantum_upgrade",         name: "Quantum Upgrade",         bonus: 999999999, max: 1 }
]

// The machine tiers a player picks between. The id is what the config carries;
// bronze is where a fresh line starts.
var TIERS = [
  { id: "bronze",   name: "Bronze" },
  { id: "steel",    name: "Steel" },
  { id: "electric", name: "Electric" }
]
var DEFAULT_TIER = "bronze"

// upgradeByRef looks a configured tier up in the table. An unknown ref is
// null, which evaluate() reads as "no upgrades" - the config states what the
// player has unlocked, and there is no other source for that.
function upgradeByRef(ref) {
  for (var i = 0; i < UPGRADES.length; i++) {
    if (UPGRADES[i].ref === ref) return UPGRADES[i]
  }
  return null
}

// Prefix convention of the whole MI universe: MI, EI and Overdrive follow it.
function tierOf(machineID) {
  if (machineID.indexOf("bronze_") === 0) return "bronze"
  if (machineID.indexOf("steel_") === 0) return "steel"
  if (machineID.indexOf("steam_") === 0) return "steam"
  return "electric"
}

function familyOf(machineID) {
  return machineID.replace(/^(bronze|steel|steam|electric)_/, "")
}

// The families that exist on steam too; everything else is electric-only.
var STEAM_FAMILIES = [
  "alloy_smelter", "bending_machine", "blast_furnace", "canning_machine",
  "composter", "compressor", "cutting_machine", "furnace", "macerator",
  "mixer", "packer", "quarry", "unpacker", "wiremill"
]

// Steam multiblocks stand in for both bronze and steel.
var STEAM_RANK = { bronze: 1, steam: 1, steel: 2 }

// Text for the tier column. The blast-furnace family is the one place where
// two machines of the same tier differ, by coil material; the catalog names
// them "Electric Blast Furnace (Kanthal Coils)" and "(Cupronickel Coils)".
function tierLabel(machineID) {
  if (machineID === "electric_blast_furnace_cupronickel") return "Cupronickel"
  if (machineID === "electric_blast_furnace") return "Kanthal"
  var tier = tierOf(machineID)
  return tier.charAt(0).toUpperCase() + tier.slice(1)
}

// Smaller is better, and only comparable inside this plugin.
function rankOf(machineID) {
  return machineID === "electric_blast_furnace_cupronickel" ? 0 : 1
}

// A pin across supplies, a ceiling within steam. An electric machine with no
// steam variant stays allowed: vetoing it only makes the recipe impossible.
function tierAllowed(machineID, want) {
  var wantRank = STEAM_RANK[want] || 0
  var mine = STEAM_RANK[tierOf(machineID)] || 0
  if (wantRank === 0) return mine === 0
  if (mine > 0) return mine <= wantRank
  return STEAM_FAMILIES.indexOf(familyOf(machineID)) < 0
}

// Modern Industrialization reference plugin. Formulas are a direct port of
// the mod's EU overclocking model: energy per tick rises with installed
// upgrade items, and duration falls as ceil(total_energy / energy_per_tick).
var plugin = {
  api_version: 1,

  evaluate: function (ctx) {
    var m = ctx.machine.data || {}
    var r = ctx.recipe.data || {}
    var cfg = ctx.config || {}
    var duration = Math.max(ctx.recipe.duration_ticks, 1)

    // Defensive bound on the table's own caps, independent of the host's
    // maxVariants cap.
    var MAX_STACK = 64

    // Whether a machine is on the EU network is read from which energy fields its
    // mod_data carries; a machine with neither keeps the recipe's catalog rate.
    var isEUMachine = m.base_energy_per_tick !== undefined || m.max_energy_per_tick !== undefined
    if (!isEUMachine) {
      return [{
        id: "base", label: "",
        rate: { num: 1, den: duration },
        costs: [], items: [], valid: true, rank: 0
      }]
    }

    var rank = rankOf(ctx.machine.machine_id)
    var want = cfg.tier || DEFAULT_TIER
    if (!tierAllowed(ctx.machine.machine_id, want)) {
      return [{
        id: "base", label: "",
        rate: { num: 1, den: duration },
        costs: [], items: [], valid: false, rank: rank
      }]
    }

    // recipes.mod_data only ever carries energy_per_tick; total_energy is
    // always derived from it, not read from the recipe.
    var total = r.total_energy > 0 ? r.total_energy : (r.energy_per_tick || 0) * duration
    var baseMax = m.max_energy_per_tick > 0 ? m.max_energy_per_tick
                : m.base_energy_per_tick > 0 ? m.base_energy_per_tick : 32
    var fixedCap = m.fixed_recipe_eu_cap || 0
    var demand = r.energy_per_tick || 0

    // build() computes one operating point: no item installed (item is null, the
    // base case) or n copies of one upgrade item. outputs is left unset.
    function build(item, n) {
      var bonus = item ? item.bonus * n : 0
      var perTick = total > 0 ? Math.min(baseMax + bonus, total) : baseMax + bonus
      var ticks = (total > 0 && perTick > 0) ? Math.ceil(total / perTick) : duration
      var banned = (fixedCap > 0 && demand > fixedCap) || (demand > baseMax + bonus)
      return {
        id: item ? item.ref + "-x" + n : "base",
        label: item ? item.name + " \u00d7" + n : "",
        rate: { num: 1, den: ticks },
        costs: [{ resource: "eu", amount: { num: perTick, den: 1 } }],
        items: item ? [{ ref: item.ref, count: n }] : [],
        valid: !banned,
        rank: rank,
        _ticks: ticks
      }
    }

    var out = [build(null, 0)]
    var top = m.upgradable ? upgradeByRef(cfg.max_upgrade) : null
    if (top) {
      // Only the highest unlocked tier, and only counts that shorten the craft.
      var cap = Math.min(top.max, MAX_STACK)
      var prevTicks = out[0]._ticks
      for (var n = 1; n <= cap; n++) {
        var v = build(top, n)
        if (v._ticks < prevTicks) {
          out.push(v)
          prevTicks = v._ticks
        }
        if (v._ticks <= 1) break
      }
    }
    for (var k = 0; k < out.length; k++) delete out[k]._ticks
    return out
  },

  machines: {
    columns: [{ id: "tier", label: "Tier" }],

    // machine is { mod_id, machine_id }. An unknown column is "", never an
    // error: the host renders every plugin's columns for every row and only
    // the owning plugin knows what to put in one.
    cell: function (machine, columnID) {
      if (columnID !== "tier") return ""
      return tierLabel(machine.machine_id)
    }
  },

  // The wizard's content belongs to this plugin; the host contributes only the
  // modal chrome. No i18n: this text is not part of the host's bundles.
  wizard: {
    mount: function (container, ctx) {
      var cfg = ctx.config || {}
      var wrap = document.createElement("div")

      var tierLabelEl = document.createElement("label")
      tierLabelEl.style.display = "block"
      tierLabelEl.appendChild(document.createTextNode("Machine tier"))
      var tierSelect = document.createElement("select")
      tierSelect.style.display = "block"
      tierSelect.style.marginTop = "4px"
      for (var t = 0; t < TIERS.length; t++) {
        var topt = document.createElement("option")
        topt.value = TIERS[t].id
        topt.textContent = TIERS[t].name
        if (TIERS[t].id === (cfg.tier || DEFAULT_TIER)) topt.selected = true
        tierSelect.appendChild(topt)
      }
      tierLabelEl.appendChild(tierSelect)
      wrap.appendChild(tierLabelEl)

      var label = document.createElement("label")
      label.style.display = "block"
      label.style.marginTop = "12px"
      label.appendChild(document.createTextNode("Highest unlocked upgrade"))

      var select = document.createElement("select")
      select.style.display = "block"
      select.style.marginTop = "4px"

      var none = document.createElement("option")
      none.value = ""
      none.textContent = "None"
      select.appendChild(none)

      for (var i = 0; i < UPGRADES.length; i++) {
        var opt = document.createElement("option")
        opt.value = UPGRADES[i].ref
        opt.textContent = UPGRADES[i].name
        if (UPGRADES[i].ref === cfg.max_upgrade) opt.selected = true
        select.appendChild(opt)
      }
      label.appendChild(select)
      wrap.appendChild(label)

      var hint = document.createElement("div")
      hint.textContent = "Every count up to a full stack of this tier is offered; lower tiers are never worth using."
      hint.style.marginTop = "8px"
      hint.style.opacity = "0.7"
      wrap.appendChild(hint)

      var save = document.createElement("button")
      save.type = "button"
      save.textContent = "Save"
      save.style.display = "block"
      save.style.marginTop = "16px"
      save.addEventListener("click", function () {
        // An empty selection drops the key rather than writing "".
        var out = { tier: tierSelect.value }
        if (select.value) out.max_upgrade = select.value
        ctx.saveConfig(out)
          .then(function () { ctx.close() })
          .catch(function () {}) // host chrome already shows the failure
      })
      wrap.appendChild(save)

      container.appendChild(wrap)
    },
    unmount: function () {}
  }
}
