// Iron Furnaces ships no process recipes: its thirteen furnaces run the vanilla
// smelting, smoking and blasting recipes, faster and in batches. All numbers are
// Config.java defaults of 4.1.8 (MC 1.20.1); the machine's own mod_data carries
// them per furnace, this table only orders the material ladder and names it.
var MATERIALS = [
  { id: "copper_furnace", name: "Copper" },
  { id: "iron_furnace", name: "Iron" },
  { id: "silver_furnace", name: "Silver" },
  { id: "gold_furnace", name: "Golden" },
  { id: "diamond_furnace", name: "Diamond" },
  { id: "emerald_furnace", name: "Emerald" },
  { id: "crystal_furnace", name: "Crystal" },
  { id: "obsidian_furnace", name: "Obsidian" },
  { id: "netherite_furnace", name: "Netherite" },
  { id: "million_furnace", name: "Rainbow" },
  { id: "allthemodium_furnace", name: "Allthemodium" },
  { id: "vibranium_furnace", name: "Vibranium" },
  { id: "unobtainium_furnace", name: "Unobtainium" }
]

// Factory slots per energy tier (SlotIronFurnaceInputFactory.isActive).
var FACTORY_SLOTS = [2, 4, 6]

// Burn value in ticks; operations per item is burn / 200, whatever the furnace.
var FUELS = [
  { id: "coals", name: "Coal / Charcoal", ref: "#coals", burn: 1600 },
  { id: "coal_block", name: "Block of Coal", ref: "#c:storage_blocks/coal", burn: 16000 },
  { id: "blaze_rod", name: "Blaze Rod", ref: "minecraft:blaze_rod", burn: 2400 },
  { id: "dried_kelp_block", name: "Dried Kelp Block", ref: "minecraft:dried_kelp_block", burn: 4000 },
  { id: "lava_bucket", name: "Lava Bucket", ref: "minecraft:lava_bucket", burn: 20000 },
  { id: "planks", name: "Planks", ref: "#minecraft:planks", burn: 300 }
]

// The red augment follows from the recipe's own machine, never from the config.
var RED_BY_BASE = {
  "minecraft:furnace": null,
  "minecraft:smoker": "smoking",
  "minecraft:blast_furnace": "blasting"
}

var DEFAULTS = { max_tier: "iron_furnace", mode: "none", fuel: "coals" }

function indexOf(list, id) {
  for (var i = 0; i < list.length; i++) if (list[i].id === id) return i
  return -1
}

function fuelById(id) {
  var i = indexOf(FUELS, id)
  return i < 0 ? FUELS[0] : FUELS[i]
}

// One invalid cell, so the machine drops out of the matrix with a reason.
function vetoed(ticks, why) {
  return [{ id: "plain", label: "", rate: { num: 1, den: ticks },
            costs: [], inputs: [], items: [], valid: false, why: why }]
}

