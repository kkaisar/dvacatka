// Общие хелперы фронтенда Dvacatka.

// api — обёртка над fetch: JSON, cookie с JWT уходит автоматически (same-origin).
async function api(method, url, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(url, opts);
  let data = null;
  const text = await res.text();
  if (text) {
    try { data = JSON.parse(text); } catch { data = text; }
  }
  if (!res.ok) {
    const msg = (data && data.error) ? data.error : (typeof data === "string" ? data : "ошибка");
    throw new Error(msg);
  }
  return data;
}

// requireAuth — гарантирует, что пользователь залогинен; иначе на /login.html.
async function requireAuth() {
  try {
    return await api("GET", "/profile");
  } catch {
    location.href = "/login.html";
    throw new Error("redirect");
  }
}

const CATEGORIES = ["A", "B", "C", "Д", "Captain"];

const LOBBY_TYPES = {
  dvacatka: { label: "Двацатка", players: 20, teams: 4 },
  tricatka: { label: "Трицатка", players: 30, teams: 6 },
  sorokovka: { label: "Сороковка", players: 40, teams: 8 },
};

const STATUS_LABEL = {
  open: "Сбор игроков",
  draft: "Драфт",
  active: "Турнир идёт",
  finished: "Завершён",
};

function statusBadge(status) {
  return `<span class="badge badge--${status}">${STATUS_LABEL[status] || status}</span>`;
}

function esc(s) {
  return String(s == null ? "" : s).replace(/[&<>"']/g, c =>
    ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
}

function qs(name) {
  return new URLSearchParams(location.search).get(name);
}

async function logout() {
  try { await api("POST", "/auth/logout"); } catch {}
  location.href = "/login.html";
}

// navBar — общая шапка с ником, настройками и выходом.
function navBar(me) {
  return `
  <header class="nav">
    <div class="nav__inner">
      <a href="/" class="nav__brand">🎮 Dva<span class="dot">·</span>catka</a>
      <nav class="nav__links">
        <span class="nav__user">${esc(me.nickname)} <span class="cat">${esc(me.category)}</span></span>
        <a href="/settings.html">Настройки</a>
        <button class="nav__btn" onclick="logout()">Выход</button>
      </nav>
    </div>
  </header>`;
}
