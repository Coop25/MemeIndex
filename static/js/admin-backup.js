// Admin backup, restore, and migration from another instance.

function renderAdminBackupStatus() {
  if (!adminBackupStatus || !adminBackupExport || !adminBackupImport) return;

  const status = state.admin.backupStatus;
  const running = status?.state === "running";
  adminBackupExport.disabled = running || adminBackupImportBusy;
  adminBackupExport.textContent = running ? "Backup Running..." : "Create Backup";
  adminBackupImport.disabled = running || adminBackupImportBusy;
  if (adminBackupDiscard) adminBackupDiscard.disabled = adminBackupImportBusy;
  renderAdminBackupDownloads(status);

  adminBackupStatus.classList.toggle("admin-backup-status-running", running);
  adminBackupStatus.classList.toggle("admin-backup-status-failed", status?.state === "failed");
  if (!status) {
    adminBackupStatus.textContent = "Loading backup status...";
    return;
  }

  const availableSuffix = status.download_available
    ? " The previous completed backup remains available to download."
    : "";
  if (running) {
    const started = status.started_at ? ` Started ${formatDateTimeZoned(status.started_at)}.` : "";
    adminBackupStatus.textContent = `The server is building a backup.${started} You can leave this page and return later.${availableSuffix}`;
    return;
  }
  if (status.state === "ready" && status.download_available) {
    const partCount = status.parts?.length || 0;
    const details = [partCount > 1 ? `Backup ready in ${partCount} parts` : "Backup ready"];
    if (status.size_bytes) details.push(formatSize(Number(status.size_bytes)));
    if (status.completed_at) details.push(`completed ${formatDateTimeZoned(status.completed_at)}`);
    adminBackupStatus.textContent = `${details.join(" · ")}. Download the manifest and every part; ` +
      `they remain available until a newer backup completes.${adminBackupIncomingSuffix()}`;
    return;
  }
  if (status.state === "failed") {
    adminBackupStatus.textContent = `Backup failed: ${status.error || "the server could not create the archive"}.${availableSuffix}`;
    return;
  }
  if (status.state === "unavailable") {
    adminBackupStatus.textContent = status.error || "Portable backups are unavailable on this server.";
    return;
  }
  adminBackupStatus.textContent = `No server backup has been created yet.${adminBackupIncomingSuffix()}`;
}

// One link per backup file. Each points straight at the download endpoint so
// the browser streams a multi-gigabyte part to disk instead of buffering it.
function renderAdminBackupDownloads(status) {
  if (!adminBackupDownloads) return;
  const files = status?.download_available && status.filename
    ? [{ filename: status.filename, label: "Manifest", size: 0 }, ...(status.parts || []).map((part) => ({
      filename: part.filename,
      label: `Part ${part.index} of ${status.parts.length}`,
      size: Number(part.size_bytes) || 0,
    }))]
    : [];
  adminBackupDownloads.classList.toggle("hidden", files.length === 0);
  // The status poller re-renders every few seconds; only rebuild the links
  // when the backup changes, so a link is never swapped out mid-click.
  const key = files.map((file) => file.filename).join("\n");
  if (adminBackupDownloads.dataset.files === key) return;
  adminBackupDownloads.dataset.files = key;
  adminBackupDownloads.innerHTML = files.map((file) => `
    <a class="secondary-button admin-backup-download" href="/api/admin/backup/download?file=${encodeURIComponent(file.filename)}"
      download="${escapeHTML(file.filename)}" title="${escapeHTML(file.filename)}">
      ${escapeHTML(file.label)}${file.size ? ` · ${escapeHTML(formatSize(file.size))}` : ""}
    </a>`).join("");
}

function adminBackupIncomingSuffix() {
  const incoming = state.admin.backupIncoming;
  adminBackupDiscard?.classList.toggle("hidden", !incoming?.staged);
  if (!incoming?.staged || incoming.complete) return "";
  const missing = (incoming.parts || []).filter((part) => !part.received).map((part) => part.filename);
  return ` A restore of ${incoming.backup_id} is waiting on ${missing.length} of ${incoming.part_count} part(s): ` +
    `${missing.join(", ")}. Choose them with "Choose Backup to Import" to continue.`;
}

