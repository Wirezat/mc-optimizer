var plugin = {
  api_version: 1,
  evaluate: function (ctx) {
    return [{
      id: "base", label: "Base",
      rate: { num: 1, den: ctx.recipe.duration_ticks },
      costs: [], items: [], valid: true,
      outputs: [{ ref: "testmod:something_else", amount: { num: 1, den: 1 },
                  probability: { num: 1, den: 1 } }]
    }]
  }
}
