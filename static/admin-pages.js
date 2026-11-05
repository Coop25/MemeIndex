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
  const response = await fetch(url, options);
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
  const response = await fetch(`/api/admin/audiobooks/uploads/${encodeURIComponent(id)}`, { method: "DELETE" });
  if (!(await expectAuthorized(response, "Failed to discard the upload."))) return;
  let body = null;
  if (response.status !== 204) { try { body = await response.json(); } catch (error) { body = null; } }
  if (!response.ok) {
    s.status = (body && body.error) || "Failed to discard the upload.";
    return;
  }
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