async function fetchAdminBackupIncoming() {
  const response = await fetch("/api/admin/backup/import/parts", { cache: "no-store" });
  state.admin.backupIncoming = response.ok ? await response.json() : null;
  return state.admin.backupIncoming;
}

async function fetchAdminBackupStatus() {
  const response = await fetch("/api/admin/backup/status", { cache: "no-store" });
  if (response.status === 401 || response.status === 403) {
    await expectAuthorized(response, "Failed to load backup status.");
    return null;
  }
  if (!response.ok) {
    state.admin.backupStatus = {
      state: "unavailable",
      download_available: false,
      error: await readAPIErrorMessage(response, "Failed to load backup status."),
    };
    renderAdminBackupStatus();
    return null;
  }

  state.admin.backupStatus = await response.json();
  await Promise.all([
    fetchAdminBackupIncoming().catch((error) => console.error(error)),
    fetchAdminMigration().catch((error) => console.error(error)),
  ]);
  renderAdminBackupStatus();
  return state.admin.backupStatus;
}

// --- Server-to-server migration ---------------------------------------------
// The source card hands out a token; the destination card pulls the source's
// whole library with it. Both run on the servers, so this page only polls.

let adminMigrationWatching = false;
let adminMigrationSample = null;
let adminMigrationRate = 0;

async function fetchAdminMigration() {
  const response = await fetch("/api/admin/migration", { cache: "no-store" });
  if (!response.ok) {
    state.admin.migration = null;
    renderAdminMigration();
    return null;
  }
  state.admin.migration = await response.json();
  renderAdminMigration();
  return state.admin.migration;
}

function renderAdminMigration() {
  const overview = state.admin.migration;
  adminMigrationSourceCard?.classList.toggle("hidden", !overview?.available || !overview.source_enabled);
  adminMigrationImportCard?.classList.toggle("hidden", !overview?.available || !overview.import_enabled);
  if (!overview) return;

  const token = overview.token || {};
  adminMigrationTokenRevoke?.classList.toggle("hidden", !token.active);
  if (adminMigrationTokenCreate) {
    adminMigrationTokenCreate.textContent = token.active ? "Generate New Token" : "Generate Migration Token";
  }
  if (!token.active) {
    adminMigrationTokenBox?.classList.add("hidden");
    if (adminMigrationTokenValue) adminMigrationTokenValue.textContent = "";
  }
  if (adminMigrationTokenStatus) {
    adminMigrationTokenStatus.textContent = token.active
      ? `A token is active until ${new Date(token.expires_at).toLocaleString()}.` +
        (adminMigrationTokenBox?.classList.contains("hidden") ? " It is only shown once; generate a new one if you lost it." : " Copy it now; it will not be shown again.")
      : "No migration token is active, so nothing can copy this server.";
  }

  renderAdminMigrationImport(overview.import || { state: "idle" });
}

