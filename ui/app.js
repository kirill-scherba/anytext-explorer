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
  picked: new Set(),
  lastPicked: -1,
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
  // A page from the internet is not a picture, whatever its address ends with: a link has no bytes of
  // its own to show.
  if (doc.kind === 'link') return false
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
  state.picked.clear()
  state.lastPicked = -1
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
    // The folders are shown with the collection: "why are there three files here" is answered by the
    // path, and a collection is a name over one or more folders of the disk.
    const folders = (b.folders || []).join(', ')
    const title = [b.description, folders].filter(Boolean).join(' · ')
    return '<div class="collection' + on + '" data-id="' + esc(b.id) + '" title="' + esc(title) + '">' +
      '<span class="name">' + esc(b.name || b.id) + '</span>' +
      (on && folders ? '<span class="path">' + esc(folders) + '</span>' : '') +
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
  drawBulk(list)
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
    box.className = 'list grid'
    box.innerHTML = shown.map((doc, i) => cardHTML(doc, i)).join('')
  } else {
    box.className = 'list'
    box.innerHTML = shown.map((doc, i) => rowHTML(doc, i)).join('')
  }

  box.querySelectorAll('[data-key]').forEach((el) => {
    el.onclick = (ev) => {
      if (ev.target.closest('[data-tick]')) return
      select(el.dataset.key)
    }
  })
  box.querySelectorAll('[data-tick]').forEach((el) => {
    el.onclick = (ev) => {
      ev.stopPropagation()
      const key = el.dataset.tick
      const index = shown.findIndex((d) => d.key === key)
      if (ev.shiftKey && state.lastPicked >= 0) {
        const [from, to] = [state.lastPicked, index].sort((a, b) => a - b)
        for (let i = from; i <= to; i++) state.picked.add(shown[i].key)
      } else if (state.picked.has(key)) {
        state.picked.delete(key)
      } else {
        state.picked.add(key)
      }
      state.lastPicked = index
      drawAll()
    }
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

function rowHTML(doc, index) {
  const st = stateOf(doc)
  const on = state.selected === doc.key ? ' on' : ''
  const ticked = state.picked.has(doc.key) ? ' checked' : ''
  const reason = doc.reason ? ' · ' + (REASON[doc.reason] || doc.reason) : ''
  return '<div class="row' + on + '" data-key="' + esc(doc.key) + '" role="listitem">' +
    '<input type="checkbox" class="tick" data-tick="' + esc(doc.key) + '" data-index="' + index + '"' + ticked + ' />' +
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
    : '<span class="kind">' + esc(kindLabel(doc)) + '</span>'
  const ticked = state.picked.has(doc.key) ? ' checked' : ''
  return '<div class="card' + on + (ticked ? ' picked' : '') + '" data-key="' + esc(doc.key) + '" role="listitem">' +
    '<input type="checkbox" class="tick" data-tick="' + esc(doc.key) + '"' + ticked + ' />' +
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
  if (!img || img.tagName !== 'IMG' || !img.dataset.permit) return
  const doc = state.files.find((d) => d.key === img.dataset.permit)
  if (!img.dataset.retried) {
    // The first failure is usually the permit: it lives ten minutes, and a picture loaded from the
    // cache outlives it. Ask for a new one and try the same picture once.
    img.dataset.retried = '1'
    if (await refreshPermit(img.dataset.permit)) {
      if (doc) img.src = img.src.replace(/mt=[^&]*/, 'mt=' + encodeURIComponent(state.media.token))
      return
    }
  }
  // The picture is really not there: a card says what kind of file it is instead of a torn icon.
  const thumb = img.closest('.card .thumb')
  if (thumb) {
    thumb.innerHTML = '<span class="kind">' + esc(doc ? kindLabel(doc) : 'файл') + '</span>'
  } else {
    img.replaceWith(document.createTextNode(doc ? 'превью недоступно: ' + doc.name : 'превью недоступно'))
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
  if (doc.kind === 'text') {
    buttons.push(button('Править файл', () => editFile(doc)))
  }
  if (doc.state === 'excluded') {
    buttons.push(button('Вернуть в поиск', () => act('api/restore', doc, 'возвращён в поиск')))
  } else {
    buttons.push(button('Убрать из поиска', () => act('api/exclude', doc, 'убран из поиска, файл на месте')))
  }
  if (isImage(doc)) {
    buttons.push(button('Описать моделью', () => describe(doc), 'accent'))
  }
  buttons.push(button('Переименовать', () => renameDoc(doc)))
  buttons.push(button('Переместить', () => moveDoc(doc)))
  buttons.push(button('Удалить', () => deleteDoc(doc), 'danger'))
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
  const section = (title, kind, body) => {
    const id = 'e' + Math.random().toString(36).slice(2)
    pending[id] = () => editIndexText(state.preview, kind, body)
    // A word, not a pen: the glyph is not on every font, and an empty box reads as a broken screen.
    return '<h4>' + title + '<button class="pen" data-act="' + id + '" title="Править текст в поиске">править</button></h4>' +
      '<pre>' + esc(body) + '</pre>'
  }
  if (txt.description) {
    html += section('описание', 'description', txt.description)
  }
  if (txt.audio) {
    html += section('расшифровка', 'audio', txt.audio)
  }
  if (txt.text) {
    html += section('текст в поиске' + (txt.truncated ? ' (показан не весь)' : ''), 'text', txt.text)
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
  // Delete removes what is marked, or the file the preview shows: the key a person presses without
  // looking for a button.
  if (e.key === 'Delete' && $('modal').hidden && !state.loading) {
    e.preventDefault()
    if (state.picked.size > 0) {
      bulk('delete')
      return
    }
    const doc = state.selected ? state.files.find((d) => d.key === state.selected) : null
    if (doc) deleteDoc(doc)
    return
  }
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

// ─── the modal: one door for every question the screen asks ──────────────

let modalClose = null

function openModal(title, bodyHTML, actions) {
  $('modal-title').textContent = title
  $('modal-body').innerHTML = bodyHTML
  const foot = $('modal-foot')
  foot.innerHTML = ''
  for (const a of actions) {
    const el = document.createElement('button')
    el.className = 'btn ' + (a.kind || '')
    el.textContent = a.label
    el.onclick = () => a.onClick()
    foot.appendChild(el)
  }
  $('modal').hidden = false
  modalClose = () => { $('modal').hidden = true; $('modal-body').innerHTML = '' }
  const first = $('modal-body').querySelector('input, textarea, select')
  if (first) { first.focus(); if (first.select) first.select() }
}

function closeModal() {
  if (modalClose) modalClose()
  modalClose = null
}

$('modal-close').onclick = () => closeModal()
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape' && !$('modal').hidden) closeModal()
})

// ─── the three actions of a file: rename, move, delete ──────────────────

function renameDoc(doc) {
  openModal('Переименовать', '<p>Новое имя файла в папке ' + esc(doc.folder) + '</p>' +
    '<input id="f-name" value="' + esc(doc.name) + '" />', [
    { label: 'Отмена', onClick: () => closeModal() },
    { label: 'Переименовать', kind: 'primary', onClick: () => submitMove(doc, doc.folder, $('f-name').value) },
  ])
}

function moveDoc(doc) {
  const folders = collectionFolders()
  const options = folders.map((f) =>
    '<option value="' + esc(f) + '"' + (f === doc.folder ? ' selected' : '') + '>' + esc(f) + '</option>').join('')
  openModal('Переместить', '<p>Куда положить ' + esc(doc.name) + '</p>' +
    '<select id="f-folder">' + options + '</select>' +
    '<p>Новое имя можно изменить здесь же.</p>' +
    '<input id="f-name" value="' + esc(doc.name) + '" />', [
    { label: 'Отмена', onClick: () => closeModal() },
    { label: 'Переместить', kind: 'primary', onClick: () => submitMove(doc, $('f-folder').value, $('f-name').value) },
  ])
}

// collectionFolders answers the folders a file of this collection may live in: the collection's own
// roots and every folder the listing has shown, which is where a person wants to put a file.
function collectionFolders() {
  const out = new Set()
  const bucket = state.collections.find((b) => b.id === state.bucket)
  for (const f of (bucket && bucket.folders) || []) out.add(f)
  // A link has no folder on the disk: its "folder" is the word the screen groups links under, and a
  // file cannot be moved into a word.
  for (const d of state.files) if (d.folder && d.kind !== 'link') out.add(d.folder)
  return [...out].sort((a, b) => a.localeCompare(b, 'ru'))
}

async function submitMove(doc, folder, name) {
  closeModal()
  progress(true)
  try {
    const res = await post('api/move', { bucket: doc.bucket, doc: doc.key, folder, name })
    toast(doc.name + ': ' + (res.moved ? 'перемещён' : 'имя не изменилось'), 'good')
    if (res.moved) await scanQuiet()
    state.selected = null
    closePreview()
    await reload()
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

function deleteDoc(doc) {
  openModal('Удалить файл', '<p>Файл будет удалён с диска, а документ — из поиска. Это не отмена.</p>' +
    '<p style="color:var(--text)"><b>' + esc(doc.name) + '</b><br />' + esc(doc.folder) + '</p>', [
    { label: 'Отмена', onClick: () => closeModal() },
    { label: 'Удалить', kind: 'danger', onClick: () => submitDelete(doc) },
  ])
}

async function submitDelete(doc) {
  closeModal()
  progress(true)
  try {
    await post('api/delete', { bucket: doc.bucket, doc: doc.key })
    toast(doc.name + ': удалён', 'good')
    state.selected = null
    state.picked.delete(doc.key)
    closePreview()
    await reload()
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

// ─── editing: the text the search holds, and the file itself ─────────────

function editIndexText(doc, kind, current) {
  const titles = { text: 'Текст в поиске', description: 'Описание', audio: 'Расшифровка' }
  openModal('Править: ' + (titles[kind] || kind), '<p>' + esc(doc.name) + '</p>' +
    '<textarea id="f-text">' + esc(current) + '</textarea>' +
    '<p>Эти слова будет находить поиск. Сам файл не меняется.</p>', [
    { label: 'Отмена', onClick: () => closeModal() },
    { label: 'Сохранить', kind: 'primary', onClick: () => submitText(doc, kind, $('f-text').value) },
  ])
}

async function submitText(doc, kind, text) {
  closeModal()
  progress(true)
  try {
    await post('api/save', { bucket: doc.bucket, doc: doc.key, kind, text })
    toast(doc.name + ': сохранено', 'good')
    await reload()
    const again = state.files.find((d) => d.key === doc.key)
    if (again) openPreview(again)
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

// editFile opens the file itself: a markdown note, a text file, a draft. The window edits the index
// text the same way; this one writes the file, because a file manager is expected to.
async function editFile(doc) {
  progress(true)
  try {
    const raw = await api('api/raw?bucket=' + encodeURIComponent(doc.bucket) + '&doc=' + encodeURIComponent(doc.key))
    openModal('Править файл: ' + doc.name, '<textarea id="f-text">' + esc(raw.text) + '</textarea>' +
      '<p>Сохраняется в файл ' + esc(doc.path || doc.name) + ' и переиндексируется.</p>', [
      { label: 'Отмена', onClick: () => closeModal() },
      { label: 'Сохранить', kind: 'primary', onClick: () => submitFile(doc, $('f-text').value) },
    ])
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

async function submitFile(doc, text) {
  closeModal()
  progress(true)
  try {
    await post('api/save', { bucket: doc.bucket, doc: doc.key, kind: 'file', text })
    toast(doc.name + ': файл сохранён', 'good')
    await scanQuiet()
    await reload()
    const again = state.files.find((d) => d.key === doc.key)
    if (again) openPreview(again)
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

// scanQuiet indexes the new and changed files without a question: the person already said what he
// wants by moving or saving a file.
async function scanQuiet() {
  try {
    await post('api/scan', { bucket: state.bucket, mode: 'changed' })
  } catch (e) { /* the answer of the move is what matters; a failed scan shows on the next refresh */ }
}

// ─── selection and group actions ────────────────────────────────────────

function pickedDocs() {
  return state.files.filter((d) => state.picked.has(d.key))
}

function drawBulk(list) {
  const n = state.picked.size
  $('bulkbar').hidden = n === 0
  if (n) $('bulk-count').textContent = 'выбрано ' + n
  list = list || visibleFiles()
  const box = $('pick-all')
  const all = list.length > 0 && list.every((d) => state.picked.has(d.key))
  box.checked = all
  box.indeterminate = !all && n > 0
  box.disabled = list.length === 0
}

const BULK_DONE = {
  exclude: 'убрано из поиска',
  restore: 'возвращено в поиск',
  describe: 'описано моделью',
  move: 'перемещено',
  delete: 'удалено с диска',
}

async function bulk(action) {
  if (action === 'clear') {
    state.picked.clear()
    state.lastPicked = -1
    drawAll()
    return
  }
  const docs = pickedDocs()
  if (!docs.length) return
  if (action === 'zip') {
    await downloadZip(docs)
    return
  }
  // Two actions ask a question first: where to put the files, and whether to really erase them.
  if (action === 'move') {
    const folders = collectionFolders()
    openModal('Переместить ' + docs.length + ' файлов',
      '<p>Куда положить выбранные файлы. Имена останутся прежними.</p>' +
      '<select id="f-folder">' + folders.map((f) =>
        '<option value="' + esc(f) + '">' + esc(f) + '</option>').join('') + '</select>', [
        { label: 'Отмена', onClick: () => closeModal() },
        { label: 'Переместить', kind: 'primary', onClick: () => runBulk('move', $('f-folder').value) },
      ])
    return
  }
  if (action === 'delete') {
    openModal('Удалить ' + docs.length + ' файлов',
      '<p>Файлы будут удалены с диска, документы — из поиска. Это не отмена.</p>' +
      '<p style="color:var(--text)">' + docs.slice(0, 12).map((d) => esc(d.name)).join('<br />') +
      (docs.length > 12 ? '<br />… и ещё ' + (docs.length - 12) : '') + '</p>', [
        { label: 'Отмена', onClick: () => closeModal() },
        { label: 'Удалить', kind: 'danger', onClick: () => runBulk('delete') },
      ])
    return
  }
  await runBulk(action)
}

// runBulk is the request itself, once the question (if any) is answered.
async function runBulk(action, folder) {
  const docs = pickedDocs()
  if (!docs.length) return
  closeModal()
  progress(true)
  toast('выполняю: ' + docs.length + ' файлов…')
  try {
    const res = await post('api/bulk', {
      action, bucket: state.bucket, docs: docs.map((d) => d.key), folder: folder || '',
    })
    const bad = (res.failed || []).length
    toast(BULK_DONE[action] + ': ' + (res.done || 0) + (bad ? ', ошибок ' + bad : ''), bad ? 'bad' : 'good')
    state.picked.clear()
    state.lastPicked = -1
    // A move changes the addresses the index knows: the new ones come from a scan, not from a guess.
    if (action === 'move') await scanQuiet()
    state.selected = null
    closePreview()
    await reload()
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

// downloadZip asks the module for one archive of the chosen files: twenty separate "save as" dialogs
// is not a download, it is a chore.
async function downloadZip(docs) {
  progress(true)
  toast('собираю архив из ' + docs.length + ' файлов…')
  try {
    const res = await fetch('api/zip', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ bucket: state.bucket, docs: docs.map((d) => d.key) }),
    })
    if (!res.ok || !(res.headers.get('content-type') || '').includes('zip')) {
      const data = await res.json().catch(() => ({}))
      throw new Error(data.error || ('HTTP ' + res.status))
    }
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = 'anytext-' + state.bucket + '.zip'
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 30000)
    toast('архив готов', 'good')
  } catch (e) {
    toast(e.message, 'bad')
  } finally {
    progress(false)
  }
}

$('pick-all').onchange = (e) => {
  const list = visibleFiles().slice(0, state.shown)
  if (e.target.checked) list.forEach((d) => state.picked.add(d.key))
  else list.forEach((d) => state.picked.delete(d.key))
  drawAll()
}

document.querySelectorAll('[data-bulk]').forEach((el) => {
  el.onclick = () => bulk(el.dataset.bulk)
})

// ─── adding files ───────────────────────────────────────────────────────

async function uploadFiles(files) {
  const list = [...(files || [])]
  if (!list.length || !state.bucket) return
  progress(true)
  toast('добавляю файлов: ' + list.length + '…')
  const form = new FormData()
  for (const f of list) form.append('file', f, f.name)
  try {
    const res = await fetch('api/upload?bucket=' + encodeURIComponent(state.bucket), { method: 'POST', body: form })
    const data = await res.json()
    if (data.error) throw new Error(data.error)
    const saved = (data.saved || []).length
    const failed = (data.failed || []).length
    toast('добавлено ' + saved + (failed ? ', ошибок ' + failed : '') + ' — индексирую…', failed ? 'bad' : 'good')
    if (saved) {
      await post('api/scan', { bucket: state.bucket, mode: 'changed' })
      await reload()
    }
  } catch (e) {
    toast('не удалось добавить: ' + e.message, 'bad')
  } finally {
    progress(false)
    $('file-input').value = ''
  }
}

$('btn-upload').onclick = () => $('file-input').click()
$('file-input').onchange = (e) => uploadFiles(e.target.files)

// Drag and drop: a person with the files in hand should not have to find a dialog.
let dragDepth = 0
const listArea = document.querySelector('.content')
listArea.addEventListener('dragenter', (e) => {
  if (!state.bucket || ![...(e.dataTransfer?.types || [])].includes('Files')) return
  dragDepth += 1
  $('drop').hidden = false
})
listArea.addEventListener('dragleave', () => {
  dragDepth = Math.max(0, dragDepth - 1)
  if (!dragDepth) $('drop').hidden = true
})
listArea.addEventListener('dragover', (e) => {
  if (state.bucket) e.preventDefault()
})
listArea.addEventListener('drop', (e) => {
  e.preventDefault()
  dragDepth = 0
  $('drop').hidden = true
  uploadFiles(e.dataTransfer?.files)
})

// The last line of the file: nothing else waits for it.
startOptions()

loadState()
