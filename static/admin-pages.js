// admin-pages.js - page modules for the admin workspace tabs that live outside
// app.js. Loaded after app.js (classic script), so it shares app.js's globals
// (state, escapeHTML, formatSize, formatRelativeTime, ...). Top-level function
// declarations become globals, so app.js can call window.renderAdmin<Page>?.().
//
// Structure: shared helpers first, then one clearly delimited section per
// page. Each section has pure builders (html strings; unit-tested by
// scripts/admin-pages.test.cjs), a render function that app.js calls from
// renderContentMode, and a load function that the page's ADMIN_TAB_LOADERS
// entry calls. To add a page, append a section below.

/* ======================= Shared helpers ======================= */

// adminPagesFetchJSON performs an admin API call and returns
// {ok, status, body, error}. Auth redirects are handled by expectAuthorized;
// other failures carry the server's error text when it sent one.
async function adminPagesFetchJSON(url, options, failureMessage) {
  const response = await fetch(url, { credentials: "same-origin", ...options });
  if (!(await expectAuthorized(response, failureMessage))) {
    return { ok: false, status: response.status, body: null, error: failureMessage };
  }
  let body = null;
  try { body = await response.json(); } catch (error) { body = null; }
  if (!response.ok) {
    return { ok: false, status: response.status, body, error: (body && body.error) || failureMessage };
  }
  return { ok: true, status: response.status, body };
}

function adminPagesJSONPost(url, payload, failureMessage) {
  return adminPagesFetchJSON(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  }, failureMessage);
}

function adminPagesEmpty(text) {
  return `<p class="admin-pages-empty">${escapeHTML(text)}</p>`;
}

/* ======================= Storage page ======================= */

function buildStorageDirs(dirs, disk) {
  const list = (Array.isArray(dirs) ? dirs : []).slice().sort((a, b) => {
    const aOther = a.name === "other" ? 1 : 0;
    const bOther = b.name === "other" ? 1 : 0;
    if (aOther !== bOther) return aOther - bOther;
    return (b.bytes || 0) - (a.bytes || 0);
  });
  const total = list.reduce((sum, dir) => sum + (dir.bytes || 0), 0);
  let html = "";
  if (disk && disk.total_bytes > 0) {
    const pct = Math.min(100, Math.max(0, (disk.used_bytes / disk.total_bytes) * 100));
    html += `<div class="admin-storage-disk${disk.low ? " is-low" : ""}">
      <div class="admin-storage-disk-head"><strong>Disk</strong><span>${escapeHTML(formatSize(disk.free_bytes))} free of ${escapeHTML(formatSize(disk.total_bytes))}${disk.low ? " - low on space" : ""}</span></div>
      <div class="admin-storage-bar"><span style="width:${pct.toFixed(1)}%"></span></div>
    </div>`;
  }
  if (!list.length) return html + adminPagesEmpty("No storage directories reported.");
  html += list.map((dir) => {
    const pct = total > 0 ? ((dir.bytes || 0) / total) * 100 : 0;
    return `<div class="admin-storage-row">
      <div class="admin-storage-row-copy"><strong title="${escapeHTML(dir.path || "")}">${escapeHTML(dir.name)}</strong><span>${escapeHTML(String(dir.files || 0))} files</span></div>
      <div class="admin-storage-bar"><span style="width:${pct.toFixed(1)}%"></span></div>
      <span class="admin-storage-size">${escapeHTML(formatSize(dir.bytes || 0))}</span>
    </div>`;
  }).join("");
  return html;
}

function buildStorageLargest(largest) {
  const list = Array.isArray(largest) ? largest : [];
  if (!list.length) return adminPagesEmpty("Nothing to show yet.");
  return list.map((item) => {
    const name = escapeHTML(item.name || item.id || "Untitled");
    const openAttr = item.kind === "meme" ? "data-storage-open-meme" : item.kind === "audiobook" ? "data-storage-open-audiobook" : "";
    const label = openAttr && item.id
      ? `<button type="button" class="text-button admin-storage-open" ${openAttr}="${escapeHTML(String(item.id))}">${name}</button>`
      : `<strong>${name}</strong>`;
    return `<div class="admin-storage-row admin-storage-largest-row">
      <div class="admin-storage-row-copy">${label}<span>${escapeHTML(item.kind || "")} &middot; ${escapeHTML(item.owner || "—")}</span></div>
      <span class="admin-storage-size">${escapeHTML(formatSize(item.bytes || 0))}</span>
    </div>`;
  }).join("");
}

function buildStorageOrphans(scan, selected) {
  if (!scan) return adminPagesEmpty("Run a scan to look for files the database no longer references.");
  const picked = selected && typeof selected.has === "function" ? selected : new Set();
  const orphans = Array.isArray(scan.orphans) ? scan.orphans : [];
  const missing = Array.isArray(scan.missing) ? scan.missing : [];
  let html = "";
  if (!orphans.length) {
    html += adminPagesEmpty("No orphaned files found.");
  } else {
    const chosen = orphans.filter((o) => picked.has(o.path));
    const chosenBytes = chosen.reduce((sum, o) => sum + (o.bytes || 0), 0);
    html += orphans.map((o) => `<label class="admin-storage-row admin-storage-orphan">
      <input type="checkbox" data-orphan-path="${escapeHTML(o.path)}"${picked.has(o.path) ? " checked" : ""} />
      <div class="admin-storage-row-copy"><strong>${escapeHTML(o.path)}</strong><span>${escapeHTML(o.dir || "")}${o.modified_at ? ` &middot; ${escapeHTML(formatRelativeTime(o.modified_at))}` : ""}</span></div>
      <span class="admin-storage-size">${escapeHTML(formatSize(o.bytes || 0))}</span>
    </label>`).join("");
    html += `<div class="admin-storage-foot">
      <button type="button" class="ghost-button" data-storage-orphans-all>${chosen.length === orphans.length ? "Select none" : "Select all"}</button>
      <span class="admin-storage-selected">${chosen.length} selected &middot; ${escapeHTML(formatSize(chosenBytes))}</span>
      <button type="button" class="danger-button" data-storage-orphans-delete${chosen.length ? "" : " disabled"}>Delete selected</button>
    </div>`;
  }
  if (missing.length) {
    html += `<h4 class="admin-storage-subhead">Missing files</h4>
      <p class="admin-storage-note">The database references these files but they are not on disk.</p>
      ${missing.map((m) => `<div class="admin-storage-row admin-storage-missing">
        <div class="admin-storage-row-copy"><strong>${escapeHTML(m.title || m.id || "")}</strong><span>${escapeHTML(m.kind || "")} &middot; ${escapeHTML(m.path || "")}</span></div>
      </div>`).join("")}`;
  }
  return html;
}