function renderAdminMigrationImport(job) {
  const running = job.state === "running";
  if (adminMigrationStart) adminMigrationStart.disabled = running || adminBackupImportBusy;
  if (adminMigrationSource) adminMigrationSource.disabled = running;
  if (adminMigrationToken) adminMigrationToken.disabled = running;
  adminMigrationCancel?.classList.toggle("hidden", !running || job.phase === "restoring");
  adminMigrationProgress?.classList.toggle("hidden", job.state === "idle");
  if (job.state === "idle" || !adminMigrationProgressText || !adminMigrationBarFill) return;

  const received = Number(job.bytes_received) || 0;
  const estimated = Number(job.estimated_bytes) || 0;
  if (running && job.phase === "transferring") {
    const now = Date.now();
    if (adminMigrationSample && now > adminMigrationSample.at) {
      const instant = (received - adminMigrationSample.bytes) / ((now - adminMigrationSample.at) / 1000);
      adminMigrationRate = adminMigrationRate ? adminMigrationRate * 0.7 + instant * 0.3 : instant;
    }
    adminMigrationSample = { at: now, bytes: received };
  } else {
    adminMigrationSample = null;
    adminMigrationRate = 0;
  }

  let percent = 0;
  let indeterminate = false;
  let text = "";
  const source = job.source || "the source";
  if (running) {
    adminMigrationWatching = true;
    switch (job.phase) {
      case "snapshot":
        indeterminate = true;
        text = "Step 1 of 3: Saving a snapshot of this server so the import can be undone...";
        break;
      case "connecting":
        indeterminate = true;
        text = `Step 2 of 3: Connecting to ${source}...`;
        break;
      case "transferring": {
        // The estimate is the source's media size; the archive also carries
        // the database, so hold at 99% until the server says it is done.
        percent = estimated > 0 ? Math.min(99, (received / estimated) * 100) : 0;
        indeterminate = estimated <= 0;
        const parts = [`Step 2 of 3: Receiving the library from ${source}: ${formatSize(received)}`];
        if (estimated > 0) parts[0] += ` of ~${formatSize(estimated)} (${percent.toFixed(1)}%)`;
        if (adminMigrationRate > 0) {
          parts.push(`${formatSize(adminMigrationRate)}/s`);
          if (estimated > received) parts.push(`about ${formatDurationShort((estimated - received) / adminMigrationRate)} left`);
        }
        text = parts.join(" · ");
        break;
      }
      case "restoring":
        percent = 100;
        indeterminate = true;
        text = `Step 3 of 3: All ${formatSize(received)} received. Loading the database and moving the media into place...`;
        break;
      default:
        indeterminate = true;
        text = "Migration running...";
    }
  } else if (job.state === "completed") {
    percent = 100;
    text = `Migration from ${source} finished: ${formatSize(received)} imported.`;
    if (adminMigrationWatching) {
      adminMigrationWatching = false;
      text += " Reloading the restored library...";
      showToast("Migration finished.", "success", { title: "Backup & Restore" });
      window.setTimeout(forceFreshHTMLReload, 1500);
    }
  } else {
    if (adminMigrationWatching) {
      adminMigrationWatching = false;
      showToast(job.state === "cancelled" ? "Migration cancelled." : "Migration failed.", job.state === "cancelled" ? "info" : "error", { title: "Backup & Restore" });
    }
    text = job.state === "cancelled" ? job.error : `Migration failed: ${job.error || "unknown error"}`;
  }

  adminMigrationBar?.classList.toggle("is-indeterminate", indeterminate);
  adminMigrationBar?.classList.toggle("is-failed", job.state === "failed");
  adminMigrationBarFill.style.width = indeterminate ? "" : `${percent}%`;
  adminMigrationProgressText.textContent = text;
}

async function createAdminMigrationToken() {
  const active = state.admin.migration?.token?.active;
  if (active && !window.confirm("Generate a new token? The current token stops working immediately.")) return;
  adminMigrationTokenCreate.disabled = true;
  try {
    const response = await fetch("/api/admin/migration/token", { method: "POST" });
    if (!(await expectAuthorized(response, "Could not generate a migration token."))) {
      if (adminMigrationTokenStatus) adminMigrationTokenStatus.textContent = await readAPIErrorMessage(response, "Could not generate a migration token.");
      return;
    }
    const payload = await response.json();
    adminMigrationTokenValue.textContent = payload.token;
    adminMigrationTokenBox?.classList.remove("hidden");
    await fetchAdminMigration();
  } finally {
    adminMigrationTokenCreate.disabled = false;
  }
}

async function startAdminMigration() {
  const source = adminMigrationSource?.value.trim() || "";
  const token = adminMigrationToken?.value.trim() || "";
  if (!source || !token) {
    adminMigrationProgress?.classList.remove("hidden");
    adminMigrationProgressText.textContent = "Enter both the source address and its migration token.";
    return;
  }
  adminMigrationStart.disabled = true;
  adminMigrationProgress?.classList.remove("hidden");
  adminMigrationBar?.classList.add("is-indeterminate");
  adminMigrationProgressText.textContent = `Checking ${source}...`;
  try {
    const body = JSON.stringify({ source, token });
    const probe = await fetch("/api/admin/migration/probe", { method: "POST", headers: { "Content-Type": "application/json" }, body });
    if (!(await expectAuthorized(probe, "Could not reach the source server."))) {
      adminMigrationBar?.classList.remove("is-indeterminate");
      adminMigrationProgressText.textContent = await readAPIErrorMessage(probe, "Could not reach the source server.");
      return;
    }
    const { source: resolved, info } = await probe.json();
    const summary = `${resolved} (MemeIndex ${info.app_version}): ${info.memes} memes, ${info.audiobooks} audiobooks, ` +
      `${info.users} users, about ${formatSize(Number(info.estimated_bytes) || 0)}`;
    adminMigrationProgressText.textContent = `Found ${summary}.`;
    if (!confirmBackupReplace(`everything from ${summary}`)) {
      adminMigrationBar?.classList.remove("is-indeterminate");
      adminMigrationProgressText.textContent = "Migration not started.";
      return;
    }
    const response = await fetch("/api/admin/migration/import", { method: "POST", headers: { "Content-Type": "application/json" }, body });
    if (!(await expectAuthorized(response, "Could not start the migration."))) {
      adminMigrationBar?.classList.remove("is-indeterminate");
      adminMigrationProgressText.textContent = await readAPIErrorMessage(response, "Could not start the migration.");
      return;
    }
    adminMigrationWatching = true;
    if (adminMigrationToken) adminMigrationToken.value = "";
    showToast("Migration started on the server.", "success", { title: "Backup & Restore" });
    await fetchAdminMigration();
  } finally {
    renderAdminMigration();
  }
}

