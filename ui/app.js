// Copyright 2026 Kirill Scherba. All rights reserved.
// Use of this source code is governed by an MIT-style license.
//
// The screen: the collections of the installation, every file of one with the state of the search,
// and a preview that shows a picture, the text the index holds, or the description of a picture.
//
// The files are not served by this module: they come from the installation's own /api/thumb and
// /api/file, which work at the machine and through the relay alike. The module only knows where the
// installation lives and carries the person's token to it (docs/MODULES.md).

const $ = (id) => document.getElementById(id)

const STATE = {
  indexed: { label: 'в поиске', cls: 'state-indexed', color: 'var(--ok)' },
  skipped: { label: 'пропущено', cls: 'state-skipped', color: 'var(--warn)' },
  excluded: { label: 'убрано', cls: 'state-excluded', color: 'var(--bad)' },
  unscanned: { label: 'не сканировано', cls: 'state-unscanned', color: 'var(--dim)' },
}

const REASON = {
  unsupported: 'формат не поддерживается',
  audio: 'запись: текст добавляет расшифровка',
  no_text: 'текста не найдено',
  unchanged: 'не изменился',
  excluded: 'исключён вами',
}

const IMAGE = /\.(png|jpe?g|gif|webp|bmp|tiff?|avif|heic)$/i
const PAGE_LIMIT = 400

const state = {
  collections: [],
  bucket: '',
  files: [],
  counts: {},
  loadedAt: 0,
  filterState: '',
  folder: '',
  q: '',
  sort: 'name',
  view: 'list',
  selected: null,
  preview: null,
  media: { base: '', token: '' },
  person: { email: '', rights: [], may_write: true },
  shown: PAGE_LIMIT,
  loading: false,
}

// ─── small helpers ──────────────────────────────────────────────────────

function esc(text) {
  return String(text == null ? '' : text)
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

function humanSize(n) {
  if (!n) return '—'
  const units = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ']
  let v = n, i = 0
  while (v >= 1024 && i < units.length - 1) { v /= 1024; i += 1 }
  return (v < 10 && i > 0 ? v.toFixed(1) : Math.round(v)) + ' ' + units[i]
}

function humanDate(iso) {
  if (!iso) return '—'
  const d = new Date(iso)
  if (isNaN(d)) return '—'
  const pad = (x) => String(x).padStart(2, '0')
  return pad(d.getDate()) + '.' + pad(d.getMonth() + 1) + '.' + String(d.getFullYear()).slice(2) +
    ' ' + pad(d.getHours()) + ':' + pad(d.getMinutes())
}

function stateOf(doc) {
  return STATE[doc.state] || { label: doc.state || '—', cls: 'state-unscanned', color: 'var(--dim)' }
}

// kindLabel is the word in the corner of a card without a picture: "pdf", "md", "web".
function kindLabel(doc) {
  if (doc.kind === 'link') return 'web'
  const ext = ((doc.name || doc.path || '').match(/\.([a-z0-9]+)$/i) || [])[1]
  return ext ? ext.toLowerCase() : 'файл'
}

function isImage(doc) {
  return doc.kind === 'image' || IMAGE.test(doc.name || doc.path || '')
}

// The installation's own endpoints: a picture is asked of it, not of this module, because the module
// has no files of its own (api.go, media.go of anytext-gallery).
function installBase() {
  const b = new URL(document.baseURI)
  b.pathname = b.pathname.replace(/api\/module\/[^/]+\/?$/, '').replace(/\/+$/, '')
  return (b.origin + b.pathname).replace(/\/+$/, '')
}

function mediaBase() {
  return state.media.base || installBase()
}

function mediaQuery(params) {
  if (state.media.token) params.set('mt', state.media.token)
  return params
}

function thumbURL(doc, max) {
  const q = mediaQuery(new URLSearchParams({ bucket: state.bucket, doc: doc.key, max: String(max || 400) }))
  return mediaBase() + '/api/thumb?' + q.toString()
}

function fileURL(doc, download) {
  const q = mediaQuery(new URLSearchParams({ bucket: state.bucket, doc: doc.key }))
  return mediaBase() + '/api/file?' + q.toString() + (download ? '&download=1' : '')
}

let toastTimer = 0
function toast(text, kind) {
  const el = $('toast')
  el.textContent = text
  el.className = 'toast ' + (kind || '')
  el.hidden = false
  clearTimeout(toastTimer)
  toastTimer = setTimeout(() => { el.hidden = true }, kind === 'bad' ? 7000 : 3500)
}

function progress(on) {
  $('progress').hidden = !on
}

// ─── talking to the module ──────────────────────────────────────────────

async function api(path, options) {
  const res = await fetch(path, options)
  const type = res.headers.get('content-type') || ''
  if (!type.includes('json')) {
    throw new Error(res.status + ' ' + res.statusText)
  }
  const data = await res.json()
  if (data && data.error) throw new Error(data.error)
  return data
}

async function post(path, body) {
  return api(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body || {}),
  })
}

