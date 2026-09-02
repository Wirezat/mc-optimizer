var plugin = {
  api_version: 1,
  evaluate: function (ctx) {
    return [{
      id: "base",
      label: "Base",
      rate: { num: 1, den: ctx.recipe.duration_ticks },
      costs: [],
      outputs: ctx.recipe.outputs,
      items: [],
      valid: true
    }]
  }
}