async function startAdminBackup() {
  if (!adminBackupExport || state.admin.backupStatus?.state === "running") return;
  adminBackupExport.disabled = true;
  adminBackupStatus.textContent = "Starting the server backup task...";

  const response = await fetch("/api/admin/backup/export", { method: "POST" });
  if (response.status === 409) {
    const contentType = response.headers.get("content-type") || "";
    if (contentType.includes("application/json")) {
      state.admin.backupStatus = await response.json();
    } else {
      state.admin.backupStatus = {
        state: "unavailable",
        download_available: false,
        error: await readAPIErrorMessage(response, "Backup creation is unavailable."),
      };
    }
    renderAdminBackupStatus();
    return;
  }
  if (!(await expectAuthorized(response, "Failed to start backup."))) {
    await fetchAdminBackupStatus();
    return;
  }

  state.admin.backupStatus = await response.json();
  renderAdminBackupStatus();
  showToast("Backup task started on the server.", "success", { title: "Backup & Restore" });
}

function syncAdminBackupPolling() {
  adminBackupPoller.sync();
}

function confirmBackupReplace(description) {
  const typed = window.prompt(
    `Import ${description}?\n\nThis REPLACES every meme and all database data on this server with the contents of the backup. ` +
    `The server automatically snapshots itself first, but the safest undo is your own export.\n\n` +
    `Type REPLACE to confirm:`,
    "",
  );
  if (typed === null) return false;
  if (typed.trim().toUpperCase() !== "REPLACE") {
    adminBackupStatus.textContent = "Import cancelled — confirmation text did not match.";
    showToast("Import cancelled.", "info", { title: "Backup & Restore" });
    return false;
  }
  return true;
}

function isBackupManifestFile(file) {
  return /\.manifest\.json$/i.test(file.name);
}

function isBackupPartFile(file) {
  return /\.tar\.gz\.part\d+$/i.test(file.name);
}

async function finishBackupImport(response) {
  if (!(await expectAuthorized(response, "Failed to import backup."))) {
    adminBackupStatus.textContent = await readAPIErrorMessage(response, "Failed to import backup.");
    return;
  }
  adminBackupStatus.textContent = "Backup imported successfully. Reloading the restored library...";
  showToast("Backup imported successfully.", "success", { title: "Backup & Restore" });
  window.setTimeout(forceFreshHTMLReload, 900);
}

async function importPortableBackup(files) {
  if (!files.length || !adminBackupImport) return;
  if (files.some(isBackupManifestFile) || files.some(isBackupPartFile)) {
    await importSplitPortableBackup(files);
    return;
  }
  if (files.length > 1) {
    adminBackupStatus.textContent = "Choose either one .tar.gz backup, or a backup's .manifest.json together with its part files.";
    return;
  }
  const [file] = files;
  if (!confirmBackupReplace(file.name)) return;

  adminBackupImportBusy = true;
  renderAdminBackupStatus();
  adminBackupStatus.textContent = "Importing backup. Keep this page open; large libraries can take several minutes...";
  try {
    const response = await fetch("/api/admin/backup/import", {
      method: "POST",
      headers: { "Content-Type": "application/gzip" },
      body: file,
    });
    await finishBackupImport(response);
  } finally {
    await endAdminBackupImport();
  }
}