// A permit for pictures lives ten minutes; one 401 is what its end looks like. The screen asks for a
// new one and reloads the picture that asked (anytext-gallery, 23.11.2026).
async function refreshPermit(doc) {
  try {
    const media = await api('api/signed?bucket=' + encodeURIComponent(state.bucket) +
      '&doc=' + encodeURIComponent(doc))
    if (media && media.token) {
      state.media = media
      return true
    }
  } catch (e) { /* the picture stays broken; the reason is said by the next action */ }
  return false
}

// ─── loading ────────────────────────────────────────────────────────────

async function loadState() {
  progress(true)
  try {
    const data = await api('api/state')
    state.collections = data.collections || []
    state.media = data.media || { base: '', token: '' }
    state.person = data.person || state.person
    drawCollections()
    const first = state.collections[0]
    if (first) await openCollection(first.id)
    else $('summary').textContent = 'в этой установке нет коллекций'
  } catch (e) {
    toast('установка не отвечает: ' + e.message, 'bad')
    $('summary').textContent = 'установка не отвечает'
  } finally {
    progress(false)
  }
}

async function openCollection(id) {
  if (!id) return
  state.bucket = id
  const bucket = state.collections.find((b) => b.id === id)
  $('collection-name').textContent = bucket ? bucket.name : id
  state.folder = ''
  state.selected = null
  state.shown = PAGE_LIMIT
  closePreview()
  await reload()
  drawCollections()
}

async function reload() {
  if (!state.bucket) return
  progress(true)
  state.loading = true
  try {
    const data = await api('api/files?bucket=' + encodeURIComponent(state.bucket))
    state.files = (data.documents || []).slice()
    state.counts = data.counts || {}
    state.loadedAt = Date.now()
    drawFolders()
    const wanted = new URLSearchParams(location.search).get('doc')
    if (wanted) {
      const doc = state.files.find((d) => d.key === wanted)
      if (doc) setTimeout(() => select(doc.key), 0)
    }
  } catch (e) {
    toast('не удалось прочитать коллекцию: ' + e.message, 'bad')
  } finally {
    state.loading = false
    progress(false)
    drawAll()
  }
}

// ─── drawing the left column ────────────────────────────────────────────

