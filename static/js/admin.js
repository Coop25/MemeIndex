// Admin views: data loading, users and permissions, shares, the delete
// queue, audit logs, and polling.

let managedUsersState = [];
let deleteQueueState = [];
let auditLogState = [];

// createAdminPoller builds a self-scheduling poller that only runs while its
// shouldRun() predicate holds AND the tab is visible, and that backs off
// geometrically when a request fails so a broken endpoint is not hammered.
function createAdminPoller({ baseInterval, maxInterval = 60000, shouldRun, tick }) {
  let timer = null;
  let delay = baseInterval;

  function stop() {
    if (timer) {
      window.clearTimeout(timer);
      timer = null;
    }
    delay = baseInterval;
  }

  function schedule() {
    if (timer) window.clearTimeout(timer);
    timer = window.setTimeout(run, delay);
  }

  async function run() {
    timer = null;
    if (!shouldRun() || document.hidden) return;
    try {
      await tick();
      delay = baseInterval;
    } catch (error) {
      console.error(error);
      delay = Math.min(maxInterval, Math.max(baseInterval, delay * 2));
    }
    if (shouldRun() && !document.hidden) schedule();
  }

  return {
    sync() {
      if (shouldRun() && !document.hidden) {
        if (!timer) schedule();
      } else {
        stop();
      }
    },
    stop,
  };
}

async function fetchManagedUsers() {
  const response = await fetch("/api/users");
  if (!(await expectAuthorized(response, "Failed to load users."))) {
    return null;
  }

  const payload = await response.json();
  managedUsersState = payload.users || [];
  renderManagedUsers();
  return managedUsersState;
}

async function fetchDeleteQueue() {
  const queueState = state.admin.queue;
  const params = new URLSearchParams({
    offset: `${queueState.offset}`,
    limit: `${queueState.limit}`,
  });
  const response = await fetch(`/api/admin/memes/pending-delete?${params.toString()}`);
  if (!(await expectAuthorized(response, "Failed to load delete queue."))) {
    return null;
  }

  const payload = await response.json();
  deleteQueueState = payload.memes || [];
  queueState.total = Number(payload.total || 0);
  queueState.loaded = true;
  syncAdminWorkQueue(state.admin, "delete_queue", queueState.total);
  refreshAdminTabBadges();
  queueState.hasMore = !!payload.has_more;
  renderDeleteQueue();
  syncAdminPagination();
  return deleteQueueState;
}

async function fetchAuditLogs() {
  const auditState = state.admin.audit;
  const params = new URLSearchParams({
    offset: `${auditState.offset}`,
    limit: `${auditState.limit}`,
  });
  if (state.admin.auditActor && isAdminView() && activeAdminTab() === "audit-logs") params.set("actor", state.admin.auditActor.user_id);
  const response = await fetch(`/api/admin/audit-logs?${params.toString()}`);
  if (!(await expectAuthorized(response, "Failed to load audit logs."))) {
    return null;
  }

  const payload = await response.json();
  auditLogState = payload.events || [];
  auditState.total = Number(payload.total || 0);
  auditState.hasMore = !!payload.has_more;
  renderAuditLogs();
  syncAdminPagination();
  return auditLogState;
}

async function fetchAdminDashboard() {
  const response = await fetch("/api/admin/dashboard");
  if (!(await expectAuthorized(response, "Failed to load admin dashboard."))) {
    return null;
  }

  const payload = await response.json();
  state.admin.dashboard = payload || null;
  renderAdminDashboard();
  return state.admin.dashboard;
}

async function fetchMemeAudit(memeID, limit = 5) {
  const response = await fetch(`/api/admin/memes/${encodeURIComponent(memeID)}/audit?limit=${encodeURIComponent(limit)}`);
  if (!(await expectAuthorized(response, "Failed to load activity."))) {
    return null;
  }

  const payload = await response.json();
  return payload.events || [];
}

function userPermissionSummary(user) {
  if (user.is_super_admin) {
    return "Super Admin";
  }

  const labels = [];
  if (user.permissions?.canView) labels.push("View");
  if (user.permissions?.canUpload) labels.push("Upload");
  if (user.permissions?.canAddTags) labels.push("Add tags");
  if (user.permissions?.canRemoveTags) labels.push("Remove tags");
  if (user.permissions?.canDeleteMemes) labels.push("Delete");
  if (user.permissions?.canViewAudiobooks) labels.push("View audiobooks");
  if (user.permissions?.canUploadAudiobooks) labels.push("Upload audiobooks");
  if (user.permissions?.canDeleteAudiobooks) labels.push("Delete audiobooks");
  if (user.permissions?.canManageAudiobookSeries) labels.push("Manage series");
  if (user.permissions?.audiobookTagRestriction?.length) labels.push(`Restricted: ${user.permissions.audiobookTagRestriction.join(", ")}`);
  return labels.length > 0 ? labels.join(" * ") : "No permissions yet";
}

