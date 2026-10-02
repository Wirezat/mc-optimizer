import { defineUom, initUom, uomHTML } from '/static/ui/js/uom.js';

const TIME = [
  { id: 't',   label: 't',   factor: { num: 1,    den: 20 } },
  { id: 's',   label: 's',   factor: { num: 1,    den: 1 } },
  { id: 'min', label: 'min', factor: { num: 60,   den: 1 } },
  { id: 'h',   label: 'h',   factor: { num: 3600, den: 1 } },
];
defineUom('time', { base: 's', default: 't', units: TIME });
defineUom('cost-time', { base: 's', default: 't', units: TIME });
defineUom('volume', { base: 'mb', units: [
  { id: 'mb', label: 'mB', factor: { num: 1,       den: 1 } },
  { id: 'b',  label: 'B',  factor: { num: 1000,    den: 1 } },
  { id: 'kb', label: 'kB', factor: { num: 1000000, den: 1 } },
]});
initUom();

const TIME_UNITS = new Set(['t', 's', 'min', 'h']);

/** rateHTML renders a rate ({ num, den } | number per unit 't'|'s'|'min'|'h') as a switchable unit badge; isFluid means mB. */
export function rateHTML(value, unit, { isFluid = false, cls = '' } = {}) {
  const num = value?.num ?? value;
  const den = value?.den ?? 1;
  return uomHTML({
    value: den && den !== 1 ? `${num}/${den}` : String(num ?? 0),
    uom: isFluid ? 'volume' : '',
    unit: isFluid ? 'mb' : '',
    per: 'time',
    perUnit: TIME_UNITS.has(unit) ? unit : 's',
    cls,
  });
}

const _resources = new Set();

/** costHTML renders a plugin cost { resource, amount: { num, den } } per tick as a unit badge. */
export function costHTML(cost, { cls = '' } = {}) {
  const name = `resource:${cost.resource}`;
  if (!_resources.has(name)) {
    _resources.add(name);
    defineUom(name, { base: 'one', units: [
      { id: 'one', label: String(cost.resource).toUpperCase(), factor: { num: 1, den: 1 } },
    ]});
  }
  return uomHTML({
    value: `${cost.amount.num}/${cost.amount.den || 1}`,
    uom: name, unit: 'one', per: 'cost-time', perUnit: 't', cls,
  });
}