function drawCollections() {
  const box = $('collections')
  box.innerHTML = state.collections.map((b) => {
    const on = b.id === state.bucket ? ' on' : ''
    return '<div class="collection' + on + '" data-id="' + esc(b.id) + '" title="' + esc(b.description || b.id) + '">' +
      '<span class="name">' + esc(b.name || b.id) + '</span>' +
      '</div>'
  }).join('') || '<div class="empty">коллекций нет</div>'
  box.querySelectorAll('.collection').forEach((el) => {
    el.onclick = () => openCollection(el.dataset.id)
  })

  const filters = [
    { id: '', label: 'все файлы', color: 'var(--accent)', n: state.files.length },
    { id: 'indexed', label: 'в поиске', color: 'var(--ok)', n: state.counts.indexed || 0 },
    { id: 'skipped', label: 'пропущено', color: 'var(--warn)', n: state.counts.skipped || 0 },
    { id: 'excluded', label: 'убрано вами', color: 'var(--bad)', n: state.counts.excluded || 0 },
    { id: 'unscanned', label: 'не сканировано', color: 'var(--dim)', n: state.counts.unscanned || 0 },
  ]
  const fbox = $('filters')
  fbox.innerHTML = filters.map((f) => {
    const on = f.id === state.filterState ? ' on' : ''
    return '<div class="filter' + on + '" data-state="' + f.id + '">' +
      '<span class="dot" style="background:' + f.color + '"></span>' +
      '<span>' + esc(f.label) + '</span><span class="n">' + f.n + '</span></div>'
  }).join('')
  fbox.querySelectorAll('.filter').forEach((el) => {
    el.onclick = () => {
      state.filterState = el.dataset.state
      state.shown = PAGE_LIMIT
      drawAll()
    }
  })
}

function drawFolders() {
  const counts = new Map()
  for (const doc of state.files) {
    const folder = doc.folder || ''
    counts.set(folder, (counts.get(folder) || 0) + 1)
  }
  const rows = [...counts.entries()].sort((a, b) => a[0].localeCompare(b[0], 'ru'))
  const box = $('folders')
  box.innerHTML = rows.map(([folder, n]) => {
    const on = folder === state.folder ? ' on' : ''
    const short = folder.split(/[\\/]/).filter(Boolean).slice(-1)[0] || folder
    return '<div class="folder' + on + '" data-folder="' + esc(folder) + '" title="' + esc(folder) + '">' +
      esc(short) + ' <span class="n">' + n + '</span></div>'
  }).join('') || '<div class="empty">папок нет</div>'
  box.querySelectorAll('.folder').forEach((el) => {
    el.onclick = () => {
      state.folder = state.folder === el.dataset.folder ? '' : el.dataset.folder
      state.shown = PAGE_LIMIT
      drawFolders()
      drawAll()
    }
  })
}

// ─── drawing the list ───────────────────────────────────────────────────

function visibleFiles() {
  const q = state.q.trim().toLowerCase()
  let list = state.files.filter((d) => {
    if (state.filterState && d.state !== state.filterState) return false
    if (state.folder && d.folder !== state.folder) return false
    if (q && !((d.name || '') + ' ' + (d.path || '')).toLowerCase().includes(q)) return false
    return true
  })
  const by = {
    name: (a, b) => (a.name || '').localeCompare(b.name || '', 'ru'),
    size: (a, b) => (b.size || 0) - (a.size || 0),
    date: (a, b) => String(b.mod_time || '').localeCompare(String(a.mod_time || '')),
    state: (a, b) => String(a.state || '').localeCompare(String(b.state || '')) ||
      (a.name || '').localeCompare(b.name || '', 'ru'),
  }
  return list.sort(by[state.sort] || by.name)
}