function hasZeroPermissions(user) {
  if (user?.is_super_admin) {
    return false;
  }

  const permissions = user?.permissions || {};
  return !permissions.canView
    && !permissions.canUpload
    && !permissions.canAddTags
    && !permissions.canRemoveTags
    && !permissions.canDeleteMemes
    && !permissions.canManageUsers
    && !permissions.canViewAudiobooks
    && !permissions.canUploadAudiobooks
    && !permissions.canDeleteAudiobooks
    && !permissions.canManageAudiobookSeries;
}

function renderManagedUsers() {
  const target = isAdminView() && activeAdminTab() === "users" && adminUsersList ? adminUsersList : usersList;
  if (!target) return;

  target.innerHTML = "";
  if (managedUsersState.length === 0) {
    target.innerHTML = `<p class="users-empty">No managed users yet.</p>`;
    return;
  }

  const sortedUsers = [...managedUsersState].sort((left, right) => {
    const leftLastActive = Number(left?.last_active_at || 0);
    const rightLastActive = Number(right?.last_active_at || 0);
    return rightLastActive - leftLastActive;
  });

  sortedUsers.forEach((user) => {
      const card = document.createElement("article");
    card.className = hasZeroPermissions(user) ? "users-card users-card-pending" : "users-card";
      card.dataset.userId = user.user_id;

    const avatar = user.avatar_url
      ? `<img class="users-avatar" src="${escapeHTML(user.avatar_url)}" alt="" />`
      : `<div class="users-avatar users-avatar-fallback">${escapeHTML((user.display_name || user.username || user.user_id || "U").trim().slice(0, 2).toUpperCase())}</div>`;

    const disabledAttr = user.is_super_admin ? "disabled" : "";
    const checked = (value) => value ? "checked" : "";

    card.innerHTML = `
      <div class="users-card-head">
        <div class="users-identity">
          ${avatar}
          <div class="users-copy">
            <strong>${escapeHTML(user.display_name || user.username || "Awaiting Discord login")}</strong>
            <span>${user.username ? `@${escapeHTML(user.username)}` : "Discord profile will appear after first login"}</span>
            <code>${escapeHTML(user.user_id)}</code>
            <span>Last active: ${escapeHTML(formatLastActive(user.last_active_at))}</span>
          </div>
        </div>
          <div class="users-badges">
            <span class="users-scope">${escapeHTML(userPermissionSummary(user))}</span>
            ${hasZeroPermissions(user) ? '<span class="users-pending-badge">Pending Access</span>' : ""}
            ${user.is_super_admin ? '<span class="users-super-admin">Env Super Admin</span>' : ""}
          </div>
        </div>
      <div class="users-permissions">
        <label><input type="checkbox" data-scope="canView" ${checked(user.permissions?.canView)} ${disabledAttr} /> <span>View</span></label>
        <label><input type="checkbox" data-scope="canUpload" ${checked(user.permissions?.canUpload)} ${disabledAttr} /> <span>Upload</span></label>
        <label><input type="checkbox" data-scope="canAddTags" ${checked(user.permissions?.canAddTags)} ${disabledAttr} /> <span>Add tags</span></label>
        <label><input type="checkbox" data-scope="canRemoveTags" ${checked(user.permissions?.canRemoveTags)} ${disabledAttr} /> <span>Remove tags</span></label>
        <label><input type="checkbox" data-scope="canDeleteMemes" ${checked(user.permissions?.canDeleteMemes)} ${disabledAttr} /> <span>Delete memes</span></label>
        <label><input type="checkbox" data-scope="canViewAudiobooks" ${checked(user.permissions?.canViewAudiobooks)} ${disabledAttr} /> <span>View audiobooks</span></label>
        <label><input type="checkbox" data-scope="canUploadAudiobooks" ${checked(user.permissions?.canUploadAudiobooks)} ${disabledAttr} /> <span>Upload audiobooks</span></label>
        <label><input type="checkbox" data-scope="canDeleteAudiobooks" ${checked(user.permissions?.canDeleteAudiobooks)} ${disabledAttr} /> <span>Delete audiobooks</span></label>
        <label><input type="checkbox" data-scope="canManageAudiobookSeries" ${checked(user.permissions?.canManageAudiobookSeries)} ${disabledAttr} /> <span>Manage audiobook series</span></label>
      </div>
      <label class="field users-tag-restriction">
        <span>Restrict audiobooks to tags (blank = unrestricted)</span>
        <input type="text" data-scope="audiobookTagRestriction" value="${escapeHTML((user.permissions?.audiobookTagRestriction || []).join(", "))}" placeholder="e.g. kids" autocomplete="off" ${disabledAttr} />
      </label>
      ${user.is_super_admin ? "" : '<div class="users-card-actions"><button class="danger-button users-delete-button" type="button">Remove User</button><button class="primary-button users-save-button" type="button">Save Permissions</button></div>'}
      `;

      const saveButton = card.querySelector(".users-save-button");
      const deleteButton = card.querySelector(".users-delete-button");
      if (saveButton) {
        saveButton.addEventListener("click", async () => {
        const permissions = {
          canView: !!card.querySelector('input[data-scope="canView"]')?.checked,
          canUpload: !!card.querySelector('input[data-scope="canUpload"]')?.checked,
          canAddTags: !!card.querySelector('input[data-scope="canAddTags"]')?.checked,
          canRemoveTags: !!card.querySelector('input[data-scope="canRemoveTags"]')?.checked,
          canDeleteMemes: !!card.querySelector('input[data-scope="canDeleteMemes"]')?.checked,
          canViewAudiobooks: !!card.querySelector('input[data-scope="canViewAudiobooks"]')?.checked,
          canUploadAudiobooks: !!card.querySelector('input[data-scope="canUploadAudiobooks"]')?.checked,
          canDeleteAudiobooks: !!card.querySelector('input[data-scope="canDeleteAudiobooks"]')?.checked,
          canManageAudiobookSeries: !!card.querySelector('input[data-scope="canManageAudiobookSeries"]')?.checked,
          audiobookTagRestriction: (card.querySelector('input[data-scope="audiobookTagRestriction"]')?.value || "")
            .split(",").map((tag) => tag.trim().toLowerCase()).filter(Boolean),
        };
        if (permissions.canUpload || permissions.canAddTags || permissions.canRemoveTags || permissions.canDeleteMemes) {
          permissions.canView = true;
          const viewToggle = card.querySelector('input[data-scope="canView"]');
          if (viewToggle) {
            viewToggle.checked = true;
          }
        }
        if (permissions.canUploadAudiobooks || permissions.canDeleteAudiobooks || permissions.canManageAudiobookSeries) {
          permissions.canViewAudiobooks = true;
          const viewAudiobooksToggle = card.querySelector('input[data-scope="canViewAudiobooks"]');
          if (viewAudiobooksToggle) {
            viewAudiobooksToggle.checked = true;
          }
        }

        const response = await fetch(`/api/users/${encodeURIComponent(user.user_id)}`, {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(permissions),
        });
        if (!(await expectAuthorized(response, "Failed to save user permissions."))) {
          return;
        }

          setUsersStatus(`Saved permissions for ${user.display_name || user.username || user.user_id}.`);
          showToast(`Saved permissions for ${user.display_name || user.username || user.user_id}.`, "success", { title: "Users" });
          await fetchManagedUsers();
        });
      }
      if (deleteButton) {
        deleteButton.addEventListener("click", async () => {
          const label = user.display_name || user.username || user.user_id;
          const confirmed = window.confirm(`Remove ${label} completely?\n\nThey will lose stored access and will not be auto-added again on Discord login until an admin manually adds them back.`);
          if (!confirmed) {
            return;
          }

          const response = await fetch(`/api/users/${encodeURIComponent(user.user_id)}`, {
            method: "DELETE",
          });
          if (!(await expectAuthorized(response, "Failed to remove user."))) {
            return;
          }

          setUsersStatus(`Removed ${label}. They will need to be manually added again before permissions can be restored.`);
          showToast(`Removed ${label}.`, "success", { title: "Users" });
          await fetchManagedUsers();
        });
      }

      target.appendChild(card);
    });
}