function buildStorageUploads(uploads) {
  const list = Array.isArray(uploads) ? uploads : [];
  if (!list.length) return adminPagesEmpty("No unfinished audiobook uploads.");
  return list.map((u) => {
    const pct = u.total_bytes > 0 ? Math.min(100, (u.received_bytes / u.total_bytes) * 100) : 0;
    return `<div class="admin-storage-row admin-storage-upload">
      <div class="admin-storage-row-copy"><strong>${escapeHTML(u.title || "Untitled")}</strong><span>${escapeHTML(u.display_name || "Unknown user")} &middot; ${escapeHTML(formatSize(u.received_bytes || 0))} of ${escapeHTML(formatSize(u.total_bytes || 0))} (${pct.toFixed(0)}%) &middot; ${escapeHTML(u.updated_at ? formatRelativeTime(u.updated_at) : "")}</span></div>
      <button type="button" class="ghost-button" data-discard-upload="${escapeHTML(String(u.id))}">Discard</button>
    </div>`;
  }).join("");
}

function adminStorageState() {
  const admin = state.admin;
  if (!admin.storage) admin.storage = { data: null, scan: null, selected: new Set(), busy: false, status: "" };
  return admin.storage;
}

function renderAdminStorage() {
  const panel = document.querySelector("#admin-storage-panel");
  if (!panel) return;
  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "storage";
  panel.classList.toggle("hidden", !visible);
  if (!visible) return;
  const s = adminStorageState();
  const data = s.data || {};
  const set = (id, html) => { const el = document.querySelector(id); if (el) el.innerHTML = html; };
  set("#admin-storage-dirs", s.data ? buildStorageDirs(data.dirs, data.disk) : adminPagesEmpty("Loading..."));
  set("#admin-storage-largest", s.data ? buildStorageLargest(data.largest) : "");
  set("#admin-storage-orphans", buildStorageOrphans(s.scan, s.selected));
  set("#admin-storage-uploads", s.data ? buildStorageUploads(data.uploads) : "");
  const status = document.querySelector("#admin-storage-status");
  if (status) status.textContent = s.status || (data.generated_at ? `Updated ${formatRelativeTime(data.generated_at)}.` : "");
  if (s.busy) panel.querySelectorAll("button").forEach((button) => { button.disabled = true; });
}

async function loadAdminStorage({ refresh = false } = {}) {
  const result = await adminPagesFetchJSON(`/api/admin/storage${refresh ? "?refresh=1" : ""}`, undefined, "Failed to load storage.");
  const s = adminStorageState();
  if (!result.ok) {
    s.status = result.error;
    s.loadFailed = true;
    renderAdminStorage();
    return false;
  }
  s.data = result.body;
  // Clear only a stale load error; a post-delete message survives its reload.
  if (s.loadFailed) { s.status = ""; s.loadFailed = false; }
  renderAdminStorage();
  return true;
}

// withStorageBusy runs fn with the panel's buttons disabled and reports a
// thrown error in the status line.
async function withStorageBusy(fn) {
  const s = adminStorageState();
  if (s.busy) return;
  s.busy = true;
  renderAdminStorage();
  try {
    await fn(s);
  } catch (error) {
    console.error(error);
    s.status = "Something went wrong. Check your connection and try again.";
  } finally {
    s.busy = false;
    renderAdminStorage();
  }
}

async function scanStorageOrphans(s) {
  s.status = "Scanning...";
  renderAdminStorage();
  const result = await adminPagesJSONPost("/api/admin/storage/orphans/scan", {}, "Failed to scan for orphaned files.");
  if (!result.ok) {
    s.status = result.error;
    return;
  }
  s.scan = result.body;
  s.selected = new Set();
  const n = (result.body.orphans || []).length;
  s.status = `Scan found ${n} orphaned file${n === 1 ? "" : "s"}.`;
}

function orphanDeleteMessage(count, bytes) {
  return `Delete ${count} file${count === 1 ? "" : "s"} (${formatSize(bytes)})? This cannot be undone.`;
}

async function deleteStorageOrphans(s) {
  const orphans = (s.scan && s.scan.orphans) || [];
  const chosen = orphans.filter((o) => s.selected.has(o.path));
  if (!chosen.length) return;
  const bytes = chosen.reduce((sum, o) => sum + (o.bytes || 0), 0);
  const message = orphanDeleteMessage(chosen.length, bytes);
  const ok = typeof confirmViaModal === "function"
    ? await confirmViaModal({ title: "Delete orphaned files", message, confirmLabel: "Delete" })
    : window.confirm(message);
  if (!ok) return;
  const result = await adminPagesJSONPost("/api/admin/storage/orphans/delete", { paths: chosen.map((o) => o.path) }, "Failed to delete files.");
  if (!result.ok) {
    // Includes the 409 "backup or restore in progress" message, shown as sent.
    s.status = result.error;
    return;
  }
  const results = result.body.results || [];
  const failed = results.filter((r) => !r.deleted);
  const done = results.length - failed.length;
  let status = `Deleted ${done} file${done === 1 ? "" : "s"}, freed ${formatSize(result.body.bytes_freed || 0)}.`;
  if (failed.length) {
    status += ` ${failed.length} could not be deleted: ${failed.map((r) => `${r.path} (${r.error || "refused"})`).join("; ")}`;
  }
  await scanStorageOrphans(s);
  s.status = status;
  await loadAdminStorage({ refresh: true });
}