// Refresh the panel without losing the import's own outcome message, which a
// plain re-render would replace with the generic backup status.
async function endAdminBackupImport() {
  adminBackupImportBusy = false;
  const message = adminBackupStatus.textContent;
  await fetchAdminBackupStatus();
  adminBackupStatus.textContent = message;
}

// A split backup restores in stages: the manifest tells the server how many
// parts to expect, each chosen part is uploaded and checked against it, and
// parts already on the server are skipped, so an interrupted import resumes by
// choosing just the files that are still missing. Once every part has
// arrived, the restore runs.
async function importSplitPortableBackup(files) {
  const manifestFile = files.find(isBackupManifestFile);
  let incoming;
  if (manifestFile) {
    const response = await fetch("/api/admin/backup/import/parts/manifest", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: await manifestFile.text(),
    });
    if (!(await expectAuthorized(response, "Could not read the backup manifest."))) {
      adminBackupStatus.textContent = await readAPIErrorMessage(response, "Could not read the backup manifest.");
      return;
    }
    incoming = await response.json();
  } else {
    incoming = await fetchAdminBackupIncoming();
    if (!incoming?.staged) {
      adminBackupStatus.textContent = "Include the backup's .manifest.json file so the server knows how many parts to expect.";
      return;
    }
  }

  const byName = new Map(files.map((file) => [file.name, file]));
  const partNames = new Set(incoming.parts.map((part) => part.filename));
  // Browsers save a repeat download as "name (1).ext", which no longer
  // matches the manifest - say so rather than silently skipping the file.
  const unmatched = files.filter((file) => !isBackupManifestFile(file) && !partNames.has(file.name)).map((file) => file.name);
  const unmatchedNote = unmatched.length
    ? ` Skipped ${unmatched.join(", ")}: not a part of ${incoming.backup_id} by name (rename a renamed download back to its original name).`
    : "";
  const missing = incoming.parts.filter((part) => !part.received);
  const toUpload = [];
  for (const part of missing) {
    const file = byName.get(part.filename);
    if (!file) continue;
    if (file.size !== Number(part.size_bytes)) {
      adminBackupStatus.textContent = `${part.filename} is ${formatSize(file.size)} but the manifest expects ` +
        `${formatSize(Number(part.size_bytes))} — the download may be incomplete. Download that part again.`;
      return;
    }
    toUpload.push({ part, file });
  }
  const willComplete = toUpload.length === missing.length;
  if (willComplete && !confirmBackupReplace(`${incoming.backup_id} (${incoming.part_count} part${incoming.part_count === 1 ? "" : "s"})`)) {
    return;
  }

  adminBackupImportBusy = true;
  renderAdminBackupStatus();
  try {
    for (const [position, { part, file }] of toUpload.entries()) {
      adminBackupStatus.textContent = `Uploading ${part.filename} (${position + 1} of ${toUpload.length} this time, ` +
        `${formatSize(file.size)}). Keep this page open...`;
      const response = await fetch(`/api/admin/backup/import/parts?file=${encodeURIComponent(part.filename)}`, {
        method: "PUT",
        headers: { "Content-Type": "application/octet-stream" },
        body: file,
      });
      if (!(await expectAuthorized(response, "Failed to upload a backup part."))) {
        adminBackupStatus.textContent = await readAPIErrorMessage(response, `Failed to upload ${part.filename}.`);
        return;
      }
      incoming = await response.json();
    }
    state.admin.backupIncoming = incoming;
    if (!incoming.complete) {
      const stillMissing = incoming.parts.filter((part) => !part.received).map((part) => part.filename);
      adminBackupStatus.textContent = `Received ${incoming.received_count} of ${incoming.part_count} parts of ${incoming.backup_id}. ` +
        `Still needed: ${stillMissing.join(", ")}. Choose those files when you have them; the parts already uploaded stay on the server.${unmatchedNote}`;
      showToast(`${incoming.received_count} of ${incoming.part_count} backup parts uploaded.`, "info", { title: "Backup & Restore" });
      return;
    }
    adminBackupStatus.textContent = "All backup parts received. Restoring the server; large libraries can take several minutes...";
    const response = await fetch("/api/admin/backup/import/parts/restore", { method: "POST" });
    await finishBackupImport(response);
  } finally {
    await endAdminBackupImport();
  }
}