async function openUsersModal() {
  if (!canManageUsers() || !usersModal) return;
  setUsersStatus("Loading users...");
  if (!usersModal.open) {
    usersModal.showModal();
  }
  const users = await fetchManagedUsers();
  setUsersStatus(users ? "" : "Could not load users.");
}

function setUsersStatus(message) {
  if (usersModalStatus) {
    usersModalStatus.textContent = message;
  }
  if (adminUsersStatus) {
    adminUsersStatus.textContent = message;
  }
}

async function submitManagedUserAdd(userID) {
  const response = await fetch("/api/users", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ user_id: userID }),
  });
  if (!(await expectAuthorized(response, "Failed to add user."))) {
    setUsersStatus("Could not add user.");
    return false;
  }

  setUsersStatus(`Added ${userID}.`);
  showToast(`Added ${userID}.`, "success", { title: "Users" });
  await fetchManagedUsers();
  return true;
}

async function fetchAdminShares() {
  const page = state.admin.sharesPage;
  const params = new URLSearchParams({ offset: String(page.offset || 0), limit: String(page.limit || 25) });
  const response = await fetch(`/api/admin/shares?${params.toString()}`, { cache: "no-store" });
  if (!(await expectAuthorized(response, "Failed to load shared memes."))) return null;
  const payload = await response.json();
  state.admin.shares = Array.isArray(payload.shares) ? payload.shares : [];
  // The server pulls an out-of-range offset back to the last page, so trust its
  // numbers over the ones that were requested.
  page.total = Math.max(0, Number(payload.total ?? state.admin.shares.length));
  page.offset = Math.max(0, Number(payload.offset || 0));
  page.limit = Math.max(1, Number(payload.limit || page.limit || 25));
  page.hasMore = Boolean(payload.has_more);
  renderAdminShares();
  return state.admin.shares;
}

