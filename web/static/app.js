// Библиотека Ереси: фильтры, отметки о прочитанном, вход через API (если он подключён).
// Без сборщиков и зависимостей — обычный ES-модуль-в-скрипте.
(() => {
  'use strict';

  const CFG = window.WAHBOOK || { base: '/', api: '' };
  const STATUS_LABEL = { '': 'Отметить', want: 'Хочу прочитать', reading: 'Читаю', read: 'Прочитано' };
  const NEXT = { '': 'want', want: 'reading', reading: 'read', read: '' };
  const LS_PROGRESS = 'wahbook.progress';
  const LS_TOKEN = 'wahbook.token';

  const $ = (sel, root = document) => root.querySelector(sel);
  const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];
  const norm = (s) => s.toLowerCase().replaceAll('ё', 'е').trim();

  // ── хранилище ───────────────────────────────────────────────────────────
  const ls = {
    get(key) { try { return localStorage.getItem(key); } catch { return null; } },
    set(key, v) { try { localStorage.setItem(key, v); } catch { /* приватный режим */ } },
    del(key) { try { localStorage.removeItem(key); } catch { /* ignore */ } },
  };

  let progress = {};          // bookId -> 'want' | 'reading' | 'read'
  let unlocked = {};          // 'id:уровень' и 'rank:номер' -> когда получено (мс); полученное не пропадает
  let user = null;            // { id, email, name } после входа
  let token = ls.get(LS_TOKEN);

  // ── долговременное хранение отметок в браузере ──────────────────────────
  // Отметки пишутся сразу в два хранилища, localStorage и IndexedDB: если браузер
  // очистит одно, данные восстановятся из другого. Дополнительно мы просим браузер
  // не вытеснять данные сайта (storage.persist) и даём копию в файл — это единственная
  // защита от ручной очистки данных и от «чистки через 7 дней» в Safari.
  const IDB_NAME = 'wahbook';
  const IDB_STORE = 'kv';
  const IDB_KEY = 'progress';

  const idb = {
    db: null,
    open() {
      this.db ??= new Promise((resolve, reject) => {
        if (!('indexedDB' in window)) return reject(new Error('IndexedDB недоступна'));
        const req = indexedDB.open(IDB_NAME, 1);
        req.onupgradeneeded = () => req.result.createObjectStore(IDB_STORE);
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      });
      return this.db;
    },
    async run(mode, fn) {
      const db = await this.open();
      return new Promise((resolve, reject) => {
        const tx = db.transaction(IDB_STORE, mode);
        const req = fn(tx.objectStore(IDB_STORE));
        tx.oncomplete = () => resolve(req.result);
        tx.onerror = tx.onabort = () => reject(tx.error);
      });
    },
    get: (key) => idb.run('readonly', (st) => st.get(key)),
    set: (key, value) => idb.run('readwrite', (st) => st.put(value, key)),
    del: (key) => idb.run('readwrite', (st) => st.delete(key)),
  };

  // Запись хранилища: { v: 1, updated: <мс>, progress: {...}, unlocked: {...} }. Раньше в localStorage
  // лежал просто объект {id: статус} — он читается как запись с updated = 0.
  const cleanProgress = (obj) => {
    const out = {};
    for (const [id, st] of Object.entries(obj || {})) {
      if (st === 'want' || st === 'reading' || st === 'read') out[id] = st;
    }
    return out;
  };
  const cleanUnlocked = (obj) => {
    const out = {};
    for (const [k, ts] of Object.entries(obj || {})) {
      if (/^[\w-]+:\d+$/.test(k) && Number.isFinite(ts) && ts > 0) out[k] = ts;
    }
    return out;
  };
  // объединение полученных достижений: время берём самое раннее
  const mergeUnlocked = (...maps) => {
    const out = {};
    for (const m of maps) for (const [k, ts] of Object.entries(m)) if (!out[k] || ts < out[k]) out[k] = ts;
    return out;
  };
  const parseRecord = (raw) => {
    try {
      const o = typeof raw === 'string' ? JSON.parse(raw) : raw;
      if (!o || typeof o !== 'object') return null;
      return o.v === 1 ? { updated: o.updated || 0, progress: cleanProgress(o.progress), unlocked: cleanUnlocked(o.unlocked) }
                       : { updated: 0, progress: cleanProgress(o), unlocked: {} };
    } catch { return null; }
  };

  function saveLocal(map = progress, updated = Date.now(), unl = unlocked) {
    const rec = { v: 1, updated, progress: map, unlocked: unl };
    ls.set(LS_PROGRESS, JSON.stringify(rec));
    idb.set(IDB_KEY, rec).catch(() => { /* IndexedDB закрыта: остаётся localStorage */ });
  }

  async function loadLocal() {
    const idbRec = await idb.get(IDB_KEY).catch(() => null);
    const recs = [parseRecord(ls.get(LS_PROGRESS)), parseRecord(idbRec)].filter(Boolean);
    if (!recs.length) return { progress: {}, unlocked: {} };
    const best = recs.reduce((a, b) => (b.updated > a.updated ? b : a));
    const unl = mergeUnlocked(...recs.map((r) => r.unlocked));
    saveLocal(best.progress, best.updated, unl); // самовосстановление: заполнить опустевшее хранилище
    return { progress: best.progress, unlocked: unl };
  }

  function clearLocal() {
    ls.del(LS_PROGRESS);
    idb.del(IDB_KEY).catch(() => {});
  }

  let persisted = null; // true — браузер обещал не вытеснять данные сайта
  async function requestPersistence() {
    if (!navigator.storage?.persist) return;
    try {
      persisted = (await navigator.storage.persisted()) || (await navigator.storage.persist());
    } catch { /* ignore */ }
    renderBackupNote();
  }

  // ── API ─────────────────────────────────────────────────────────────────
  async function api(path, { method = 'GET', body } = {}) {
    const res = await fetch(CFG.api + path, {
      method,
      headers: {
        ...(body ? { 'Content-Type': 'application/json' } : {}),
        ...(token ? { Authorization: 'Bearer ' + token } : {}),
      },
      body: body ? JSON.stringify(body) : undefined,
    });
    if (res.status === 204) return null;
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw Object.assign(new Error(data.message || 'Ошибка ' + res.status), { status: res.status });
    return data;
  }

  async function setStatus(id, st) {
    await ready; // пока сохранённые отметки не прочитаны, писать в хранилище нельзя: затрём их
    if (st) progress[id] = st; else delete progress[id];
    renderCard(id);
    renderStats();
    applyFilters();
    if (user) {
      api('/api/progress/' + encodeURIComponent(id), { method: 'PUT', body: { status: st } })
        .catch((e) => console.warn('не удалось сохранить на сервере:', e));
    } else {
      saveLocal();
      if (persisted === null) requestPersistence();
    }
    evaluateAchievements();
  }

  // ── отрисовка ───────────────────────────────────────────────────────────
  function renderCard(id) {
    const card = $(`.card[data-id="${CSS.escape(id)}"]`);
    if (!card) return;
    const st = progress[id] || '';
    if (st) card.dataset.st = st; else delete card.dataset.st;
    $('.status', card).textContent = STATUS_LABEL[st];
  }

  function renderStats() {
    for (const section of $$('.series')) {
      const cards = $$('.card', section);
      const read = cards.filter((c) => progress[c.dataset.id] === 'read').length;
      const key = section.dataset.series;
      $(`[data-nav-read="${key}"]`).textContent = read;
      $(`[data-bar="${key}"]`).style.width = (cards.length ? (read / cards.length) * 100 : 0) + '%';
    }
  }

  // ── фильтры ─────────────────────────────────────────────────────────────
  const el = {
    q: $('#q'), kind: $('#f-kind'), status: $('#f-status'), author: $('#f-author'), tr: $('#f-tr'), sort: $('#f-sort'),
    reset: $('#f-reset'), summary: $('#summary'), empty: $('#empty'), active: $('#f-active'),
  };

  function applyFilters() {
    const q = norm(el.q.value);
    const kind = el.kind.value;
    const status = el.status.value;
    const author = el.author.value;
    const tr = el.tr.value;
    let shown = 0;
    const total = $$('.card').length;

    for (const section of $$('.series')) {
      let visible = 0;
      for (const card of $$('.card', section)) {
        const st = progress[card.dataset.id] || 'none';
        const ok =
          (!q || norm(card.dataset.search).includes(q)) &&
          (!kind || card.dataset.kind === kind) &&
          (!status || st === status) &&
          (!author || card.dataset.authors.split('|').includes(author)) &&
          (!tr || card.dataset.tr === tr);
        card.hidden = !ok;
        if (ok) visible++;
      }
      section.hidden = visible === 0;
      shown += visible;
    }

    const read = Object.values(progress).filter((s) => s === 'read').length;
    el.summary.textContent = `Показано ${shown} из ${total} · прочитано ${read}`;
    el.empty.hidden = shown !== 0;
    el.reset.hidden = !(q || kind || status || author || tr);
    const active = [q, kind, status, author, tr].filter(Boolean).length; // порядок — не фильтр
    el.active.hidden = !active;
    el.active.textContent = `Активно: ${active}`;
  }

  el.q.addEventListener('input', applyFilters);
  el.kind.addEventListener('change', applyFilters);
  el.status.addEventListener('change', applyFilters);
  el.author.addEventListener('change', applyFilters);
  el.tr.addEventListener('change', applyFilters);
  // На телефоне панель по умолчанию свёрнута: список книг важнее; заголовок с числом активных фильтров виден всегда.
  if (matchMedia('(max-width: 560px)').matches) $('#filters').open = false;

  // ── порядок книг внутри серий ───────────────────────────────────────────
  // Серии остаются блоками; сортируются карточки внутри каждой. Исходный порядок (как в каталоге)
  // запоминаем сразу: он же — запасной ключ, так что сортировка устойчива.
  const LS_SORT = 'wahbook.sort';
  const collator = new Intl.Collator('ru');
  $$('.card').forEach((c, i) => { c.dataset.pos = i; });
  const num = (c, key) => Number(c.dataset[key]);
  const SORTS = {
    series: () => 0,
    'year-asc': (a, b) => num(a, 'year') - num(b, 'year'),
    'year-desc': (a, b) => num(b, 'year') - num(a, 'year'),
    // без оценки — в конец; при равенстве выше та, у которой больше голосов
    rating: (a, b) => {
      const ra = a.dataset.rating ? num(a, 'rating') : -1;
      const rb = b.dataset.rating ? num(b, 'rating') : -1;
      return rb - ra || num(b, 'votes') - num(a, 'votes');
    },
    title: (a, b) => collator.compare(a.dataset.title, b.dataset.title),
    // антологии (без автора) — в конец: !by равно 1 только у них
    author: (a, b) => (!a.dataset.by - !b.dataset.by) || collator.compare(a.dataset.by, b.dataset.by),
  };

  function applySort() {
    const cmp = SORTS[el.sort.value] || SORTS.series;
    for (const grid of $$('.grid')) {
      const cards = $$('.card', grid).sort((a, b) => cmp(a, b) || num(a, 'pos') - num(b, 'pos'));
      grid.append(...cards);
    }
  }
  el.sort.addEventListener('change', () => { applySort(); ls.set(LS_SORT, el.sort.value); });
  { // запомненный порядок
    const saved = ls.get(LS_SORT);
    if (saved && SORTS[saved]) { el.sort.value = saved; applySort(); }
  }
  el.reset.addEventListener('click', () => {
    el.q.value = ''; el.kind.value = ''; el.status.value = ''; el.author.value = ''; el.tr.value = '';
    applyFilters();
  });
  document.addEventListener('keydown', (e) => {
    if (e.key === '/' && !/INPUT|SELECT|TEXTAREA/.test(document.activeElement.tagName)) {
      e.preventDefault();
      el.q.focus();
    }
  });

  // ── подсветка текущей серии в навигации ─────────────────────────────────
  if ('IntersectionObserver' in window) {
    const links = new Map($$('[data-nav]').map((a) => [a.dataset.nav, a]));
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) {
        if (e.isIntersecting) {
          links.forEach((a) => a.classList.remove('active'));
          links.get(e.target.dataset.series)?.classList.add('active');
        }
      }
    }, { rootMargin: '-35% 0px -60% 0px' });
    $$('.series').forEach((s) => io.observe(s));
  }

  // ── карточки и диалог книги ─────────────────────────────────────────────
  const CONTENTS = (() => { try { return JSON.parse($('#contents-data')?.textContent || '{}'); } catch { return {}; } })();
  const dlg = $('#book-dialog');
  let dlgBook = null;

  document.addEventListener('click', (e) => {
    const statusBtn = e.target.closest('[data-status]');
    if (statusBtn) {
      const id = statusBtn.dataset.status;
      setStatus(id, NEXT[progress[id] || '']);
      return;
    }
    const open = e.target.closest('[data-open]');
    if (open) openBook(open.dataset.open);
  });

  function openBook(id) {
    const card = $(`.card[data-id="${CSS.escape(id)}"]`);
    if (!card) return;
    dlgBook = id;
    $('#bd-title').textContent = $('h3', card).textContent;
    const orig = $('.orig', card);
    $('#bd-orig').textContent = orig ? orig.textContent : '';
    $('#bd-by').textContent = $('.by', card).textContent;
    $('#bd-by-orig').textContent = $('.by-orig', card)?.textContent ?? '';
    $('#bd-sub').textContent = $('.sub', card).textContent;
    const section = card.closest('.series');
    const badge = $('.badge', card)?.textContent;
    $('#bd-series').textContent = $('h2', section).firstChild.textContent.trim() + (badge ? ` · № ${badge}` : '');

    const cover = $('#bd-cover');
    cover.replaceChildren($('.cover img, .cover .ph', card).cloneNode(true));

    const trLine = $('#bd-tr');
    trLine.hidden = !card.dataset.tr;
    trLine.className = `bd-tr tr tr-${card.dataset.tr}`;
    trLine.textContent = card.dataset.trNote || '';
    const rating = $('#bd-rating');
    rating.hidden = !card.dataset.rating;
    if (card.dataset.rating) {
      rating.textContent = `★ ${card.dataset.rating} · оценок: ${card.dataset.votes}`;
      rating.href = card.dataset.fantlab;
    }
    const summary = card.dataset.summary || '';
    $('#bd-summary').textContent = summary;
    const annotation = $('#bd-annotation');
    annotation.hidden = !summary;
    annotation.open = false; // каждая карточка открывается со свёрнутой аннотацией

    renderContents(id);

    const cur = progress[id] || '';
    $$('input[name=bd-st]', dlg).forEach((r) => { r.checked = r.value === cur; });
    dlg.showModal();
  }

  // Состав антологии или сборника: раскрываемый список, по умолчанию свёрнут.
  function renderContents(id) {
    const box = $('#bd-contents');
    const stories = CONTENTS[id] || [];
    box.hidden = !stories.length;
    box.open = false;
    if (!stories.length) { $('#bd-contents-list').replaceChildren(); return; }
    $('#bd-contents-summary').textContent = `Состав (${stories.length})`;
    $('#bd-contents-list').replaceChildren(...stories.map((s) => {
      const li = document.createElement('li');
      const title = document.createElement('b');
      title.textContent = s.title;
      li.append(title);
      if (s.orig) {
        const orig = document.createElement('i');
        orig.textContent = ` ${s.orig}`;
        li.append(orig);
      }
      const meta = [s.authors, s.kind, s.year].filter(Boolean).join(' · ');
      if (meta) {
        const m = document.createElement('span');
        m.className = 'meta';
        m.textContent = ` — ${meta}`;
        li.append(m);
      }
      if (s.rating) {
        const a = document.createElement('a');
        a.href = s.url; a.target = '_blank'; a.rel = 'noopener';
        a.textContent = `★ ${s.rating}`;
        a.title = `Оценок: ${s.votes}`;
        li.append(' ', a);
      }
      return li;
    }));
  }

  dlg.addEventListener('change', (e) => {
    if (e.target.name === 'bd-st' && dlgBook) setStatus(dlgBook, e.target.value);
  });
  for (const d of $$('dialog')) {
    d.addEventListener('click', (e) => { if (e.target === d) d.close(); }); // клик по фону
  }

  // ── аккаунт (только если сайт собран с адресом API) ─────────────────────
  const acc = {
    box: $('#account'), login: $('#login-btn'), who: $('#who'), logout: $('#logout-btn'),
    dlg: $('#auth-dialog'), form: $('#auth-form'), err: $('#auth-error'), title: $('#au-title'),
  };
  let mode = 'login';

  function renderAccount() {
    acc.login.hidden = !!user;
    acc.who.hidden = acc.logout.hidden = !user;
    if (user) acc.who.textContent = user.name || user.email;
  }

  function setMode(m) {
    mode = m;
    acc.title.textContent = m === 'login' ? 'Вход' : 'Регистрация';
    $$('[data-tab]', acc.dlg).forEach((t) => t.setAttribute('aria-selected', String(t.dataset.tab === m)));
    $$('[data-only]', acc.form).forEach((l) => { l.hidden = l.dataset.only !== m; });
    acc.form.password.autocomplete = m === 'login' ? 'current-password' : 'new-password';
    acc.err.hidden = true;
  }

  // Подтянуть прогресс с сервера; локальные отметки, которых там нет, отправить туда.
  async function syncAfterLogin() {
    const remote = (await api('/api/progress')).progress || {};
    const local = (await loadLocal()).progress;
    const pushes = [];
    for (const [id, st] of Object.entries(local)) {
      if (!remote[id]) {
        remote[id] = st;
        pushes.push(api('/api/progress/' + encodeURIComponent(id), { method: 'PUT', body: { status: st } }));
      }
    }
    await Promise.allSettled(pushes);
    progress = remote;
    clearLocal(); // на этом устройстве источник правды — аккаунт
  }

  function refreshAll() {
    $$('.card').forEach((c) => renderCard(c.dataset.id));
    renderStats();
    applyFilters();
  }

  let localProgress = {};
  const ready = loadLocal().then((rec) => { localProgress = rec.progress; unlocked = rec.unlocked; });

  // ── копия отметок в файл ────────────────────────────────────────────────
  const backup = { save: $('#backup-save'), load: $('#backup-load'), file: $('#backup-file'), msg: $('#backup-msg') };
  let msgUntil = 0; // пока показано сообщение о копии, фоновые обновления его не затирают
  const say = (text) => {
    backup.msg.textContent = text;
    msgUntil = Date.now() + 6000;
    setTimeout(renderBackupNote, 6100);
  };
  function renderBackupNote() {
    if (!backup.msg || Date.now() < msgUntil) return;
    backup.msg.textContent = user ? 'Отметки хранятся в вашем аккаунте.'
      : persisted ? 'Отметки хранятся в этом браузере и защищены от автоочистки.'
      : 'Отметки хранятся только в этом браузере — сохраните копию.';
  }

  backup.save.addEventListener('click', () => {
    const rec = { app: 'wahbook', v: 1, exported: new Date().toISOString(), progress, unlocked };
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([JSON.stringify(rec, null, 2)], { type: 'application/json' }));
    a.download = `wahbook-${rec.exported.slice(0, 10)}.json`;
    document.body.append(a);
    a.click();
    a.remove();
    setTimeout(() => URL.revokeObjectURL(a.href), 2000);
    say(`Копия сохранена: отметок — ${Object.keys(progress).length}.`);
  });
  backup.load.addEventListener('click', () => backup.file.click());
  backup.file.addEventListener('change', async () => {
    const f = backup.file.files[0];
    backup.file.value = '';
    if (!f) return;
    try {
      const raw = JSON.parse(await f.text());
      const incoming = cleanProgress(raw.progress ?? raw);
      const known = new Set($$('.card').map((c) => c.dataset.id));
      unlocked = mergeUnlocked(unlocked, cleanUnlocked(raw.unlocked)); // сначала полученное: оно не должно объявляться заново
      let n = 0;
      for (const [id, st] of Object.entries(incoming)) {
        if (known.has(id) && progress[id] !== st) { setStatus(id, st); n++; }
      }
      say(`Загружено отметок из копии: ${n}.`);
    } catch {
      say('Не удалось прочитать файл: нужна копия, сохранённая на этом сайте.');
    }
  });

  // ── достижения ──────────────────────────────────────────────────────────
  // Правила разобраны при сборке сайта (internal/achievements): здесь только подсчёт отмеченных книг.
  const ACH = (() => { try { return JSON.parse($('#ach-data')?.textContent || 'null'); } catch { return null; } })();
  const achDialog = $('#ach-dialog');
  let achFilter = 'all';

  const readCount = () => Object.values(progress).filter((s) => s === 'read').length;

  // сколько засчитано достижению: см. комментарий к Achievement в Go
  function achProgress(a) {
    const has = (id) => progress[id] === a.status;
    if (a.mode === 'cover') return a.groups.filter((g) => g.some(has)).length;
    if (a.mode === 'best') return Math.max(0, ...a.groups.map((g) => g.filter(has).length));
    return a.groups[0].filter(has).length;
  }
  const levelsGot = (a) => a.levels.filter((_, i) => unlocked[`${a.id}:${i + 1}`]).length;
  const rankGot = () => ACH.ranks.reduce((idx, _, i) => (unlocked[`rank:${i}`] ? i : idx), -1);

  function evaluateAchievements({ silent = false } = {}) {
    if (!ACH) return;
    const gained = [];
    const unlock = (key, what) => {
      if (!unlocked[key]) { unlocked[key] = Date.now(); gained.push(what); }
    };
    for (const a of ACH.achievements) {
      const p = achProgress(a);
      a.levels.forEach((l, i) => { if (p >= l.need) unlock(`${a.id}:${i + 1}`, { a, level: i + 1 }); });
    }
    const n = readCount();
    ACH.ranks.forEach((r, i) => { if (n >= r.at) unlock(`rank:${i}`, { rank: r }); });

    if (gained.length) {
      if (!user) saveLocal(); // с аккаунтами даты получения пока не синхронизируются
      if (!silent) gained.forEach(announce);
    }
    renderAchButton();
    if (achDialog.open) renderAchievements();
  }

  const MEDAL = (tier) => `<svg viewBox="0 0 48 56" class="medal medal-${tier}" aria-hidden="true">
    <path class="rib" d="M13 0h9l6 21-9 4zM26 0h9L30 25l-9-4z"/><circle class="disc" cx="24" cy="36" r="17"/>
    <circle class="ring" cx="24" cy="36" r="12"/><path class="star" d="M24 27.5l2.7 5.6 6.1.9-4.4 4.3 1 6.1-5.4-2.9-5.4 2.9 1-6.1-4.4-4.3 6.1-.9z"/></svg>`;

  function announce(g) {
    if (g.rank) return toast('Новое звание', g.rank.title, 'gold');
    const many = g.a.levels.length > 1;
    toast(g.a.secret ? 'Секретное достижение!' : 'Достижение получено',
      g.a.title + (many ? ` · уровень ${g.level} из ${g.a.levels.length}` : ''), g.a.levels[g.level - 1].tier);
  }

  function toast(kicker, text, tier) {
    const box = $('#toasts');
    if (!box) return;
    const el = document.createElement('div');
    el.className = 'toast';
    el.innerHTML = MEDAL(tier);
    const body = document.createElement('div');
    const k = document.createElement('small');
    k.textContent = kicker;
    const t = document.createElement('strong');
    t.textContent = text;
    body.append(k, t);
    el.append(body);
    box.append(el);
    while (box.children.length > 4) box.firstChild.remove();
    setTimeout(() => el.classList.add('out'), 5200);
    setTimeout(() => el.remove(), 5700);
  }

  function renderAchButton() {
    const btn = $('#ach-btn');
    if (!ACH || !btn) return;
    btn.hidden = false;
    const ri = rankGot();
    $('#ach-rank').textContent = ri >= 0 ? ACH.ranks[ri].title : 'Достижения';
    btn.title = ri >= 0 ? `Достижения · звание: ${ACH.ranks[ri].title}` : 'Достижения и звания';
    $('#ach-count').textContent = `${ACH.achievements.filter((a) => levelsGot(a) > 0).length}/${ACH.achievements.length}`;
  }

  const fmtDate = (ms) => new Date(ms).toLocaleDateString('ru-RU');

  function achCard(a) {
    const got = levelsGot(a);
    const hidden = a.secret && got === 0;
    const p = achProgress(a);
    const li = document.createElement('li');
    li.className = 'ach' + (got ? ' got' : '') + (got === a.levels.length ? ' done' : '');

    const medal = document.createElement('div');
    medal.className = 'ach-medal';
    medal.innerHTML = MEDAL(got ? a.levels[got - 1].tier : 'locked');

    const body = document.createElement('div');
    body.className = 'ach-body';
    const h = document.createElement('h4');
    h.textContent = hidden ? '???' : a.title;
    const d = document.createElement('p');
    d.textContent = hidden ? 'Секретное достижение: откроется само, когда вы его заработаете.' : a.description;
    body.append(h, d);

    if (!hidden) {
      const next = a.levels[got]; // первый ещё не полученный уровень
      const pips = document.createElement('div');
      pips.className = 'pips';
      a.levels.forEach((l, i) => {
        const pip = document.createElement('i');
        pip.className = `pip pip-${l.tier}` + (i < got ? ' on' : '');
        pip.title = `Уровень ${i + 1}: ${l.need}`;
        pips.append(pip);
      });
      const state = document.createElement('div');
      state.className = 'ach-state';
      if (next) {
        const bar = document.createElement('div');
        bar.className = 'bar';
        const fill = document.createElement('i');
        fill.style.width = Math.min(100, (p / next.need) * 100) + '%';
        bar.append(fill);
        const txt = document.createElement('span');
        txt.textContent = `${Math.min(p, next.need)} / ${next.need}`;
        state.append(bar, txt);
      } else {
        const txt = document.createElement('span');
        txt.textContent = `Все уровни получены · ${fmtDate(unlocked[`${a.id}:${a.levels.length}`])}`;
        state.append(txt);
      }
      body.append(pips, state);
    }
    li.append(medal, body);
    return li;
  }

  function renderAchievements() {
    const n = readCount();
    const ri = rankGot();
    const next = ACH.ranks[ri + 1];
    $('#ach-rank-title').textContent = ri >= 0 ? ACH.ranks[ri].title : 'Без звания';
    const from = ri >= 0 ? ACH.ranks[ri].at : 0;
    $('#ach-rank-bar').style.width = (next ? Math.max(0, Math.min(1, (n - from) / (next.at - from))) * 100 : 100) + '%';
    $('#ach-rank-text').textContent = next
      ? `Прочитано книг: ${n}. До звания «${next.title}» — ещё ${Math.max(0, next.at - n)}.`
      : `Прочитано книг: ${n}. Высшее звание получено.`;
    $('#ach-summary').textContent =
      `Получено достижений: ${ACH.achievements.filter((a) => levelsGot(a) > 0).length} из ${ACH.achievements.length}`;
    $$('.ach-filters button').forEach((b) => b.setAttribute('aria-pressed', String(b.dataset.f === achFilter)));

    const groups = new Map();
    for (const a of ACH.achievements) {
      const got = levelsGot(a);
      if (achFilter === 'done' && !got) continue;
      if (achFilter === 'todo' && got === a.levels.length) continue;
      if (!groups.has(a.group)) groups.set(a.group, []);
      groups.get(a.group).push(a);
    }
    const list = $('#ach-list');
    list.replaceChildren();
    if (!groups.size) {
      const p = document.createElement('p');
      p.className = 'empty';
      p.textContent = achFilter === 'done' ? 'Пока ничего не получено — отметьте первую прочитанную книгу.' : 'Все достижения получены.';
      list.append(p);
    }
    for (const [name, items] of groups) {
      const sec = document.createElement('section');
      const h = document.createElement('h3');
      h.textContent = name;
      const ul = document.createElement('ul');
      ul.className = 'ach-grid';
      items.forEach((a) => ul.append(achCard(a)));
      sec.append(h, ul);
      list.append(sec);
    }
  }

  if (ACH) {
    $('#ach-btn').addEventListener('click', () => { renderAchievements(); achDialog.showModal(); });
    $$('.ach-filters button').forEach((b) => b.addEventListener('click', () => { achFilter = b.dataset.f; renderAchievements(); }));
    renderAchButton();
  }

  async function enterSession(u) {
    user = u;
    renderAccount();
    await syncAfterLogin();
    refreshAll();
  }

  if (CFG.api) {
    acc.box.hidden = false;
    setMode('login');

    acc.login.addEventListener('click', () => acc.dlg.showModal());
    $$('[data-tab]', acc.dlg).forEach((t) => t.addEventListener('click', () => setMode(t.dataset.tab)));

    acc.form.addEventListener('submit', async (e) => {
      e.preventDefault();
      acc.err.hidden = true;
      const f = acc.form;
      try {
        const body = { email: f.email.value.trim(), password: f.password.value };
        if (mode === 'register') body.name = f.name.value.trim();
        const res = await api('/api/' + mode, { method: 'POST', body });
        token = res.token;
        ls.set(LS_TOKEN, token);
        f.password.value = '';
        acc.dlg.close();
        await enterSession(res.user);
      } catch (err) {
        acc.err.textContent = err.message;
        acc.err.hidden = false;
      }
    });

    acc.logout.addEventListener('click', async () => {
      try { await api('/api/logout', { method: 'POST' }); } catch { /* токен мог истечь */ }
      token = null; user = null; progress = {};
      ls.del(LS_TOKEN);
      clearLocal();
      renderAccount();
      refreshAll();
    });

    if (token) {
      ready
        .then(() => api('/api/me'))
        .then((r) => enterSession(r.user))
        .catch((err) => {
          if (err.status === 401) { token = null; ls.del(LS_TOKEN); }
        });
    }
  }

  // ── старт ───────────────────────────────────────────────────────────────
  refreshAll();
  ready.then(() => {
    if (!user) { progress = localProgress; refreshAll(); }
    renderBackupNote();
    evaluateAchievements({ silent: true }); // отметки, сделанные раньше, засчитываем без уведомлений
  });
  navigator.storage?.persisted?.().then((p) => { persisted = p || null; renderBackupNote(); }).catch(() => {});
})();