async function discardStorageUpload(s, id) {
  const upload = ((s.data && s.data.uploads) || []).find((u) => String(u.id) === String(id));
  const message = `Discard the unfinished upload of “${(upload && upload.title) || "this book"}”?`;
  const ok = typeof confirmViaModal === "function"
    ? await confirmViaModal({ title: "Discard upload", message, confirmLabel: "Discard" })
    : window.confirm(message);
  if (!ok) return;
  const result = await adminPagesFetchJSON(`/api/admin/audiobooks/uploads/${encodeURIComponent(id)}`, { method: "DELETE" }, "Failed to discard the upload.");
  if (!result.ok) {
    s.status = result.error;
    return;
  }
  const body = result.body;
  s.status = body && body.state === "unavailable" ? "Audiobook uploads are unavailable on this server." : "Upload discarded.";
  await loadAdminStorage({ refresh: true });
}

function initAdminStoragePanel() {
  const panel = document.querySelector("#admin-storage-panel");
  if (!panel || panel.dataset.wired) return;
  panel.dataset.wired = "1";
  panel.addEventListener("click", (event) => {
    const s = adminStorageState();
    const target = event.target.closest("button");
    if (!target || target.disabled) return;
    if (target.id === "admin-storage-refresh") {
      withStorageBusy(async () => { s.status = ""; await loadAdminStorage({ refresh: true }); });
    } else if (target.id === "admin-storage-scan") {
      withStorageBusy(scanStorageOrphans);
    } else if (target.hasAttribute("data-storage-orphans-delete")) {
      withStorageBusy(deleteStorageOrphans);
    } else if (target.hasAttribute("data-storage-orphans-all")) {
      const orphans = (s.scan && s.scan.orphans) || [];
      s.selected = s.selected.size === orphans.length ? new Set() : new Set(orphans.map((o) => o.path));
      renderAdminStorage();
    } else if (target.hasAttribute("data-discard-upload")) {
      withStorageBusy((st) => discardStorageUpload(st, target.getAttribute("data-discard-upload")));
    } else if (target.hasAttribute("data-storage-open-meme")) {
      openAdminMemeByID(target.getAttribute("data-storage-open-meme")).catch((error) => console.error(error));
    } else if (target.hasAttribute("data-storage-open-audiobook")) {
      openAdminLibraryAudiobook(target.getAttribute("data-storage-open-audiobook"), s, renderAdminStorage).catch((error) => {
        console.error(error);
        s.status = "Could not open the audiobook.";
        renderAdminStorage();
      });
    }
  });
  panel.addEventListener("change", (event) => {
    const box = event.target.closest("input[data-orphan-path]");
    if (!box) return;
    const s = adminStorageState();
    const path = box.getAttribute("data-orphan-path");
    if (box.checked) s.selected.add(path); else s.selected.delete(path);
    renderAdminStorage();
  });
}
if (typeof document !== "undefined") initAdminStoragePanel();

/* ======================= Audiobook Library page ======================= */

const ADMIN_LIBRARY_COLUMNS = [
  { key: "title", label: "Title" },
  { key: "author", label: "Author" },
  { key: "format", label: "Format" },
  { key: "size", label: "Size", num: true },
  { key: "duration", label: "Duration", num: true },
  { key: "uploader", label: "Uploader" },
  { key: "created_at", label: "Added" },
  { key: "cover", label: "Cover", flag: true },
  { key: "chapters", label: "Chapters", flag: true },
  { key: "series", label: "Series", flag: true },
  { key: "listeners", label: "Listeners (30d)", num: true },
  { key: "hours", label: "Hours listened", num: true },
];

function adminLibraryDuration(seconds) {
  const total = Math.round(Number(seconds) || 0);
  if (total <= 0) return "—";
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  return h > 0 ? `${h}h ${m}m` : `${m}m`;
}

function adminLibraryFlag(ok, name) {
  return ok
    ? `<span class="admin-library-ok" aria-label="Has ${name}">✓</span>`
    : `<span class="admin-library-gap" aria-label="No ${name}">—</span>`;
}