function renderAdminShares() {
  if (!adminViewTable || activeAdminTab() !== "shares") return;
  const shares = Array.isArray(state.admin.shares) ? state.admin.shares : [];
  const page = state.admin.sharesPage;
  const total = Math.max(Number(page.total || 0), shares.length);
  const layout = document.createElement("div");
  layout.className = "admin-shares-layout";
  if (shares.length) {
    const toolbar = document.createElement("div");
    toolbar.className = "admin-shares-toolbar";
    toolbar.innerHTML = `<div><strong>${total.toLocaleString()} active share link${total === 1 ? "" : "s"}</strong><span>Revoke individual links below or remove public access from all of them.</span></div><button class="danger-button shared-revoke-all-button" type="button">Revoke All</button>`;
    toolbar.querySelector(".shared-revoke-all-button")?.addEventListener("click", async (event) => {
      if (!window.confirm(`Revoke all ${total.toLocaleString()} active share link${total === 1 ? "" : "s"}?\n\nEvery existing public link will stop working immediately, including the ones on other pages.`)) return;
      const button = event.currentTarget;
      button.disabled = true;
      const response = await fetch("/api/admin/shares", { method: "DELETE" });
      if (!(await expectAuthorized(response, "Failed to revoke all shares."))) {
        button.disabled = false;
        return;
      }
      const payload = await response.json();
      const revoked = Number(payload.revoked || 0);
      showToast(`${revoked.toLocaleString()} share link${revoked === 1 ? "" : "s"} revoked.`, "success", { title: "Public Access Removed", duration: 3600 });
      state.admin.sharesPage.offset = 0;
      await fetchAdminShares();
      setAdminViewStatus("No memes are currently shared.");
    });
    layout.appendChild(toolbar);
  }
  const table = document.createElement("div");
  table.className = "admin-table shared-memes-table";
  table.innerHTML = `
    <div class="admin-table-head shared-memes-table-head">
      <span>Preview</span><span>Meme</span><span>Shared By</span><span>Expires</span><span>Actions</span>
    </div>`;

  shares.forEach((entry) => {
    const meme = entry.meme || {};
    const share = entry.share || {};
    const sharedByID = String(share.shared_by_user_id || "").trim();
    const sharedByDisplayName = String(entry.shared_by_display_name || sharedByID || "Local user").trim();
    const sharedByIDDetail = sharedByID && sharedByID !== sharedByDisplayName
      ? `<code>${escapeHTML(sharedByID)}</code>`
      : "";
    const row = document.createElement("article");
    row.className = "admin-table-row shared-memes-table-row";
    row.innerHTML = `
      <div class="admin-table-cell admin-table-preview-cell" data-label="Preview"><div class="shared-meme-preview"></div></div>
      <div class="admin-table-cell shared-meme-name-cell" data-label="Meme"><div class="users-copy"><strong>${escapeHTML(meme.originalName || "Unknown meme")}</strong><code>${escapeHTML(meme.id || "")}</code></div></div>
      <div class="admin-table-cell shared-meme-owner-cell" data-label="Shared By"><div class="users-copy"><strong>${escapeHTML(sharedByDisplayName)}</strong>${sharedByIDDetail}</div></div>
      <div class="admin-table-cell shared-meme-expiry-cell" data-label="Expires"><span title="${escapeHTML(formatDateTimeZoned(share.expires_at))}">${escapeHTML(formatRelativeTime(share.expires_at))}</span></div>
      <div class="admin-table-cell shared-meme-actions-cell" data-label="Actions"><div class="admin-row-actions">
        <button class="ghost-button shared-copy-button" type="button">Copy Link</button>
        <button class="ghost-button shared-open-button" type="button">Open Meme</button>
        <button class="ghost-button shared-revoke-button" type="button">Revoke</button>
      </div></div>`;
    const preview = row.querySelector(".shared-meme-preview");
    if (preview && meme.id) preview.appendChild(buildPreview(meme));
    row.querySelector(".shared-copy-button")?.addEventListener("click", async () => {
      try {
        await copyShareText(String(entry.page_url || entry.url || ""));
        showToast("Share link copied.", "success", { title: "Shared Memes" });
      } catch (error) {
        showToast("Could not copy the share link.", "error", { title: "Shared Memes" });
      }
    });
    row.querySelector(".shared-open-button")?.addEventListener("click", () => openAdminMemeByID(meme.id));
    row.querySelector(".shared-revoke-button")?.addEventListener("click", async () => {
      if (!window.confirm(`Revoke public access to ${meme.originalName || "this meme"}?`)) return;
      const response = await fetch(`/api/admin/shares/${encodeURIComponent(meme.id)}`, { method: "DELETE" });
      if (!(await expectAuthorized(response, "Failed to revoke share."))) return;
      showToast("Public access has been removed.", "success", { title: "Share Revoked" });
      await fetchAdminShares();
      setAdminViewStatus(state.admin.shares.length ? "" : "No memes are currently shared.");
    });
    table.appendChild(row);
  });
  layout.appendChild(table);
  if (total > page.limit) {
    const pageNumber = Math.floor(page.offset / page.limit) + 1;
    const pageCount = Math.max(1, Math.ceil(total / page.limit));
    const pager = document.createElement("nav");
    pager.className = "admin-tag-review-pagination";
    pager.setAttribute("aria-label", "Shared meme pages");
    pager.innerHTML = `
      <button type="button" class="admin-tag-page-button" data-admin-shares-page="prev" ${page.offset <= 0 ? "disabled" : ""}>Previous</button>
      <span>Page ${pageNumber.toLocaleString()} of ${pageCount.toLocaleString()} · ${(page.offset + 1).toLocaleString()}-${(page.offset + shares.length).toLocaleString()} of ${total.toLocaleString()}</span>
      <button type="button" class="admin-tag-page-button" data-admin-shares-page="next" ${page.hasMore ? "" : "disabled"}>Next</button>`;
    pager.querySelectorAll("[data-admin-shares-page]").forEach((button) => {
      button.addEventListener("click", () => {
        if (button.disabled) return;
        const delta = button.getAttribute("data-admin-shares-page") === "prev" ? -page.limit : page.limit;
        page.offset = Math.max(0, page.offset + delta);
        fetchAdminShares().catch((error) => console.error(error));
      });
    });
    layout.appendChild(pager);
  }
  adminViewTable.replaceChildren(layout);
}