var plugin = {
  api_version: 1,

  evaluate: function (ctx) {
    var m = ctx.machine.data || {}
    var cfg = ctx.config || {}
    var duration = Math.max(ctx.recipe.duration_ticks, 1)

    var maxTier = cfg.max_tier || DEFAULTS.max_tier
    var mode = cfg.mode || DEFAULTS.mode
    var fuel = fuelById(cfg.fuel || DEFAULTS.fuel)

    var speed = m.speed > 0 ? m.speed : 200
    var tier = m.energy_tier > 0 ? m.energy_tier : 0
    var batch = m.batch > 0 ? m.batch : 1

    // Integer arithmetic exactly as the mod's getSpeed() does it.
    var base = Math.max(1, Math.floor(speed * duration / 200))

    // The thirteen materials share one supply, so the config is a ceiling.
    var own = indexOf(MATERIALS, ctx.machine.machine_id)
    var cap = indexOf(MATERIALS, maxTier)
    if (own < 0 || cap < 0 || own > cap) return vetoed(base, "material not built")
    if (mode === "generator") return vetoed(base, "generator does not smelt")

    var red = RED_BY_BASE[(ctx.recipe.machine_mod || "") + ":" + (ctx.recipe.machine_id || "")]
    if (red === undefined) red = null

    var factory = mode === "factory"
    var slots = factory ? FACTORY_SLOTS[tier] : 1

    var greens = [
      { key: null, id: "plain", label: "", num: 1, den: 1, rank: 1 },
      { key: "fuel", id: "fuel", label: "Fuel Efficiency", num: 1, den: 2, rank: 0 },
      { key: "speed", id: "speed", label: "Speed", num: 2, den: 1, rank: 2 }
    ]

    var out = []
    for (var i = 0; i < greens.length; i++) {
      var g = greens[i]
      var t = base
      if (g.key === "speed") t = Math.max(1, Math.floor(base / 2))
      if (g.key === "fuel") t = Math.max(1, Math.ceil(base * 1.25))

      // Speed without a gain is dominated: same rate, twice the draw.
      if (g.key === "speed" && t === base) continue

      var items = []
      if (red) items.push({ ref: "ironfurnaces:augment_" + red, count: 1 })
      if (g.key) items.push({ ref: "ironfurnaces:augment_" + g.key, count: 1 })
      if (factory) items.push({ ref: "ironfurnaces:augment_factory", count: 1 })

      var costs = [], inputs = []
      if (factory) {
        // RF per operation and slot is recipe duration x 20; speed x2, fuel /2.
        costs.push({ resource: "rf", amount: { num: slots * duration * 20 * g.num, den: t * g.den } })
      } else {
        // Fuel per tick, the unit the host charges vanilla in, and per craft.
        // The cooking time cancels out of the mod's fuel formula: only the green
        // augment and the batch change what an item costs.
        costs.push({ resource: fuel.id, amount: { num: slots * 200 * g.num, den: t * fuel.burn * g.den } })
        inputs.push({ ref: fuel.ref, amount: { num: 200 * g.num, den: fuel.burn * batch * g.den } })
      }

      var label = []
      if (red) label.push(red.charAt(0).toUpperCase() + red.slice(1))
      if (factory) label.push("Factory x" + slots)
      if (g.label) label.push(g.label)

      out.push({
        // Schematic id - a permanent contract, never derived from the label.
        id: (factory ? "factory_" : "") + (red ? red + "_" : "") + g.id,
        label: label.join(" + "),
        rate: { num: slots * batch, den: t },
        costs: costs,
        inputs: inputs,
        items: items,
        valid: true,
        // Cheaper material first, and within one material fuel before plain
        // before speed.
        rank: own * 10 + g.rank
      })
    }
    return out
  },

  machines: {
    // MI already owns "Tier", and the host flattens columns across mods.
    columns: [{ id: "if_material", label: "Material" }],

    cell: function (machine, columnID) {
      if (columnID !== "if_material") return ""
      var i = indexOf(MATERIALS, machine.machine_id)
      return i < 0 ? "" : MATERIALS[i].name
    }
  },

  wizard: {
    mount: function (container, ctx) {
      var cfg = ctx.config || {}

      function field(labelText, control, caption) {
        var l = document.createElement("label")
        l.className = "field"
        var s = document.createElement("span")
        s.className = "field-label"
        s.textContent = labelText
        l.append(s, control)
        if (caption) {
          var c = document.createElement("span")
          c.className = "field-caption"
          c.textContent = caption
          l.appendChild(c)
        }
        return l
      }

      function select(options, selected) {
        var sel = document.createElement("select")
        sel.className = "input"
        for (var i = 0; i < options.length; i++) {
          var o = document.createElement("option")
          o.value = options[i][0]
          o.textContent = options[i][1]
          if (options[i][0] === selected) o.selected = true
          sel.appendChild(o)
        }
        return sel
      }

      var tiers = MATERIALS.map(function (mt) { return [mt.id, mt.name + " Furnace"] })
      var fuels = FUELS.map(function (f) {
        return [f.id, f.name + " — " + (f.burn / 200) + " operations each"]
      })
      var tierSel = select(tiers, cfg.max_tier || DEFAULTS.max_tier)
      var modeSel = select([["none", "None — fuel"], ["factory", "Factory — RF"]],
        cfg.mode || DEFAULTS.mode)
      var fuelSel = select(fuels, cfg.fuel || DEFAULTS.fuel)

      var fTier = field("Highest furnace built", tierSel,
        "Everything up to this counts as available; the solver picks the smallest that fits.")
      var fMode = field("Operating mode", modeSel,
        "A fixed choice: without the augment the furnace burns fuel, with it the factory draws RF.")
      var fFuel = field("Fuel", fuelSel, "Only without the factory augment.")

      function syncFuel() { fFuel.hidden = modeSel.value === "factory" }
      modeSel.addEventListener("change", syncFuel)
      syncFuel()

      var save = document.createElement("button")
      save.type = "button"
      save.className = "btn btn-primary"
      save.textContent = "Save"
      save.addEventListener("click", function () {
        var out = { max_tier: tierSel.value, mode: modeSel.value }
        if (modeSel.value !== "factory") out.fuel = fuelSel.value
        ctx.saveConfig(out).then(function () { ctx.close() }).catch(function () {})
      })
      var actions = document.createElement("div")
      actions.className = "form-actions"
      actions.appendChild(save)

      container.append(fTier, fMode, fFuel, actions)
    },
    unmount: function () {}
  }
}
