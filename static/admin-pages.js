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
    const label = item.kind === "meme" && item.id
      ? `<button type="button" class="text-button admin-storage-open" data-storage-open-meme="${escapeHTML(String(item.id))}">${name}</button>`
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
  if (!result.ok) return false;
  adminStorageState().data = result.body;
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

async function deleteStorageOrphans(s) {
  const orphans = (s.scan && s.scan.orphans) || [];
  const chosen = orphans.filter((o) => s.selected.has(o.path));
  if (!chosen.length) return;
  const bytes = chosen.reduce((sum, o) => sum + (o.bytes || 0), 0);
  const message = `Delete ${chosen.length} files (${formatSize(bytes)})? This cannot be undone.`;
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
async function openAdminLibraryAudiobook(id) {
  const s = adminLibraryState();
  s.status = "Opening...";
  renderAdminAudiobookLibrary();
  const result = await adminPagesFetchJSON(`/api/audiobooks/${encodeURIComponent(id)}`, undefined, "Could not open the audiobook.");
  if (!result.ok || !result.body) {
    s.status = result.error || "Could not open the audiobook.";
    renderAdminAudiobookLibrary();
    return;
  }
  s.status = "";
  renderAdminAudiobookLibrary();
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