function renderDeleteQueue() {
  const target = isAdminView() && activeAdminTab() === "delete-queue" && adminViewTable ? adminViewTable : deleteQueueList;
  if (!target) return;

  target.innerHTML = "";
  if (deleteQueueState.length === 0) {
    target.innerHTML = `<p class="users-empty">No pending delete requests.</p>`;
    return;
  }

  const table = document.createElement("div");
  table.className = "admin-table delete-queue-table";
  table.innerHTML = `
    <div class="admin-table-head">
      <span>Preview</span>
      <span>Meme</span>
      <span>Requested By</span>
      <span>Requested At</span>
      <span>Actions</span>
    </div>
  `;

  deleteQueueState.forEach((entry) => {
    const card = document.createElement("article");
    card.className = "admin-table-row delete-queue-table-row";
    const requestedBy = entry.requested_by?.display_name || entry.requested_by?.username || entry.requested_by?.user_id || "Unknown user";
    const previewMarkup = buildDeleteQueuePreviewMarkup(entry.meme);
    card.innerHTML = `
      <div class="admin-table-cell admin-table-preview-cell" data-label="Preview">
        <div class="delete-queue-preview-wrap admin-inline-preview">
          ${previewMarkup}
        </div>
      </div>
      <div class="admin-table-cell" data-label="Meme">
        <div class="users-copy">
          <strong>${escapeHTML(entry.meme?.originalName || "Unknown meme")}</strong>
          <span>${escapeHTML(formatSize(Number(entry.meme?.sizeBytes || 0)))} * ${escapeHTML(entry.meme?.contentType || "unknown")}</span>
          <code>${escapeHTML(entry.meme?.id || "")}</code>
        </div>
      </div>
      <div class="admin-table-cell" data-label="Requested By">
        <div class="users-copy">
          <strong>${escapeHTML(requestedBy)}</strong>
          <span>${escapeHTML(entry.requested_by?.username ? `@${entry.requested_by.username}` : entry.requested_by?.user_id || "")}</span>
        </div>
      </div>
      <div class="admin-table-cell" data-label="Requested At">
        <span title="${escapeHTML(formatDateTimeZoned(entry.requested_at))}">${escapeHTML(formatRelativeTime(entry.requested_at))}</span>
      </div>
      <div class="admin-table-cell" data-label="Actions">
        <div class="admin-row-actions">
          <button class="ghost-button queue-open-modal-button" type="button">Open In Editor</button>
          <a class="ghost-button delete-queue-open-button" href="${escapeHTML(entry.meme?.filePath || "#")}" target="_blank" rel="noreferrer">Open Original</a>
          <button class="ghost-button queue-reject-button" type="button">Keep Meme</button>
          <button class="danger-button queue-approve-button" type="button">Approve Delete</button>
        </div>
      </div>
    `;

    card.querySelector(".queue-open-modal-button")?.addEventListener("click", () => {
      openModalWithMeme(entry.meme);
    });

    card.querySelector(".queue-reject-button")?.addEventListener("click", async () => {
      const response = await fetch(`/api/admin/memes/${encodeURIComponent(entry.meme.id)}/reject-delete`, {
        method: "POST",
      });
      if (!(await expectAuthorized(response, "Failed to reject delete."))) {
        return;
      }
      deleteQueueStatus.textContent = `Kept ${entry.meme.originalName}.`;
      showToast(`Kept ${entry.meme.originalName}.`, "success", { title: "Delete Queue" });
      if (deleteQueueState.length === 1 && state.admin.queue.offset > 0) {
        state.admin.queue.offset = Math.max(0, state.admin.queue.offset - state.admin.queue.limit);
      }
      await fetchDeleteQueue();
      await loadInitialMemes();
    });

    card.querySelector(".queue-approve-button")?.addEventListener("click", async () => {
      const label = entry.meme.originalName || "this meme";
      if (!window.confirm(`Permanently delete ${label}?\n\nThe file, its thumbnail, and all of its tags and favorites are removed. This cannot be undone.`)) {
        return;
      }
      const response = await fetch(`/api/admin/memes/${encodeURIComponent(entry.meme.id)}/approve-delete`, {
        method: "POST",
      });
      if (!(await expectAuthorized(response, "Failed to approve delete."))) {
        return;
      }
      deleteQueueStatus.textContent = `Deleted ${entry.meme.originalName}.`;
      showToast(`Deleted ${entry.meme.originalName}.`, "success", { title: "Delete Queue" });
      if (deleteQueueState.length === 1 && state.admin.queue.offset > 0) {
        state.admin.queue.offset = Math.max(0, state.admin.queue.offset - state.admin.queue.limit);
      }
      await fetchDeleteQueue();
      await loadInitialMemes();
    });

    table.appendChild(card);
  });

  target.appendChild(table);
}