// buildAudiobookLibraryTable renders the sortable table. sort is {sort, dir}.
// Flag columns (cover/chapters/series) are not sortable by the API.
function buildAudiobookLibraryTable(rows, sort) {
  const list = Array.isArray(rows) ? rows : [];
  if (!list.length) return adminPagesEmpty("No audiobooks match this filter.");
  const active = sort && sort.sort;
  const head = ADMIN_LIBRARY_COLUMNS.map((col) => {
    const cls = `${col.num ? "is-num" : ""}${col.flag ? "is-flag" : ""}`;
    const classAttr = cls ? ` class="${cls}"` : "";
    if (col.flag) return `<th scope="col"${classAttr}>${escapeHTML(col.label)}</th>`;
    const aria = active === col.key ? (sort.dir === "asc" ? "ascending" : "descending") : "none";
    const arrow = aria === "ascending" ? " ▲" : aria === "descending" ? " ▼" : "";
    return `<th scope="col"${classAttr} aria-sort="${aria}"><button type="button" class="admin-library-sort" data-library-sort="${col.key}">${escapeHTML(col.label)}<span aria-hidden="true">${arrow}</span></button></th>`;
  }).join("");
  const body = list.map((item) => {
    const id = escapeHTML(String(item.id));
    const added = item.created_at ? formatRelativeTime(item.created_at) : "";
    return `<tr>
      <td data-col="title"><button type="button" class="text-button admin-library-open" data-admin-audiobook-id="${id}">${escapeHTML(item.title || "Untitled")}</button></td>
      <td data-col="author">${escapeHTML(item.author || "—")}</td>
      <td data-col="format">${escapeHTML(item.format || "—")}</td>
      <td data-col="size" class="is-num">${escapeHTML(formatSize(item.size_bytes || 0))}</td>
      <td data-col="duration" class="is-num">${escapeHTML(adminLibraryDuration(item.duration_seconds))}</td>
      <td data-col="uploader">${escapeHTML(item.uploader_name || item.uploader_id || "—")}</td>
      <td data-col="created_at">${escapeHTML(added)}</td>
      <td data-col="cover" class="is-flag">${adminLibraryFlag(item.has_cover, "cover")}</td>
      <td data-col="chapters" class="is-flag">${adminLibraryFlag(item.has_chapters, "chapters")}</td>
      <td data-col="series" class="is-flag">${adminLibraryFlag(item.has_series, "series")}</td>
      <td data-col="listeners" class="is-num">${escapeHTML(String(item.active_listeners || 0))}</td>
      <td data-col="hours" class="is-num">${escapeHTML(((item.seconds_listened || 0) / 3600).toFixed(1))}</td>
    </tr>`;
  }).join("");
  return `<table class="admin-library-table"><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
}

function audiobookLibraryPageInfo(data) {
  const size = Math.max(1, (data && data.page_size) || 50);
  const total = Math.max(0, (data && data.total) || 0);
  const page = Math.max(1, (data && data.page) || 1);
  const pages = Math.max(1, Math.ceil(total / size));
  return { page, pages, label: `Page ${page} of ${pages}`, hasPrev: page > 1, hasNext: page < pages };
}

function audiobookLibraryQuery(s) {
  const params = new URLSearchParams();
  params.set("sort", s.sort);
  params.set("dir", s.dir);
  if (s.filter) params.set("filter", s.filter);
  params.set("page", String(s.page || 1));
  return params.toString();
}

// Numeric/date columns read best largest-first on first click; text ascending.
const ADMIN_LIBRARY_DESC_FIRST = ["size", "duration", "created_at", "listeners", "hours"];

function adminLibraryState() {
  const admin = state.admin;
  const s = admin.audiobookLibrary || (admin.audiobookLibrary = {});
  if (s.filter == null) s.filter = "";
  if (!s.sort) s.sort = "created_at";
  if (!s.dir) s.dir = "desc";
  if (!s.page) s.page = 1;
  if (s.data === undefined) s.data = null;
  if (s.status === undefined) s.status = "";
  return s;
}

let adminLibraryRequestToken = 0;

// adminLibraryPresetState is the library state when another page (the
// dashboard health counts) opens the tab with a filter: keep sort/dir, but
// restart at page 1 with no stale rows.
function adminLibraryPresetState(existing, filter) {
  return { ...(existing || {}), filter, page: 1, data: null, busy: false, status: "" };
}

// refreshAdminAudiobookLibrary reloads the table after an edit, only when the
// library tab is the one on screen.
function refreshAdminAudiobookLibrary() {
  if (isAdminView() && canManageUsers() && activeAdminTab() === "audiobook-library") reloadAdminAudiobookLibrary();
}

function renderAdminAudiobookLibrary() {
  const panel = document.querySelector("#admin-audiobook-library-panel");
  if (!panel) return;
  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "audiobook-library";
  panel.classList.toggle("hidden", !visible);
  if (!visible) return;
  const s = adminLibraryState();
  const unavailable = !!(s.data && s.data.state === "unavailable");
  const set = (id, html) => { const el = document.querySelector(id); if (el) el.innerHTML = html; };
  const filter = document.querySelector("#admin-library-filter");
  if (filter) { filter.value = s.filter; filter.disabled = unavailable; }
  const pager = document.querySelector("#admin-library-pager");
  if (unavailable) {
    set("#admin-library-table", adminPagesEmpty("Requires PostgreSQL"));
    if (pager) pager.classList.add("hidden");
  } else if (!s.data) {
    set("#admin-library-table", adminPagesEmpty(s.status || "Loading..."));
    if (pager) pager.classList.add("hidden");
  } else {
    set("#admin-library-table", buildAudiobookLibraryTable(s.data.items, s));
    const info = audiobookLibraryPageInfo(s.data);
    if (pager) pager.classList.toggle("hidden", info.pages <= 1);
    const label = document.querySelector("#admin-library-page-label");
    if (label) label.textContent = info.label;
    const prev = document.querySelector("#admin-library-prev");
    const next = document.querySelector("#admin-library-next");
    if (prev) prev.disabled = !info.hasPrev || s.busy;
    if (next) next.disabled = !info.hasNext || s.busy;
  }
  const status = document.querySelector("#admin-library-status");
  if (status) {
    status.textContent = s.status || (s.data && s.data.state !== "unavailable" && s.data.total != null
      ? `${s.data.total} audiobook${s.data.total === 1 ? "" : "s"}` : "");
  }
}

async function loadAdminAudiobookLibrary() {
  const s = adminLibraryState();
  const token = ++adminLibraryRequestToken;
  s.busy = true;
  s.status = "";
  renderAdminAudiobookLibrary();
  let result;
  try {
    result = await adminPagesFetchJSON(`/api/admin/audiobooks?${audiobookLibraryQuery(s)}`, undefined, "Failed to load the audiobook library.");
  } catch (error) {
    console.error(error);
    result = { ok: false, error: "Failed to load the audiobook library." };
  }
  if (token !== adminLibraryRequestToken) return true; // a newer request owns the panel
  s.busy = false;
  if (!result.ok) {
    s.status = result.error;
    renderAdminAudiobookLibrary();
    return false;
  }
  s.data = result.body;
  renderAdminAudiobookLibrary();
  return true;
}

// openAdminLibraryAudiobook fetches the full record (the list rows are
// summaries) and hands it to the shared audiobook edit modal in app.js.
// The status line and re-render default to the Library page's; the Storage
// page passes its own.
async function openAdminLibraryAudiobook(id, s = adminLibraryState(), render = renderAdminAudiobookLibrary) {
  s.status = "Opening...";
  render();
  const result = await adminPagesFetchJSON(`/api/audiobooks/${encodeURIComponent(id)}`, undefined, "Could not open the audiobook.");
  if (!result.ok || !result.body) {
    s.status = result.error || "Could not open the audiobook.";
    render();
    return;
  }
  s.status = "";
  render();
  openAudiobookEditModal(result.body);
}

// focusSelector, when given, is re-focused after the table re-renders so
// keyboard users stay on the control they used.
function reloadAdminAudiobookLibrary(focusSelector) {
  loadAdminAudiobookLibrary().catch((error) => console.error(error)).then(() => {
    if (!focusSelector) return;
    const el = document.querySelector("#admin-audiobook-library-panel " + focusSelector);
    if (el && typeof el.focus === "function") el.focus();
  });
}

function initAdminAudiobookLibraryPanel() {
  const panel = document.querySelector("#admin-audiobook-library-panel");
  if (!panel || panel.dataset.wired) return;
  panel.dataset.wired = "1";
  panel.addEventListener("click", (event) => {
    const target = event.target.closest("button");
    if (!target || target.disabled) return;
    const s = adminLibraryState();
    if (target.hasAttribute("data-library-sort")) {
      const col = target.getAttribute("data-library-sort");
      if (s.sort === col) s.dir = s.dir === "asc" ? "desc" : "asc";
      else { s.sort = col; s.dir = ADMIN_LIBRARY_DESC_FIRST.includes(col) ? "desc" : "asc"; }
      s.page = 1;
      reloadAdminAudiobookLibrary(`[data-library-sort="${col}"]`);
    } else if (target.hasAttribute("data-admin-audiobook-id")) {
      openAdminLibraryAudiobook(target.getAttribute("data-admin-audiobook-id")).catch((error) => {
        console.error(error);
        s.status = "Could not open the audiobook.";
        renderAdminAudiobookLibrary();
      });
    } else if (target.id === "admin-library-prev" || target.id === "admin-library-next") {
      s.page = Math.max(1, (s.page || 1) + (target.id === "admin-library-next" ? 1 : -1));
      reloadAdminAudiobookLibrary();
    }
  });
  panel.addEventListener("change", (event) => {
    if (event.target.id !== "admin-library-filter") return;
    const s = adminLibraryState();
    s.filter = event.target.value;
    s.page = 1;
    reloadAdminAudiobookLibrary();
  });
}
if (typeof document !== "undefined") initAdminAudiobookLibraryPanel();

/* ======================= User Activity page ======================= */

const ADMIN_USER_ACTIVITY_COLUMNS = [
  { key: "name", label: "User", text: true, value: (u) => String(u.display_name || u.username || u.user_id || "").toLowerCase() },
  { key: "memes", label: "Memes uploaded", num: true, value: (u) => Number(u.memes_uploaded) || 0 },
  { key: "books", label: "Audiobooks uploaded", num: true, value: (u) => Number(u.audiobooks_uploaded) || 0 },
  { key: "favorites", label: "Favorites", num: true, value: (u) => Number(u.favorites) || 0 },
  { key: "hours30", label: "Hours (30d)", num: true, value: (u) => Number(u.seconds_30d) || 0 },
  { key: "hours", label: "Hours (all time)", num: true, value: (u) => Number(u.seconds_all_time) || 0 },
  { key: "finished", label: "Books finished", num: true, value: (u) => Number(u.books_finished) || 0 },
  { key: "last_active", label: "Last active", value: (u) => Number(u.last_active_at) || 0 },
];

function adminUserActivityHours(seconds) {
  return ((Number(seconds) || 0) / 3600).toFixed(1);
}

function adminUserActivityLastActive(unixSeconds) {
  const sec = Number(unixSeconds) || 0;
  return sec > 0 ? formatRelativeTime(new Date(sec * 1000)) : "Never";
}

// buildUserActivityTable renders the per-user table, sorted client-side.
// sort is {sort, dir}; unknown keys fall back to the name column.
function buildUserActivityTable(users, sort) {
  const list = Array.isArray(users) ? users.slice() : [];
  if (!list.length) return adminPagesEmpty("No users yet.");
  const key = (sort && sort.sort) || "name";
  const dir = sort && sort.dir === "asc" ? 1 : -1;
  const col = ADMIN_USER_ACTIVITY_COLUMNS.find((c) => c.key === key) || ADMIN_USER_ACTIVITY_COLUMNS[0];
  list.sort((a, b) => {
    const av = col.value(a);
    const bv = col.value(b);
    if (av < bv) return -dir;
    if (av > bv) return dir;
    return 0;
  });
  const head = ADMIN_USER_ACTIVITY_COLUMNS.map((c) => {
    const active = c.key === col.key;
    const aria = active ? (dir === 1 ? "ascending" : "descending") : "none";
    const arrow = aria === "ascending" ? " ▲" : aria === "descending" ? " ▼" : "";
    return `<th scope="col"${c.num ? ' class="is-num"' : ""} aria-sort="${aria}"><button type="button" class="admin-library-sort" data-user-activity-sort="${c.key}">${escapeHTML(c.label)}<span aria-hidden="true">${arrow}</span></button></th>`;
  }).join("");
  const body = list.map((u) => {
    const id = escapeHTML(String(u.user_id));
    const name = u.display_name || u.username || u.user_id || "Unknown";
    const handle = u.username ? ` <span class="admin-user-activity-handle">@${escapeHTML(u.username)}</span>` : "";
    return `<tr>
      <td data-col="name"><button type="button" class="text-button admin-library-open" data-user-activity-id="${id}" data-user-activity-name="${escapeHTML(String(name))}">${escapeHTML(String(name))}</button>${handle}</td>
      <td data-col="memes" class="is-num">${escapeHTML(String(u.memes_uploaded || 0))}</td>
      <td data-col="books" class="is-num">${escapeHTML(String(u.audiobooks_uploaded || 0))}</td>
      <td data-col="favorites" class="is-num">${escapeHTML(String(u.favorites || 0))}</td>
      <td data-col="hours30" class="is-num">${escapeHTML(adminUserActivityHours(u.seconds_30d))}</td>
      <td data-col="hours" class="is-num">${escapeHTML(adminUserActivityHours(u.seconds_all_time))}</td>
      <td data-col="finished" class="is-num">${escapeHTML(String(u.books_finished || 0))}</td>
      <td data-col="last_active">${escapeHTML(adminUserActivityLastActive(u.last_active_at))}</td>
    </tr>`;
  }).join("");
  return `<table class="admin-library-table admin-user-activity-table"><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
}

