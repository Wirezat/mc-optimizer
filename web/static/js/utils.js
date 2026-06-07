/* utils.js — shared UI utilities */
const Utils = (() => {
  function debounce(fn, ms = 250) {
    let t;
    return (...args) => { clearTimeout(t); t = setTimeout(() => fn(...args), ms); };
  }

  // Attach/update a datalist autocomplete to an input element.
  // options: string[] (capped at 200 to keep the list useful)
  function suggestions(inputEl, options) {
    if (!inputEl) return;
    const id = '_dl_' + inputEl.id;
    let dl = document.getElementById(id);
    if (!dl) {
      dl = document.createElement('datalist');
      dl.id = id;
      inputEl.setAttribute('list', id);
      document.body.appendChild(dl);
    }
    dl.innerHTML = options.slice(0, 200).map(s =>
      `<option value="${String(s ?? '').replace(/&/g, '&amp;').replace(/"/g, '&quot;')}">`
    ).join('');
  }

  return { debounce, suggestions };
})();