async function openDeleteQueueModal() {
  if (!canManageUsers() || !deleteQueueModal) return;
  deleteQueueStatus.textContent = "Loading delete queue...";
  if (!deleteQueueModal.open) {
    deleteQueueModal.showModal();
  }
  const entries = await fetchDeleteQueue();
  deleteQueueStatus.textContent = entries ? "" : "Could not load delete queue.";
}

function renderAuditLogs() {
  const target = isAdminView() && activeAdminTab() === "audit-logs" && adminViewTable ? adminViewTable : auditLogsList;
  if (!target) return;

  target.innerHTML = "";
  const auditActor = state.admin.auditActor;
  if (auditActor && target === adminViewTable) {
    const chip = document.createElement("div");
    chip.className = "audit-actor-chip";
    chip.innerHTML = `<button type="button" class="ghost-button" data-clear-audit-actor aria-label="Clear user filter">Showing activity for ${escapeHTML(auditActor.display_name)} <span aria-hidden="true">×</span></button>`;
    chip.querySelector("[data-clear-audit-actor]").addEventListener("click", () => {
      setAdminAuditActor(null);
      loadInitialMemes().catch((error) => console.error(error));
    });
    target.appendChild(chip);
  }
  if (auditLogState.length === 0) {
    target.insertAdjacentHTML("beforeend", `<p class="users-empty">${auditActor ? "No audit activity recorded for this user." : "No audit activity recorded yet."}</p>`);
    return;
  }

  const table = document.createElement("div");
  table.className = "admin-table audit-log-table";
  table.innerHTML = `
    <div class="admin-table-head audit-log-table-head">
      <span>Time</span>
      <span>Action</span>
      <span>Actor</span>
      <span>Meme</span>
      <span>Actions</span>
    </div>
  `;

  auditLogState.forEach((event) => {
    const row = document.createElement("article");
    row.className = "admin-table-row audit-log-table-row";
    const actorName = event.actor?.display_name || event.actor?.username || event.actor?.user_id || "Unknown user";
    const actorHandle = event.actor?.username ? `@${event.actor.username}` : "";
    const isSystemEvent = !event.meme_id;
    const memeTitle = isSystemEvent ? "System action" : (event.meme_original_name || "Unknown or deleted meme");
    const canOpenEditor = !!event.meme_id && !!event.meme_file_path;
    const actionsMarkup = canOpenEditor
      ? `
        <div class="admin-row-actions">
          <button class="ghost-button audit-log-open-modal-button" type="button">Edit Meme</button>
          <a class="ghost-button audit-log-open-button" href="${escapeHTML(event.meme_file_path)}" target="_blank" rel="noreferrer">Open File</a>
        </div>
      `
      : `<span class="users-empty">${isSystemEvent ? "Workspace-wide action" : "Meme no longer available"}</span>`;

    row.innerHTML = `
      <div class="admin-table-cell" data-label="Time">
        <span title="${escapeHTML(formatDateTimeZoned(event.created_at))}">${escapeHTML(formatRelativeTime(event.created_at))}</span>
      </div>
      <div class="admin-table-cell" data-label="Action">
        <div class="users-copy">
          <strong>${escapeHTML(event.description || event.action || "Activity")}</strong>
          <span>${escapeHTML((event.action || "activity").replaceAll("_", " "))}</span>
        </div>
      </div>
      <div class="admin-table-cell" data-label="Actor">
        <div class="users-copy">
          <strong>${escapeHTML(actorName)}</strong>
          <span>${escapeHTML(actorHandle || event.actor?.user_id || "")}</span>
        </div>
      </div>
      <div class="admin-table-cell" data-label="Meme">
        <div class="users-copy">
          <strong>${escapeHTML(memeTitle)}</strong>
          <span>${escapeHTML(isSystemEvent ? "—" : (event.meme_content_type || "Unknown type"))}</span>
          <code>${escapeHTML(event.meme_id || "")}</code>
        </div>
      </div>
      <div class="admin-table-cell" data-label="Actions">
        ${actionsMarkup}
      </div>
    `;

    row.querySelector(".audit-log-open-modal-button")?.addEventListener("click", async () => {
      await openAdminMemeByID(event.meme_id);
    });

    table.appendChild(row);
  });

  target.appendChild(table);
}