// setAdminAuditActor sets (or, with null, clears) the user the Audit Logs tab
// is filtered to, and returns its paging to the first page.
function setAdminAuditActor(actor) {
  state.admin.auditActor = actor && actor.user_id
    ? { user_id: String(actor.user_id), display_name: String(actor.display_name || actor.user_id) }
    : null;
  if (state.admin.audit) state.admin.audit.offset = 0;
}

function adminUserActivityState() {
  const s = state.admin.userActivity || (state.admin.userActivity = {});
  if (!s.sort) s.sort = "last_active";
  if (!s.dir) s.dir = "desc";
  if (s.data === undefined) s.data = null;
  if (s.status === undefined) s.status = "";
  return s;
}

let adminUserActivityRequestToken = 0;

function renderAdminUserActivity() {
  const panel = document.querySelector("#admin-user-activity-panel");
  if (!panel) return;
  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "user-activity";
  panel.classList.toggle("hidden", !visible);
  if (!visible) return;
  const s = adminUserActivityState();
  const table = document.querySelector("#admin-user-activity-table");
  if (table) {
    table.innerHTML = s.data
      ? buildUserActivityTable(s.data.users, s)
      : adminPagesEmpty(s.status || "Loading...");
  }
  const status = document.querySelector("#admin-user-activity-status");
  if (status) status.textContent = s.status || (s.data ? `${(s.data.users || []).length} users` : "");
}

