var plugin = {
  api_version: 1,
  evaluate: function (ctx) {
    return [{
      id: "upgraded", label: "Upgraded",
      rate: { num: 1, den: 10 },
      costs: [], outputs: ctx.recipe.outputs, valid: true,
      items: [{ ref: "testmod:upgrade", count: 1 }]
    }]
  }
}