async function openAuditLogsModal() {
  if (!canManageUsers() || !auditLogsModal) return;
  auditLogsStatus.textContent = "Loading audit logs...";
  if (!auditLogsModal.open) {
    auditLogsModal.showModal();
  }
  const events = await fetchAuditLogs();
  auditLogsStatus.textContent = events ? "" : "Could not load audit logs.";
}

function buildDeleteQueuePreviewMarkup(meme) {
  if (!meme) {
    return `<div class="file-icon"><strong>FILE</strong><span>Missing meme</span></div>`;
  }

  const filePath = escapeHTML(meme.filePath || "#");
  const previewPath = escapeHTML(meme.previewPath || meme.filePath || "#");
  const originalName = escapeHTML(meme.originalName || "Queued meme");
  const contentType = `${meme.contentType || ""}`;

  if (contentType.startsWith("image/")) {
    return `<img class="delete-queue-preview-media" src="${filePath}" alt="${originalName}" loading="lazy" />`;
  }

  if (contentType.startsWith("video/")) {
    return `
      <video class="delete-queue-preview-media" src="${filePath}" controls preload="metadata" playsinline muted></video>
    `;
  }

  if (contentType.startsWith("audio/")) {
    return `
      <div class="delete-queue-audio-preview">
        <div class="file-icon">
          <strong>${escapeHTML(pickIcon(contentType))}</strong>
          <span>${escapeHTML(contentType)}</span>
        </div>
        <audio src="${filePath}" controls preload="metadata"></audio>
      </div>
    `;
  }

  return `
    <a class="delete-queue-file-preview" href="${filePath}" target="_blank" rel="noreferrer">
      <div class="file-icon">
        <strong>${escapeHTML(pickIcon(contentType || originalName))}</strong>
        <span>${escapeHTML(contentType || "file")}</span>
        <span>Open original to review</span>
      </div>
    </a>
  `;
}