function drawAll() {
  const list = visibleFiles()
  const shown = list.slice(0, state.shown)
  $('summary').textContent = list.length + ' из ' + state.files.length + ' файлов' +
    (state.filterState ? ' · фильтр: ' + stateOf({ state: state.filterState }).label : '') +
    (state.folder ? ' · папка: ' + state.folder : '')

  const box = $('list')
  if (!state.files.length) {
    box.className = 'list'
    box.innerHTML = '<div class="empty">' + (state.loading ? 'читаю коллекцию…' : 'в этой коллекции нет файлов') + '</div>'
    $('more').hidden = true
    return
  }
  if (!list.length) {
    box.className = 'list'
    box.innerHTML = '<div class="empty">ничего не подходит под фильтр</div>'
    $('more').hidden = true
    return
  }

  if (state.view === 'grid') {
    box.className = 'grid'
    box.innerHTML = shown.map((doc) => cardHTML(doc)).join('')
  } else {
    box.className = 'list'
    box.innerHTML = shown.map((doc) => rowHTML(doc)).join('')
  }

  box.querySelectorAll('[data-key]').forEach((el) => {
    el.onclick = () => select(el.dataset.key)
  })

  const more = $('more')
  if (list.length > shown.length) {
    more.hidden = false
    more.innerHTML = 'показано ' + shown.length + ' из ' + list.length +
      ' <button class="btn" id="more-btn">показать ещё ' + Math.min(PAGE_LIMIT, list.length - shown.length) + '</button>'
    $('more-btn').onclick = () => { state.shown += PAGE_LIMIT; drawAll() }
  } else {
    more.hidden = true
  }
}

function rowHTML(doc) {
  const st = stateOf(doc)
  const on = state.selected === doc.key ? ' on' : ''
  const reason = doc.reason ? ' · ' + (REASON[doc.reason] || doc.reason) : ''
  return '<div class="row' + on + '" data-key="' + esc(doc.key) + '" role="listitem">' +
    '<span class="dot" style="background:' + st.color + '"></span>' +
    '<span class="chip ' + st.cls + '">' + esc(st.label) + '</span>' +
    '<span class="name">' + esc(doc.name) + '<span class="sub">' + esc(doc.folder) + reason + '</span></span>' +
    '<span class="size">' + humanSize(doc.size) + '</span>' +
    '<span class="date">' + humanDate(doc.mod_time) + '</span>' +
    '</div>'
}

function cardHTML(doc) {
  const st = stateOf(doc)
  const on = state.selected === doc.key ? ' on' : ''
  const thumb = isImage(doc)
    ? '<img loading="lazy" src="' + thumbURL(doc, 320) + '" alt="" data-permit="' + esc(doc.key) + '" />'
    : '<span class="kind">' + (doc.kind === 'link' ? '🔗' : '📄') + '</span>'
  return '<div class="card' + on + '" data-key="' + esc(doc.key) + '" role="listitem">' +
    '<div class="thumb">' + thumb + '</div>' +
    '<div class="meta"><span class="name" title="' + esc(doc.name) + '">' + esc(doc.name) + '</span>' +
    '<span class="sub"><span class="dot" style="width:7px;height:7px;border-radius:50%;background:' + st.color + '"></span>' +
    esc(st.label) + ' · ' + humanSize(doc.size) + '</span></div>' +
    '</div>'
}

// A picture that comes back 401 means the permit is over: ask for a new one and reload this picture
// once (an <img> carries no token, so the permit travels in its address).
document.addEventListener('error', async (e) => {
  const img = e.target
  if (!img || img.tagName !== 'IMG' || !img.dataset.permit || img.dataset.retried) return
  img.dataset.retried = '1'
  if (await refreshPermit(img.dataset.permit)) {
    const doc = state.files.find((d) => d.key === img.dataset.permit)
    if (doc) img.src = img.src.replace(/mt=[^&]*/, 'mt=' + encodeURIComponent(state.media.token))
  }
}, true)

// ─── the preview ────────────────────────────────────────────────────────

function select(key) {
  const doc = state.files.find((d) => d.key === key)
  if (!doc) return
  state.selected = key
  drawAll()
  openPreview(doc)
}

function closePreview() {
  state.preview = null
  $('preview').hidden = true
  $('preview-media').innerHTML = ''
  $('preview-body').innerHTML = ''
  $('preview-actions').innerHTML = ''
  document.querySelector('.shell').classList.remove('with-preview')
}

