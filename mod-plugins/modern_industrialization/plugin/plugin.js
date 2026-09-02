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

// Machine-tier prefix convention of MI's addon machines. steam_blast_furnace
// carries no prefix but is the only steam-tier blast furnace.
function tierOf(machineID) {
  if (machineID.indexOf("bronze_") === 0) return "bronze"
  if (machineID.indexOf("steel_") === 0) return "steel"
  if (machineID === "steam_blast_furnace") return "steam"
  return "electric"
}

// Text for the tier column. The blast-furnace family is the one place where
// two machines of the same tier differ, by coil material; the catalog names
// them "Electric Blast Furnace (Kanthal Coils)" and "(Cupronickel Coils)".
function tierLabel(machineID) {
  if (machineID === "electric_blast_furnace_cupronickel") return "Cupronickel"
  if (machineID === "electric_blast_furnace") return "Kanthal"
  var tier = tierOf(machineID)
  return tier.charAt(0).toUpperCase() + tier.slice(1)
}

function indexOfTier(candidates, tier) {
  for (var i = 0; i < candidates.length; i++) {
    if (tierOf(candidates[i].machine_id) === tier) return i
  }
  return -1
}

// Cupronickel is preferred within the electric bucket; the host has already
// dropped whichever variant cannot run this recipe.
var ELECTRIC_PREFERENCE = [
  "electric_blast_furnace_cupronickel",
  "electric_blast_furnace"
]

function electricIndex(candidates) {
  for (var p = 0; p < ELECTRIC_PREFERENCE.length; p++) {
    for (var i = 0; i < candidates.length; i++) {
      if (candidates[i].machine_id === ELECTRIC_PREFERENCE[p]) return i
    }
  }
  var any = indexOfTier(candidates, "electric")
  return any >= 0 ? any : 0
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
        costs: [], items: [], valid: true
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
        _ticks: ticks
      }
    }

    var out = [build(null, 0)]
    var top = m.upgradable ? upgradeByRef(cfg.max_upgrade) : null
    if (top) {
      // Only the highest unlocked tier is swept, and every count of it is emitted -
      // including counts that raise the energy draw without shortening the craft.
      // PickVariant never selects one of those on its own.
      var cap = Math.min(top.max, MAX_STACK)
      for (var n = 1; n <= cap; n++) {
        var v = build(top, n)
        out.push(v)
        // One tick per craft is the floor; every further count is strictly
        // worse and there is no reason to cache it.
        if (v._ticks <= 1) break
      }
    }
    for (var k = 0; k < out.length; k++) delete out[k]._ticks
    return out
  },

  // Machine-selection hooks, run by the host in the browser. They only decide
  // which recipe option a chain row uses, expressed as a recipe override.
  machines: {
    // Extra columns in the chain table, filled by cell() per row.
    columns: [{ id: "tier", label: "Tier" }],

    // machine is { mod_id, machine_id }. An unknown column is "", never an
    // error: the host renders every plugin's columns for every row and only
    // the owning plugin knows what to put in one.
    cell: function (machine, columnID) {
      if (columnID !== "tier") return ""
      return tierLabel(machine.machine_id)
    },

    // candidates is [{ mod_id, machine_id }] for one recipe, config is this
    // mod's own config. Returns the index of the machine that runs the recipe
    // at the configured tier, or -1 to leave the row alone. The tier lives in
    // the config so the host never has to know MI has tiers at all.
    resolve: function (candidates, config) {
      if (!candidates.length) return -1
      var want = (config && config.tier) || DEFAULT_TIER
      if (want === "bronze" || want === "steel") {
        var own = indexOfTier(candidates, want)
        if (own >= 0) return own
        var steam = indexOfTier(candidates, "steam")
        if (steam >= 0) return steam
      }
      return electricIndex(candidates)
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