async function loadAdminUserActivity() {
  const s = adminUserActivityState();
  const token = ++adminUserActivityRequestToken;
  s.status = "";
  let result;
  try {
    result = await adminPagesFetchJSON("/api/admin/user-activity", undefined, "Failed to load user activity.");
  } catch (error) {
    console.error(error);
    result = { ok: false, error: "Failed to load user activity." };
  }
  if (token !== adminUserActivityRequestToken) return true; // a newer request owns the panel
  if (!result.ok) {
    s.status = result.error;
    renderAdminUserActivity();
    return false;
  }
  s.data = result.body;
  renderAdminUserActivity();
  return true;
}

function initAdminUserActivityPanel() {
  const panel = document.querySelector("#admin-user-activity-panel");
  if (!panel || panel.dataset.wired) return;
  panel.dataset.wired = "1";
  panel.addEventListener("click", (event) => {
    const target = event.target.closest("button");
    if (!target || target.disabled) return;
    const s = adminUserActivityState();
    if (target.hasAttribute("data-user-activity-sort")) {
      const col = target.getAttribute("data-user-activity-sort");
      if (s.sort === col) s.dir = s.dir === "asc" ? "desc" : "asc";
      else { s.sort = col; s.dir = col === "name" ? "asc" : "desc"; }
      renderAdminUserActivity();
      const again = panel.querySelector(`[data-user-activity-sort="${col}"]`);
      if (again) again.focus();
    } else if (target.hasAttribute("data-user-activity-id")) {
      setAdminAuditActor({
        user_id: target.getAttribute("data-user-activity-id"),
        display_name: target.getAttribute("data-user-activity-name"),
      });
      navigateAdminTab("audit-logs");
    }
  });
}
if (typeof document !== "undefined") initAdminUserActivityPanel();

/* ======================= Series & Tags page ======================= */

function seriesDeleteConfirmText(series) {
  const name = (series && series.name) || "";
  const n = Number(series && series.book_count) || 0;
  return n > 0
    ? `Delete “${name}”? Its ${n} ${n === 1 ? "book" : "books"} will no longer be in a series.`
    : `Delete “${name}”?`;
}

function seriesMergeConfirmText(from, into) {
  const n = Number(from && from.book_count) || 0;
  const fromName = (from && from.name) || "";
  return `Move ${n} ${n === 1 ? "book" : "books"} from “${fromName}” into “${(into && into.name) || ""}” and delete “${fromName}”?`;
}

// parseSeriesSequence returns the number in a sequence input, or NaN when it
// is blank, not numeric, or negative. Decimals are allowed (7.5 novellas).
function parseSeriesSequence(text) {
  const trimmed = String(text == null ? "" : text).trim();
  if (!/^\d+(\.\d+)?$/.test(trimmed)) return NaN;
  return Number(trimmed);
}