async function loadModalAudit(memeID) {
  if (!modalAuditSection || !modalAuditList) return;
  if (!canManageUsers()) {
    setModalAuditVisibility(false);
    return;
  }

  setModalAuditVisibility(true);
  modalAuditList.innerHTML = `<p class="users-empty">Loading activity...</p>`;
  const events = await fetchMemeAudit(memeID, 5);
  if (!events) {
    modalAuditList.innerHTML = `<p class="users-empty">Could not load activity.</p>`;
    return;
  }
  if (events.length === 0) {
    modalAuditList.innerHTML = `<p class="users-empty">No activity recorded yet.</p>`;
    return;
  }

  modalAuditList.innerHTML = "";
  events.forEach((event) => {
    const row = document.createElement("div");
    row.className = "modal-audit-entry";
    const actorName = event.actor?.display_name || event.actor?.username || event.actor?.user_id || "Unknown user";
    row.innerHTML = `
      <strong>${escapeHTML(event.description || event.action || "Activity")}</strong>
      <span>${escapeHTML(actorName)} * ${escapeHTML(formatDateTime(event.created_at))}</span>
    `;
    modalAuditList.appendChild(row);
  });
}

function syncAdminPagination() {
  const pageState = getActiveAdminPageState();
  if (!adminPagination || !adminPagePrev || !adminPageNext || !adminPageLabel || !pageState) {
    return;
  }

  const pageNumber = Math.floor(pageState.offset / pageState.limit) + 1;
  const totalPages = Math.max(1, Math.ceil((pageState.total || 0) / pageState.limit));
  adminPageLabel.textContent = `Page ${pageNumber} of ${totalPages}`;
  adminPagePrev.disabled = pageState.offset <= 0;
  adminPageNext.disabled = !pageState.hasMore;
}

// ADMIN_TAB_LOADERS maps each admin tab to how its data is fetched and what
// status text to show. This replaces the previous per-tab if/else ladder in
// loadInitialMemes; the header copy comes from ADMIN_TABS.
const ADMIN_TAB_LOADERS = {
  dashboard: { loading: "Loading dashboard...", run: () => fetchAdminDashboard(), fail: "Could not load dashboard." },
  users: { loading: "Loading users...", run: () => fetchManagedUsers(), fail: "Could not load users." },
  shares: {
    loading: "Loading shared memes...", run: () => fetchAdminShares(),
    ok: (result) => (result?.length ? "" : "No memes are currently shared."),
    fail: "Could not load shared memes.",
  },
  "tag-hygiene": { loading: "Loading tag hygiene tools...", run: () => fetchAdminTagHygiene(), fail: "Could not load tag hygiene tools." },
  "audit-logs": { loading: "Loading audit logs...", paginated: true, run: () => fetchAuditLogs(), fail: "Could not load audit logs." },
  "delete-queue": { loading: "Loading delete queue...", paginated: true, run: () => fetchDeleteQueue(), fail: "Could not load delete queue." },
  "tag-queue": { run: () => fetchAdminTagQueueStatus() },
  "tag-review": { run: () => fetchAdminTagQueueStatus() },
  "link-retries": { run: () => fetchAdminLinkRetryStatus() },
  "rejected-links": { run: () => fetchAdminLinkRetryStatus() },
  storage: { loading: "Loading storage...", run: () => window.loadAdminStorage(), fail: "Could not load storage." },
  "user-activity": { loading: "Loading user activity...", run: () => window.loadAdminUserActivity(), fail: "Could not load user activity." },
  "audiobook-library": { loading: "Loading audiobook library...", run: () => window.loadAdminAudiobookLibrary(), fail: "Could not load the audiobook library." },
  "audiobook-series": { loading: "Loading series and tags...", run: () => window.loadAdminAudiobookSeries(), fail: "Could not load series and tags." },
  backup: { run: () => { renderAdminBackupStatus(); return fetchAdminBackupStatus(); } },
  "audiobook-conversion": { run: () => fetchAdminRemuxStatus() },
};