async function openPreview(doc) {
  const st = stateOf(doc)
  state.preview = doc
  $('preview').hidden = false
  document.querySelector('.shell').classList.add('with-preview')
  $('preview-title').textContent = doc.name
  $('preview-actions').innerHTML = ''
  $('preview-media').innerHTML = isImage(doc)
    ? '<img src="' + thumbURL(doc, 900) + '" alt="" data-permit="' + esc(doc.key) + '" data-big="1" />'
    : ''

  const meta =
    '<dl class="meta">' +
    '<dt>тип</dt><dd>' + esc(doc.kind || '—') + '</dd>' +
    '<dt>размер</dt><dd>' + humanSize(doc.size) + '</dd>' +
    '<dt>изменён</dt><dd>' + humanDate(doc.mod_time) + '</dd>' +
    '<dt>папка</dt><dd>' + esc(doc.folder || '—') + '</dd>' +
    '<dt>состояние</dt><dd class="' + st.cls + '">' + esc(st.label) +
    (doc.reason ? ' — ' + esc(REASON[doc.reason] || doc.reason) : '') + '</dd>' +
    '</dl>'
  $('preview-body').innerHTML = meta + '<div class="hint" style="margin-top:14px;color:var(--dim);font-size:12.5px">читаю текст из индекса…</div>'

  const buttons = []
  buttons.push(button('Открыть', () => window.open(fileURL(doc), '_blank', 'noopener')))
  buttons.push(button('Скачать', () => {
    const a = document.createElement('a')
    a.href = fileURL(doc, true)
    a.download = doc.name || 'file'
    document.body.appendChild(a)
    a.click()
    a.remove()
  }))
  if (doc.state === 'excluded') {
    buttons.push(button('Вернуть в поиск', () => act('api/restore', doc, 'возвращён в поиск')))
  } else {
    buttons.push(button('Убрать из поиска', () => act('api/exclude', doc, 'убран из поиска, файл на месте'), 'danger'))
  }
  if (isImage(doc)) {
    buttons.push(button('Описать моделью', () => describe(doc), 'accent'))
  }
  $('preview-actions').innerHTML = buttons.join('')

  try {
    const txt = await api('api/text?bucket=' + encodeURIComponent(doc.bucket) +
      '&doc=' + encodeURIComponent(doc.key))
    $('preview-body').innerHTML = meta + textHTML(txt)
  } catch (e) {
    $('preview-body').innerHTML = meta +
      '<div class="hint" style="margin-top:14px;color:var(--bad)">текст не прочитался: ' + esc(e.message) + '</div>'
  }
}

function button(label, onClick, kind) {
  const id = 'b' + Math.random().toString(36).slice(2)
  pending[id] = onClick
  return '<button class="btn' + (kind === 'danger' ? ' danger' : kind === 'accent' ? ' accent' : '') +
    '" data-act="' + id + '">' + esc(label) + '</button>'
}

const pending = {}
document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-act]')
  if (!el) return
  const fn = pending[el.dataset.act]
  if (fn) fn()
})

function textHTML(txt) {
  let html = ''
  if (txt.description) {
    html += '<h4>описание</h4><pre>' + esc(txt.description) + '</pre>'
  }
  if (txt.audio) {
    html += '<h4>расшифровка</h4><pre>' + esc(txt.audio) + '</pre>'
  }
  if (txt.text) {
    html += '<h4>текст в поиске' + (txt.truncated ? ' (показан не весь)' : '') + '</h4><pre>' + esc(txt.text) + '</pre>'
  }
  if (!html) {
    html = '<div class="hint" style="margin-top:14px;color:var(--dim);font-size:12.5px">' +
      'в индексе нет текста этого файла</div>'
  }
  return html
}

// ─── actions ────────────────────────────────────────────────────────────