function buildSeriesBooks(books, errors) {
  const list = (Array.isArray(books) ? books : []).slice().sort((a, b) => {
    const sa = a.series_sequence == null ? Infinity : Number(a.series_sequence);
    const sb = b.series_sequence == null ? Infinity : Number(b.series_sequence);
    if (sa !== sb) return sa < sb ? -1 : 1;
    return String(a.title || "").localeCompare(String(b.title || ""));
  });
  if (!list.length) return adminPagesEmpty("No books in this series.");
  const errs = errors || {};
  return `<ul class="admin-series-books">${list.map((book) => {
    const id = escapeHTML(String(book.id));
    const seq = book.series_sequence == null ? "" : String(book.series_sequence);
    const err = errs[book.id];
    return `<li class="admin-series-book"><span class="admin-series-book-title">${escapeHTML(book.title || "Untitled")}</span>`
      + `<label class="admin-series-seq"><span>Book #</span>`
      + `<input type="number" step="0.5" min="0" inputmode="decimal" data-series-sequence="${id}" data-series-sequence-orig="${escapeHTML(seq)}" value="${escapeHTML(seq)}"${err ? ' aria-invalid="true"' : ""}></label>`
      + (err ? `<span class="admin-series-error" role="alert">${escapeHTML(err)}</span>` : "")
      + `</li>`;
  }).join("")}</ul>`;
}

function buildSeriesList(series, expandedID, books, errors) {
  const list = Array.isArray(series) ? series : [];
  if (!list.length) return adminPagesEmpty("No audiobook series yet.");
  return list.map((item) => {
    const id = escapeHTML(String(item.id));
    const name = escapeHTML(item.name || "");
    const n = Number(item.book_count) || 0;
    const expanded = String(item.id) === String(expandedID || "");
    const options = list.filter((other) => other.id !== item.id)
      .map((other) => `<option value="${escapeHTML(String(other.id))}">${escapeHTML(other.name || "")}</option>`).join("");
    return `<article class="admin-series-row" data-series-id="${id}">`
      + `<div class="admin-series-main">`
      + `<input type="text" class="admin-series-name" maxlength="200" aria-label="Series name" data-series-name="${id}" value="${name}">`
      + `<button type="button" class="secondary-button" data-series-rename="${id}">Rename</button>`
      + `<span class="admin-series-count">${n} book${n === 1 ? "" : "s"}</span>`
      + `<button type="button" class="secondary-button" data-series-toggle="${id}" aria-expanded="${expanded}">${expanded ? "Hide books" : "Books"}</button>`
      + `</div>`
      + `<div class="admin-series-actions">`
      + `<select aria-label="Merge ${name} into another series" data-series-merge-target="${id}"><option value="">Merge into…</option>${options}</select>`
      + `<button type="button" class="secondary-button" data-series-merge="${id}">Merge</button>`
      + `<button type="button" class="danger-button" data-series-delete="${id}">Delete</button>`
      + `</div>`
      + (expanded ? `<div class="admin-series-expanded">${books ? buildSeriesBooks(books, errors) : adminPagesEmpty("Loading...")}</div>` : "")
      + `</article>`;
  }).join("");
}

function buildAudiobookTagUsage(tags) {
  const list = Array.isArray(tags) ? tags : [];
  if (!list.length) return adminPagesEmpty("No audiobook tags in use.");
  const rows = list.map((t) => {
    const tag = escapeHTML(t.tag || "");
    const users = Array.isArray(t.restricted_users) ? t.restricted_users : [];
    const chips = users.length
      ? users.map((u) => `<span class="admin-series-chip">${escapeHTML(u.display_name || u.user_id || "")}</span>`).join("")
      : `<span class="admin-series-muted">None</span>`;
    return `<tr><td><button type="button" class="text-button" data-series-tag-hygiene="${tag}">${tag}</button></td>`
      + `<td class="is-num">${Number(t.audiobooks) || 0}</td><td class="is-num">${Number(t.memes) || 0}</td>`
      + `<td class="admin-series-chips">${chips}</td></tr>`;
  }).join("");
  return `<table class="admin-library-table admin-series-tag-table"><thead><tr><th>Tag</th><th class="is-num">Audiobooks</th><th class="is-num">Memes</th><th>Restricted accounts</th></tr></thead><tbody>${rows}</tbody></table>`;
}

function adminSeriesState() {
  const s = state.admin.audiobookSeries || (state.admin.audiobookSeries = {});
  if (s.series === undefined) s.series = null;
  if (s.tags === undefined) s.tags = null;
  if (s.unavailable === undefined) s.unavailable = false;
  if (s.expanded === undefined) s.expanded = "";
  if (s.books === undefined) s.books = null;
  if (!s.errors) s.errors = {};
  if (s.status === undefined) s.status = "";
  if (s.busy === undefined) s.busy = false;
  return s;
}

function renderAdminAudiobookSeries() {
  const panel = document.querySelector("#admin-audiobook-series-panel");
  if (!panel) return;
  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "audiobook-series";
  panel.classList.toggle("hidden", !visible);
  if (!visible) return;
  const s = adminSeriesState();
  const set = (id, html) => { const el = document.querySelector(id); if (el) el.innerHTML = html; };
  if (s.unavailable) {
    set("#admin-series-list", adminPagesEmpty("Requires PostgreSQL"));
    set("#admin-audiobook-tags", adminPagesEmpty("Requires PostgreSQL"));
  } else {
    set("#admin-series-list", s.series ? buildSeriesList(s.series, s.expanded, s.books, s.errors) : adminPagesEmpty(s.status || "Loading..."));
    set("#admin-audiobook-tags", s.tags ? buildAudiobookTagUsage(s.tags) : adminPagesEmpty("Loading..."));
  }
  const status = document.querySelector("#admin-series-status");
  if (status) status.textContent = s.status || "";
  if (s.busy) panel.querySelectorAll("button").forEach((button) => { button.disabled = true; });
}

async function loadAdminSeriesBooks(s) {
  if (!s.expanded) { s.books = null; return; }
  const id = s.expanded;
  const result = await adminPagesFetchJSON(`/api/audiobooks?series_id=${encodeURIComponent(id)}`, undefined, "Could not load this series' books.");
  if (s.expanded !== id) return;
  if (!result.ok) { s.status = result.error; s.books = []; return; }
  s.books = (result.body && result.body.audiobooks) || [];
}

async function loadAdminAudiobookSeries({ keepStatus = false } = {}) {
  const s = adminSeriesState();
  const [list, tags] = await Promise.all([
    adminPagesFetchJSON("/api/audiobook-series", undefined, "Failed to load audiobook series."),
    adminPagesFetchJSON("/api/admin/audiobook-tags", undefined, "Failed to load audiobook tags."),
  ]);
  const unavailable = list.status === 501 || (tags.ok && tags.body && tags.body.state === "unavailable")
    || (list.ok && list.body && list.body.state === "unavailable");
  s.unavailable = !!unavailable;
  if (!keepStatus && (unavailable || (list.ok && tags.ok))) s.status = "";
  if (!unavailable) {
    if (list.ok) s.series = (list.body && list.body.series) || [];
    else s.status = list.error;
    if (tags.ok) s.tags = (tags.body && tags.body.tags) || [];
    else if (!s.status) s.status = tags.error;
    if (s.expanded && !(s.series || []).some((x) => x.id === s.expanded)) { s.expanded = ""; s.books = null; }
    if (s.expanded) await loadAdminSeriesBooks(s);
  }
  renderAdminAudiobookSeries();
  return list.ok || unavailable;
}

// withSeriesBusy runs fn with the panel's buttons disabled; a thrown error is
// reported in the status line. When fn reports a change, the lists (and the
// library tab, if loaded) are refreshed.
async function withSeriesBusy(fn) {
  const s = adminSeriesState();
  if (s.busy) return;
  s.busy = true;
  renderAdminAudiobookSeries();
  try {
    const changed = await fn(s);
    if (changed) {
      await loadAdminAudiobookSeries({ keepStatus: true });
      window.refreshAdminAudiobookLibrary?.();
    }
  } catch (error) {
    console.error(error);
    s.status = "Something went wrong. Check your connection and try again.";
  } finally {
    s.busy = false;
    renderAdminAudiobookSeries();
  }
}

function seriesConfirm(title, message, confirmLabel) {
  return typeof confirmViaModal === "function"
    ? confirmViaModal({ title, message, confirmLabel })
    : Promise.resolve(window.confirm(message));
}

function findAdminSeries(s, id) {
  return ((s && s.series) || []).find((x) => String(x.id) === String(id));
}

async function renameAdminSeries(s, id, name) {
  const item = findAdminSeries(s, id);
  const next = String(name || "").trim();
  if (!item || !next) { s.status = "Enter a series name."; return false; }
  if (next === item.name) { s.status = ""; return false; }
  const result = await adminPagesFetchJSON(`/api/audiobook-series/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: next }),
  }, "Failed to rename the series.");
  if (!result.ok) { s.status = result.error; return false; }
  s.status = "Series renamed.";
  return true;
}

