// Shared auth utilities — include on every protected page.

const Auth = (() => {
  function getToken()    { return localStorage.getItem('access_token'); }
  function getRefresh()  { return localStorage.getItem('refresh_token'); }
  function setTokens(a, r) {
    localStorage.setItem('access_token',  a);
    localStorage.setItem('refresh_token', r);
  }

  function logout() {
    const token   = getToken();
    const refresh = getRefresh();
    if (token) {
      fetch('/api/auth/logout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Authorization': 'Bearer ' + token },
        body: JSON.stringify({ refresh_token: refresh || '' }),
      }).catch(() => {});
    }
    localStorage.clear();
    window.location.href = '/login.html';
  }

  // Try to get a new access token using the refresh token.
  // Returns true on success, false if refresh token is also expired.
  async function refresh() {
    const r = getRefresh();
    if (!r) return false;
    try {
      const res = await fetch('/api/auth/refresh', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ refresh_token: r }),
      });
      if (!res.ok) return false;
      const data = await res.json();
      setTokens(data.access_token, data.refresh_token);
      return true;
    } catch {
      return false;
    }
  }

  // Authenticated fetch — retries once after token refresh on 401.
  async function apiFetch(url, options = {}) {
    const token = getToken();
    if (!token) { logout(); return null; }

    const headers = { 'Content-Type': 'application/json', ...options.headers, 'Authorization': 'Bearer ' + token };
    let res = await fetch(url, { ...options, headers });

    if (res.status === 401) {
      const ok = await refresh();
      if (!ok) { logout(); return null; }
      const newHeaders = { ...headers, 'Authorization': 'Bearer ' + getToken() };
      res = await fetch(url, { ...options, headers: newHeaders });
      if (res.status === 401) { logout(); return null; }
    }
    return res;
  }

  // Guard: redirect to login if not authenticated.
  function guard() {
    if (!getToken() && !getRefresh()) {
      window.location.href = '/login.html';
    }
  }

  // ── User / role helpers ───────────────────────────────────────────────────

  let _user = null;

  // Returns the cached user, or fetches /api/me once.
  async function getUser() {
    if (_user) return _user;
    const res = await apiFetch('/api/me');
    if (!res || !res.ok) return null;
    _user = await res.json();
    return _user;
  }

  // Store a pre-fetched user (called by header.js to avoid a second request).
  function setUser(u) { _user = u; }

  function isAdmin() { return !!(_user && _user.is_admin); }

  // Hide/remove every [data-requires="<role>"] element the current user lacks.
  // Must be called after setUser() or getUser() resolves.
  function applyRoles() {
    if (!_user) return;
    document.querySelectorAll('[data-requires]').forEach(el => {
      const required = el.getAttribute('data-requires');
      if (required === 'admin' && !_user.is_admin) el.remove();
    });
  }

  return { apiFetch, logout, guard, getToken, getUser, setUser, isAdmin, applyRoles };
})();