async function act(path, doc, done) {
  progress(true)
  try {
    await post(path, { bucket: doc.bucket, doc: doc.key })
    toast(doc.name + ': ' + done, 'good')
    await reload()
    const again = state.files.find((d) => d.key === doc.key)
    if (again) openPreview(again)
    else closePreview()
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

async function describe(doc) {
  progress(true)
  toast('описываю картинку моделью…')
  try {
    await post('api/describe', { bucket: doc.bucket, doc: doc.key })
    toast(doc.name + ': описан моделью', 'good')
    await reload()
    const again = state.files.find((d) => d.key === doc.key)
    if (again) openPreview(again)
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

async function scan() {
  if (!state.bucket) return
  const bucket = state.collections.find((b) => b.id === state.bucket)
  if (!confirm('Проиндексировать новое и изменённое в коллекции «' + (bucket ? bucket.name : state.bucket) + '»?')) return
  progress(true)
  toast('индексирую коллекцию…')
  try {
    const res = await post('api/scan', { bucket: state.bucket, mode: 'changed' })
    toast('просканировано ' + (res.scanned || 0) + ' · добавлено ' + (res.ingested || 0) +
      ' · без изменений ' + (res.unchanged || 0) + ' · не распознано ' + (res.skipped || 0), 'good')
    await reload()
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

// ─── wiring ─────────────────────────────────────────────────────────────

$('btn-refresh').onclick = () => reload()
$('btn-scan').onclick = () => scan()
$('preview-close').onclick = () => { closePreview(); drawAll() }

$('q').oninput = (e) => {
  state.q = e.target.value
  state.shown = PAGE_LIMIT
  drawAll()
}

document.addEventListener('keydown', (e) => {
  if (e.key === '/' && document.activeElement !== $('q')) {
    e.preventDefault()
    $('q').focus()
  }
  if (e.key === 'Escape') {
    if (document.activeElement === $('q') && state.q) {
      $('q').value = ''
      state.q = ''
      drawAll()
    } else if (state.preview) {
      closePreview()
      drawAll()
    }
  }
})

document.querySelectorAll('#sort button').forEach((el) => {
  el.onclick = () => {
    state.sort = el.dataset.sort
    document.querySelectorAll('#sort button').forEach((b) => b.classList.toggle('on', b === el))
    drawAll()
  }
})
document.querySelectorAll('#view button').forEach((el) => {
  el.onclick = () => {
    state.view = el.dataset.view
    localStorage.setItem('explorer.view', state.view)
    document.querySelectorAll('#view button').forEach((b) => b.classList.toggle('on', b === el))
    drawAll()
  }
})

// A theme and a view can be asked for in the address: ?theme=dark, ?view=grid. The choice is kept, so
// a person who prefers one does not repeat it on every open.
function startOptions() {
  const params = new URLSearchParams(location.search)
  const stored = { theme: localStorage.getItem('explorer.theme'), view: localStorage.getItem('explorer.view') }
  const theme = params.get('theme') || stored.theme
  const view = params.get('view') || stored.view
  if (theme === 'dark' || theme === 'light') {
    document.documentElement.dataset.theme = theme
    localStorage.setItem('explorer.theme', theme)
  }
  if (view === 'grid' || view === 'list') {
    state.view = view
    localStorage.setItem('explorer.view', view)
    document.querySelectorAll('#view button').forEach((b) => b.classList.toggle('on', b.dataset.view === view))
  }
}


// A theme and a view can be asked for in the address: ?theme=dark, ?view=grid. The choice is kept, so
// a person who prefers one does not repeat it on every open.
function startOptions() {
  const params = new URLSearchParams(location.search)
  const theme = params.get('theme') || localStorage.getItem('explorer.theme')
  const view = params.get('view') || localStorage.getItem('explorer.view')
  if (theme === 'dark' || theme === 'light') {
    document.documentElement.dataset.theme = theme
    localStorage.setItem('explorer.theme', theme)
  }
  if (view === 'grid' || view === 'list') {
    state.view = view
    localStorage.setItem('explorer.view', view)
    document.querySelectorAll('#view button').forEach((b) => b.classList.toggle('on', b.dataset.view === view))
  }
}

// The last line of the file: nothing else waits for it.
startOptions()

loadState()