async function mergeAdminSeries(s, fromID, intoID) {
  const from = findAdminSeries(s, fromID);
  const into = findAdminSeries(s, intoID);
  if (!from || !into) { s.status = "Choose a series to merge into."; return false; }
  if (!(await seriesConfirm("Merge series", seriesMergeConfirmText(from, into), "Merge"))) return false;
  const result = await adminPagesJSONPost(`/api/admin/audiobook-series/${encodeURIComponent(fromID)}/merge`, { into: intoID }, "Failed to merge the series.");
  if (!result.ok) { s.status = result.error; return false; }
  if (result.body && result.body.state === "unavailable") { s.status = "Requires PostgreSQL"; return false; }
  const moved = Number(result.body && result.body.moved) || 0;
  if (s.expanded === fromID) { s.expanded = intoID; s.books = null; }
  s.status = `Moved ${moved} book${moved === 1 ? "" : "s"} into “${into.name}”.`;
  return true;
}

async function deleteAdminSeries(s, id) {
  const item = findAdminSeries(s, id);
  if (!item) return false;
  if (!(await seriesConfirm("Delete series", seriesDeleteConfirmText(item), "Delete"))) return false;
  const result = await adminPagesFetchJSON(`/api/audiobook-series/${encodeURIComponent(id)}`, { method: "DELETE" }, "Failed to delete the series.");
  if (!result.ok) { s.status = result.error; return false; }
  if (s.expanded === id) { s.expanded = ""; s.books = null; }
  s.status = "Series deleted.";
  return true;
}

async function saveAdminSeriesSequence(s, bookID, rawValue, origValue) {
  const value = parseSeriesSequence(rawValue);
  if (Number.isNaN(value)) { s.errors[bookID] = "Enter a number (0 or more)."; return false; }
  if (String(origValue) !== "" && Number(origValue) === value) { delete s.errors[bookID]; return false; }
  const result = await adminPagesFetchJSON(`/api/audiobooks/${encodeURIComponent(bookID)}/series`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ seriesId: s.expanded, sequence: value }),
  }, "Failed to save the sequence.");
  if (!result.ok) { s.errors[bookID] = result.error; return false; }
  delete s.errors[bookID];
  s.status = "Sequence saved.";
  return true;
}

// openAdminTagInHygiene jumps to Tag Hygiene with the tag in the merge-source
// field. The field is static markup, so it can be filled right away.
function openAdminTagInHygiene(tag) {
  navigateAdminTab("tag-hygiene");
  const input = document.querySelector("#admin-tag-merge-source");
  if (input) input.value = tag;
}

function initAdminAudiobookSeriesPanel() {
  const panel = document.querySelector("#admin-audiobook-series-panel");
  if (!panel || panel.dataset.wired) return;
  panel.dataset.wired = "1";
  panel.addEventListener("click", (event) => {
    const target = event.target.closest("button");
    if (!target || target.disabled) return;
    const s = adminSeriesState();
    const attr = (name) => target.getAttribute(name);
    if (target.hasAttribute("data-series-rename")) {
      const id = attr("data-series-rename");
      const input = panel.querySelector(`[data-series-name="${CSS.escape(id)}"]`);
      withSeriesBusy((st) => renameAdminSeries(st, id, input ? input.value : ""));
    } else if (target.hasAttribute("data-series-toggle")) {
      const id = attr("data-series-toggle");
      s.expanded = s.expanded === id ? "" : id;
      s.books = null;
      s.errors = {};
      renderAdminAudiobookSeries();
      withSeriesBusy(async (st) => { await loadAdminSeriesBooks(st); return false; });
    } else if (target.hasAttribute("data-series-merge")) {
      const id = attr("data-series-merge");
      const select = panel.querySelector(`[data-series-merge-target="${CSS.escape(id)}"]`);
      withSeriesBusy((st) => mergeAdminSeries(st, id, select ? select.value : ""));
    } else if (target.hasAttribute("data-series-delete")) {
      withSeriesBusy((st) => deleteAdminSeries(st, attr("data-series-delete")));
    } else if (target.hasAttribute("data-series-tag-hygiene")) {
      openAdminTagInHygiene(attr("data-series-tag-hygiene"));
    }
  });
  panel.addEventListener("change", (event) => {
    const input = event.target.closest("input[data-series-sequence]");
    if (!input) return;
    const bookID = input.getAttribute("data-series-sequence");
    const orig = input.getAttribute("data-series-sequence-orig") || "";
    withSeriesBusy((st) => saveAdminSeriesSequence(st, bookID, input.value, orig));
  });
  panel.addEventListener("keydown", (event) => {
    if (event.key !== "Enter") return;
    const input = event.target.closest("input[data-series-name]");
    if (!input) return;
    event.preventDefault();
    const id = input.getAttribute("data-series-name");
    withSeriesBusy((st) => renameAdminSeries(st, id, input.value));
  });
}
if (typeof document !== "undefined") initAdminAudiobookSeriesPanel();
