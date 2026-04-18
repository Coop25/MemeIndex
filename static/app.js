const state = {
  memes: [],
  library: {
    counts: {
      total: 0,
      favorites: 0,
      videos: 0,
      images: 0,
      mp3s: 0,
      untagged: 0,
      files: 0,
    },
    pageIndex: 0,
    hasMore: false,
    loading: false,
  },
  admin: {
    audit: {
      offset: 0,
      limit: 100,
      total: 0,
      hasMore: false,
    },
    queue: {
      offset: 0,
      limit: 25,
      total: 0,
      hasMore: false,
    },
  },
  auth: {
    enabled: false,
    authenticated: true,
    user: null,
    version: "dev",
    permissions: {
      canView: true,
      canAdd: true,
      canManage: true,
    },
    logoutURL: "/auth/logout",
  },
  filters: {
    tag: "",
    view: "library",
  },
};

const uploadForm = document.querySelector("#upload-form");
const uploadStatus = document.querySelector("#upload-status");
const uploadModal = document.querySelector("#upload-modal");
const openUploadModalButton = document.querySelector("#open-upload-modal");
const openRandomReelButton = document.querySelector("#open-random-reel");
const drawerToggle = document.querySelector("#drawer-toggle");
const drawerBackdrop = document.querySelector("#drawer-backdrop");
const authTrigger = document.querySelector("#auth-trigger");
const authMenu = document.querySelector("#auth-menu");
const authAvatar = document.querySelector("#auth-avatar");
const authAvatarFallback = document.querySelector("#auth-avatar-fallback");
const authName = document.querySelector("#auth-name");
const authRole = document.querySelector("#auth-role");
const authVersion = document.querySelector("#auth-version");
const authVersionValue = document.querySelector("#auth-version-value");
const authManageUsers = document.querySelector("#auth-manage-users");
const authDeleteQueue = document.querySelector("#auth-delete-queue");
const authAuditLogs = document.querySelector("#auth-audit-logs");
const authLogout = document.querySelector("#auth-logout");
const uploadModalClose = document.querySelector("#upload-modal-close");
const usersModal = document.querySelector("#users-modal");
const usersModalClose = document.querySelector("#users-modal-close");
const usersAddForm = document.querySelector("#users-add-form");
const usersAddID = document.querySelector("#users-add-id");
const usersModalStatus = document.querySelector("#users-modal-status");
const usersList = document.querySelector("#users-list");
const deleteQueueModal = document.querySelector("#delete-queue-modal");
const deleteQueueClose = document.querySelector("#delete-queue-close");
const deleteQueueStatus = document.querySelector("#delete-queue-status");
const deleteQueueList = document.querySelector("#delete-queue-list");
const auditLogsModal = document.querySelector("#audit-logs-modal");
const auditLogsClose = document.querySelector("#audit-logs-close");
const auditLogsStatus = document.querySelector("#audit-logs-status");
const auditLogsList = document.querySelector("#audit-logs-list");
const uploadPreview = document.querySelector("#upload-preview");
const uploadPreviewWrap = document.querySelector(".upload-preview-wrap");
const uploadFileInput = document.querySelector("#upload-file-input");
const uploadTagChips = document.querySelector("#upload-tag-chips");
const uploadTagsInput = document.querySelector("#upload-tags-input");
const uploadTagSuggestions = document.querySelector("#upload-tag-suggestions");
const uploadTagsHidden = document.querySelector("#upload-tags-hidden");
const contentPanel = document.querySelector(".content-panel");
const adminView = document.querySelector("#admin-view");
const adminViewKicker = document.querySelector("#admin-view-kicker");
const adminViewTitle = document.querySelector("#admin-view-title");
const adminViewCopy = document.querySelector("#admin-view-copy");
const adminViewStatus = document.querySelector("#admin-view-status");
const adminViewTable = document.querySelector("#admin-view-table");
const adminPagination = document.querySelector("#admin-pagination");
const adminPagePrev = document.querySelector("#admin-page-prev");
const adminPageNext = document.querySelector("#admin-page-next");
const adminPageLabel = document.querySelector("#admin-page-label");
const memeGridTopSpacer = document.querySelector("#meme-grid-top-spacer");
const memeGridBottomSpacer = document.querySelector("#meme-grid-bottom-spacer");
const memeGrid = document.querySelector("#meme-grid");
const memeGridSentinel = document.querySelector("#meme-grid-sentinel");
const memePagePrev = document.querySelector("#meme-page-prev");
const memePageNext = document.querySelector("#meme-page-next");
const memePageLabel = document.querySelector("#meme-page-label");
const memeGridLoader = document.querySelector("#meme-grid-loader");
const memeGridStatus = document.querySelector("#meme-grid-status");
const emptyState = document.querySelector("#empty-state");
const tagSearchInput = document.querySelector("#tag-search-input");
const tagSearchSuggestions = document.querySelector("#tag-search-suggestions");
const sidebarNavItems = document.querySelectorAll(".nav-item[data-view]");
const sidebarToggle = document.querySelector("#sidebar-toggle");
const totalCount = document.querySelector("#total-count");
const favoriteCount = document.querySelector("#favorite-count");
const videoCount = document.querySelector("#video-count");
const imageCount = document.querySelector("#image-count");
const mp3Count = document.querySelector("#mp3-count");
const untaggedCount = document.querySelector("#untagged-count");
const otherCount = document.querySelector("#other-count");
const cardTemplate = document.querySelector("#meme-card-template");
const memeModal = document.querySelector("#meme-modal");
const overlayClose = document.querySelector("#overlay-close");
const modalPreview = document.querySelector("#modal-preview");
const modalPreviewWrap = document.querySelector(".modal-preview-wrap");
const modalBody = document.querySelector("#meme-modal .modal-body");
const modalTitle = document.querySelector("#modal-title");
const modalMeta = document.querySelector("#modal-meta");
const modalCloseButton = document.querySelector("#modal-close");
const modalPanelToggle = document.querySelector("#modal-panel-toggle");
const modalDrawerToggle = document.querySelector("#modal-drawer-toggle");
const modalDrawerClose = document.querySelector("#modal-drawer-close");
const modalMediaControls = document.querySelector("#modal-media-controls");
const modalProgressWrap = document.querySelector("#modal-progress-wrap");
const modalCurrentTime = document.querySelector("#modal-current-time");
const modalProgress = document.querySelector("#modal-progress");
const modalDuration = document.querySelector("#modal-duration");
const modalPlay = document.querySelector("#modal-play");
const modalPlayIcon = document.querySelector("#modal-play-icon");
const modalVolumeWrap = document.querySelector("#modal-volume-wrap");
const modalVolumeToggle = document.querySelector("#modal-volume-toggle");
const modalVolumeIcon = document.querySelector("#modal-volume-icon");
const modalVolume = document.querySelector("#modal-volume");
const modalTagChips = document.querySelector("#modal-tag-chips");
const modalTagsInput = document.querySelector("#modal-tags-input");
const modalTagSuggestions = document.querySelector("#modal-tag-suggestions");
const modalNotesInput = document.querySelector("#modal-notes-input");
const modalOpenLink = document.querySelector("#modal-open-link");
const modalSave = document.querySelector("#modal-save");
const modalDelete = document.querySelector("#modal-delete");
const modalFavorite = document.querySelector("#modal-favorite");
const modalAuditSection = document.querySelector("#modal-audit-section");
const modalAuditList = document.querySelector("#modal-audit-list");
const randomReelModal = document.querySelector("#random-reel-modal");
const randomReelStage = document.querySelector("#random-reel-stage");
const randomReelMedia = document.querySelector("#random-reel-media");
const randomReelLoader = document.querySelector("#random-reel-loader");
const randomReelEdgeBanner = document.querySelector("#random-reel-edge-banner");
const randomReelTitle = document.querySelector("#random-reel-title");
const randomReelMeta = document.querySelector("#random-reel-meta");
const randomReelTags = document.querySelector("#random-reel-tags");
const randomReelOpen = document.querySelector("#random-reel-open");
const randomReelHint = document.querySelector("#random-reel-hint");
const randomReelFavorite = document.querySelector("#random-reel-favorite");
const randomReelPlay = document.querySelector("#random-reel-play");
const randomReelPlayIcon = document.querySelector("#random-reel-play-icon");
const randomReelVolumeWrap = document.querySelector("#random-reel-volume-wrap");
const randomReelVolumeToggle = document.querySelector("#random-reel-volume-toggle");
const randomReelVolumeIcon = document.querySelector("#random-reel-volume-icon");
const randomReelVolume = document.querySelector("#random-reel-volume");
const randomReelPrev = document.querySelector("#random-reel-prev");
const randomReelNext = document.querySelector("#random-reel-next");
const randomReelClose = document.querySelector("#random-reel-close");

let activeMemeId = null;
let modalSnapshot = null;
let modalTagState = [];
let modalSuggestionState = [];
let activeSuggestionIndex = -1;
let tagSuggestionAbortController = null;
let modalTagSequence = 0;
let uploadTagState = [];
let uploadSuggestionState = [];
let activeUploadSuggestionIndex = -1;
let uploadTagSuggestionAbortController = null;
let uploadPreviewURL = null;
let uploadDragDepth = 0;
let topTagSuggestionState = [];
let activeTopTagSuggestionIndex = -1;
let topTagSuggestionAbortController = null;
let topTagSearchDebounce = null;
let randomReelSessionID = null;
let randomReelActiveMemeID = null;
let randomReelCanGoPrev = false;
let randomReelWheelLock = false;
let randomReelWheelTimeout = null;
let randomReelUITimeout = null;
let memeModalUITimeout = null;
let randomReelTouchStartY = null;
let randomReelTouchDeltaY = 0;
let randomReelTouchActive = false;
let randomReelTouchBlocked = false;
let randomReelStepLock = false;
let memeGridObserver = null;
let memeGridRenderFrame = null;
let memePageFetchSequence = 0;
let memePendingPageIndex = 0;
let managedUsersState = [];
let deleteQueueState = [];
let auditLogState = [];

function getActiveAdminPageState() {
  if (isAdminAuditView()) return state.admin.audit;
  if (isAdminDeleteQueueView()) return state.admin.queue;
  return null;
}
const drawerMediaQuery = window.matchMedia("(max-width: 1100px)");
const modalDetailsDrawerMediaQuery = window.matchMedia("(max-width: 1100px)");
const MEME_PAGE_SIZE = 100;
const MEDIA_VOLUME_STORAGE_KEY = "memeindex.mediaVolume";
const DEFAULT_MEDIA_VOLUME = 0.10;
const MODAL_PROGRESS_SCALE_MAX = 1000;

function getAuthInitials() {
  const label = state.auth.user?.display_name || state.auth.user?.username || "MemeIndex";
  const pieces = label.trim().split(/\s+/).filter(Boolean).slice(0, 2);
  if (pieces.length === 0) return "MI";
  return pieces.map((part) => part[0]).join("").toUpperCase();
}

function closeAuthMenu() {
  authMenu.classList.add("hidden");
  authTrigger.setAttribute("aria-expanded", "false");
}

function toggleAuthMenu() {
  const nextHiddenState = !authMenu.classList.contains("hidden");
  authMenu.classList.toggle("hidden", nextHiddenState);
  authTrigger.setAttribute("aria-expanded", String(!nextHiddenState));
}

function canView() {
  return !!state.auth.permissions?.canView;
}

function canUpload() {
  return !!state.auth.permissions?.canUpload || !!state.auth.permissions?.canAdd;
}

function canAdd() {
  return canUpload();
}

function canAddTags() {
  return !!state.auth.permissions?.canAddTags || !!state.auth.permissions?.canManage;
}

function canRemoveTags() {
  return !!state.auth.permissions?.canRemoveTags || !!state.auth.permissions?.canManage;
}

function canDeleteMemes() {
  return !!state.auth.permissions?.canDeleteMemes || !!state.auth.permissions?.canManage;
}

function canManageUsers() {
  return !!state.auth.permissions?.canManageUsers;
}

function canEditMetadata() {
  return canAddTags() || canRemoveTags() || canManageUsers();
}

function canManage() {
  return canEditMetadata() || canDeleteMemes() || canManageUsers();
}

function isAdminAuditView() {
  return state.filters.view === "admin-audit-logs";
}

function isAdminDeleteQueueView() {
  return state.filters.view === "admin-delete-queue";
}

function isAdminView() {
  return isAdminAuditView() || isAdminDeleteQueueView();
}

function permissionLabel() {
  if (canManageUsers()) return "Super Admin";
  const permissions = [];
  if (canView()) permissions.push("View");
  if (canUpload()) permissions.push("Upload");
  if (canAddTags()) permissions.push("Add tags");
  if (canRemoveTags()) permissions.push("Remove tags");
  if (canDeleteMemes()) permissions.push("Delete memes");
  if (permissions.length > 0) return permissions.join(" • ");
  return "No access";
}

function renderAuthState() {
  const displayName = state.auth.user?.display_name || state.auth.user?.username || "Local access";
  authName.textContent = displayName;
  authRole.textContent = permissionLabel();
  authVersionValue.textContent = state.auth.version || "dev";
  authAvatarFallback.textContent = getAuthInitials();
  authTrigger.title = `${displayName} (${permissionLabel()}) - ${state.auth.version || "dev"}`;
  authLogout.href = state.auth.logoutURL || "/auth/logout";
  authLogout.classList.toggle("hidden", !state.auth.enabled || !state.auth.authenticated);
  authManageUsers?.classList.toggle("hidden", !canManageUsers());
  authDeleteQueue?.classList.toggle("hidden", !canManageUsers());
  authAuditLogs?.classList.toggle("hidden", !canManageUsers());
  openUploadModalButton.disabled = !canUpload();
  openUploadModalButton.setAttribute("aria-disabled", String(!canUpload()));
  openUploadModalButton.title = canUpload() ? "Add File" : "You do not have permission to upload";

  if (state.auth.user?.avatar_url) {
    authAvatar.src = state.auth.user.avatar_url;
    authAvatar.classList.remove("hidden");
    authAvatarFallback.classList.add("hidden");
  } else {
    authAvatar.removeAttribute("src");
    authAvatar.classList.add("hidden");
    authAvatarFallback.classList.remove("hidden");
  }
}

async function fetchAuthSession() {
  const response = await fetch("/api/auth/session");
  if (response.status === 401) {
    window.location.href = "/auth/login";
    return;
  }
  if (!response.ok) {
    throw new Error("Failed to load auth session");
  }

  const payload = await response.json();
  state.auth = {
    enabled: !!payload.enabled,
    authenticated: payload.authenticated !== false,
    user: payload.user || null,
    version: payload.version || "dev",
    permissions: payload.permissions || {
      canView: false,
      canUpload: false,
      canAddTags: false,
      canRemoveTags: false,
      canDeleteMemes: false,
      canManageUsers: false,
      canAdd: false,
      canManage: false,
    },
    logoutURL: payload.logout_url || "/auth/logout",
  };

  renderAuthState();
}

async function expectAuthorized(response, failureMessage) {
  if (response.status === 401) {
    window.location.href = "/auth/login";
    return false;
  }

  if (response.status === 403) {
    window.alert("You do not have permission to do that.");
    return false;
  }

  if (!response.ok) {
    window.alert(failureMessage);
    return false;
  }

  return true;
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
  return labels.length > 0 ? labels.join(" * ") : "No permissions yet";
}

function formatLastActive(unixSeconds) {
  const timestamp = Number(unixSeconds || 0);
  if (!Number.isFinite(timestamp) || timestamp <= 0) {
    return "No activity recorded yet";
  }

  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(timestamp * 1000));
  } catch (error) {
    return new Date(timestamp * 1000).toLocaleString();
  }
}

function formatDateTime(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown time";
  }

  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(date);
  } catch (error) {
    return date.toLocaleString();
  }
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll("\"", "&quot;")
    .replaceAll("'", "&#39;");
}

function renderManagedUsers() {
  if (!usersList) return;

  usersList.innerHTML = "";
  if (managedUsersState.length === 0) {
    usersList.innerHTML = `<p class="users-empty">No managed users yet.</p>`;
    return;
  }

  const sortedUsers = [...managedUsersState].sort((left, right) => {
    const leftLastActive = Number(left?.last_active_at || 0);
    const rightLastActive = Number(right?.last_active_at || 0);
    return rightLastActive - leftLastActive;
  });

  sortedUsers.forEach((user) => {
    const card = document.createElement("article");
    card.className = "users-card";
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
          ${user.is_super_admin ? '<span class="users-super-admin">Env Super Admin</span>' : ""}
        </div>
      </div>
      <div class="users-permissions">
        <label><input type="checkbox" data-scope="canView" ${checked(user.permissions?.canView)} ${disabledAttr} /> <span>View</span></label>
        <label><input type="checkbox" data-scope="canUpload" ${checked(user.permissions?.canUpload)} ${disabledAttr} /> <span>Upload</span></label>
        <label><input type="checkbox" data-scope="canAddTags" ${checked(user.permissions?.canAddTags)} ${disabledAttr} /> <span>Add tags</span></label>
        <label><input type="checkbox" data-scope="canRemoveTags" ${checked(user.permissions?.canRemoveTags)} ${disabledAttr} /> <span>Remove tags</span></label>
        <label><input type="checkbox" data-scope="canDeleteMemes" ${checked(user.permissions?.canDeleteMemes)} ${disabledAttr} /> <span>Delete memes</span></label>
      </div>
      ${user.is_super_admin ? "" : '<div class="users-card-actions"><button class="primary-button users-save-button" type="button">Save</button></div>'}
    `;

    const saveButton = card.querySelector(".users-save-button");
    if (saveButton) {
      saveButton.addEventListener("click", async () => {
        const permissions = {
          canView: !!card.querySelector('input[data-scope="canView"]')?.checked,
          canUpload: !!card.querySelector('input[data-scope="canUpload"]')?.checked,
          canAddTags: !!card.querySelector('input[data-scope="canAddTags"]')?.checked,
          canRemoveTags: !!card.querySelector('input[data-scope="canRemoveTags"]')?.checked,
          canDeleteMemes: !!card.querySelector('input[data-scope="canDeleteMemes"]')?.checked,
        };
        if (permissions.canUpload || permissions.canAddTags || permissions.canRemoveTags || permissions.canDeleteMemes) {
          permissions.canView = true;
          const viewToggle = card.querySelector('input[data-scope="canView"]');
          if (viewToggle) {
            viewToggle.checked = true;
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

        usersModalStatus.textContent = `Saved permissions for ${user.display_name || user.username || user.user_id}.`;
        await fetchManagedUsers();
      });
    }

    usersList.appendChild(card);
  });
}

async function openUsersModal() {
  if (!canManageUsers() || !usersModal) return;
  usersModalStatus.textContent = "Loading users...";
  if (!usersModal.open) {
    usersModal.showModal();
  }
  const users = await fetchManagedUsers();
  usersModalStatus.textContent = users ? "" : "Could not load users.";
}

function renderDeleteQueue() {
  const target = isAdminDeleteQueueView() && adminViewTable ? adminViewTable : deleteQueueList;
  if (!target) return;

  target.innerHTML = "";
  if (deleteQueueState.length === 0) {
    target.innerHTML = `<p class="users-empty">No pending delete requests.</p>`;
    return;
  }

  const table = document.createElement("div");
  table.className = "admin-table";
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
        <span>${escapeHTML(formatDateTime(entry.requested_at))}</span>
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
      if (deleteQueueState.length === 1 && state.admin.queue.offset > 0) {
        state.admin.queue.offset = Math.max(0, state.admin.queue.offset - state.admin.queue.limit);
      }
      await fetchDeleteQueue();
      await loadInitialMemes();
    });

    card.querySelector(".queue-approve-button")?.addEventListener("click", async () => {
      const response = await fetch(`/api/admin/memes/${encodeURIComponent(entry.meme.id)}/approve-delete`, {
        method: "POST",
      });
      if (!(await expectAuthorized(response, "Failed to approve delete."))) {
        return;
      }
      deleteQueueStatus.textContent = `Deleted ${entry.meme.originalName}.`;
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
  const target = isAdminAuditView() && adminViewTable ? adminViewTable : auditLogsList;
  if (!target) return;

  target.innerHTML = "";
  if (auditLogState.length === 0) {
    target.innerHTML = `<p class="users-empty">No audit activity recorded yet.</p>`;
    return;
  }

  const table = document.createElement("div");
  table.className = "admin-table";
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
    const memeTitle = event.meme_original_name || "Unknown or deleted meme";
    const canOpenEditor = !!event.meme_id && !!event.meme_file_path;
    const actionsMarkup = canOpenEditor
      ? `
        <div class="admin-row-actions">
          <button class="ghost-button audit-log-open-modal-button" type="button">Open In Editor</button>
          <a class="ghost-button audit-log-open-button" href="${escapeHTML(event.meme_file_path)}" target="_blank" rel="noreferrer">Open File</a>
        </div>
      `
      : `<span class="users-empty">Meme no longer available</span>`;

    row.innerHTML = `
      <div class="admin-table-cell" data-label="Time">
        <span>${escapeHTML(formatDateTime(event.created_at))}</span>
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
          <span>${escapeHTML(event.meme_content_type || "Unknown type")}</span>
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

function forceFreshHTMLReload() {
  const url = new URL(window.location.href);
  url.searchParams.set("refresh", `${Date.now()}`);
  closeAuthMenu();
  window.location.replace(url.toString());
}

function setSidebarCollapsed(collapsed) {
  if (drawerMediaQuery.matches) {
    document.body.classList.remove("sidebar-collapsed");
    if (sidebarToggle) {
      sidebarToggle.setAttribute("aria-expanded", String(document.body.classList.contains("drawer-open")));
      sidebarToggle.setAttribute("aria-label", document.body.classList.contains("drawer-open") ? "Close menu" : "Open menu");
    }
    return;
  }

  document.body.classList.toggle("sidebar-collapsed", collapsed);
  if (sidebarToggle) {
    sidebarToggle.setAttribute("aria-expanded", String(!collapsed));
    sidebarToggle.setAttribute("aria-label", collapsed ? "Expand sidebar" : "Collapse sidebar");
  }

  try {
    window.localStorage.setItem("memeindex.sidebarCollapsed", collapsed ? "true" : "false");
  } catch (error) {
    console.warn("Could not persist sidebar state", error);
  }
}

function openSidebarDrawer() {
  if (!drawerMediaQuery.matches) return;
  document.body.classList.add("drawer-open");
  drawerBackdrop.classList.remove("hidden");
  drawerToggle.classList.remove("hidden");
  drawerToggle.setAttribute("aria-expanded", "true");
  drawerToggle.setAttribute("aria-label", "Close menu");
  if (sidebarToggle) {
    sidebarToggle.setAttribute("aria-expanded", "true");
    sidebarToggle.setAttribute("aria-label", "Close menu");
  }
}

function closeSidebarDrawer() {
  document.body.classList.remove("drawer-open");
  if (drawerMediaQuery.matches) {
    drawerBackdrop.classList.add("hidden");
    drawerToggle.classList.remove("hidden");
    drawerToggle.setAttribute("aria-expanded", "false");
    drawerToggle.setAttribute("aria-label", "Open menu");
    if (sidebarToggle) {
      sidebarToggle.setAttribute("aria-expanded", "false");
      sidebarToggle.setAttribute("aria-label", "Open menu");
    }
  } else {
    drawerBackdrop.classList.add("hidden");
    drawerToggle.classList.add("hidden");
  }
}

function syncResponsiveSidebar() {
  if (drawerMediaQuery.matches) {
    document.body.classList.add("drawer-mode");
    document.body.classList.remove("sidebar-collapsed");
    drawerToggle.classList.remove("hidden");
    drawerToggle.setAttribute("aria-expanded", String(document.body.classList.contains("drawer-open")));
    drawerToggle.setAttribute("aria-label", document.body.classList.contains("drawer-open") ? "Close menu" : "Open menu");
    if (!document.body.classList.contains("drawer-open")) {
      drawerBackdrop.classList.add("hidden");
    }
    if (sidebarToggle) {
      sidebarToggle.setAttribute("aria-expanded", String(document.body.classList.contains("drawer-open")));
      sidebarToggle.setAttribute("aria-label", document.body.classList.contains("drawer-open") ? "Close menu" : "Open menu");
    }
    return;
  }

  document.body.classList.remove("drawer-mode", "drawer-open");
  drawerBackdrop.classList.add("hidden");
  drawerToggle.classList.add("hidden");

  try {
    setSidebarCollapsed(window.localStorage.getItem("memeindex.sidebarCollapsed") === "true");
  } catch (error) {
    setSidebarCollapsed(false);
  }
}

function setMemeGridStatus(message = "", hidden = !message) {
  memeGridStatus.textContent = message;
  memeGridStatus.classList.toggle("hidden", hidden);
}

function setMemeGridLoading(loading) {
  if (!memeGridLoader) {
    return;
  }

  memeGridLoader.classList.toggle("hidden", !loading);
  memeGridLoader.setAttribute("aria-hidden", String(!loading));
}

function syncMemePagination() {
  const pageNumber = (state.library.loading ? memePendingPageIndex : state.library.pageIndex) + 1;
  memePageLabel.textContent = state.library.loading
    ? `Loading page ${pageNumber}...`
    : `Page ${pageNumber}`;
  memePagePrev.disabled = state.library.loading || state.library.pageIndex === 0;
  memePageNext.disabled = state.library.loading || !state.library.hasMore;
}

function syncMemeGridObserver() {
  if (!memeGridSentinel) {
    return;
  }

  memeGridSentinel.classList.add("hidden");
  syncMemePagination();
  setMemeGridLoading(state.library.loading);

  if (state.library.loading) {
    setMemeGridStatus("Loading memes...", false);
    return;
  }

  setMemeGridStatus("", true);
}

function renderContentMode() {
  const adminMode = isAdminView();
  adminView?.classList.toggle("hidden", !adminMode);
  adminPagination?.classList.toggle("hidden", !adminMode);
  memeGridLoader?.classList.toggle("hidden", adminMode || !state.library.loading);
  memeGridStatus?.classList.toggle("hidden", adminMode || memeGridStatus.textContent === "");
  memeGridTopSpacer?.classList.toggle("hidden", adminMode);
  memeGridBottomSpacer?.classList.toggle("hidden", adminMode);
  memeGrid?.classList.toggle("hidden", adminMode);
  memeGridSentinel?.classList.toggle("hidden", adminMode);
  emptyState?.classList.toggle("hidden", adminMode || state.memes.length !== 0);
  document.querySelector(".library-toolbar")?.classList.toggle("hidden", adminMode);
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

async function fetchMemes({ page = 0 } = {}) {
  if (state.library.loading) {
    return;
  }

  const requestedPage = Math.max(0, page);
  const fetchSequence = ++memePageFetchSequence;
  state.library.loading = true;
  memePendingPageIndex = requestedPage;
  syncMemeGridObserver();

  try {
    const params = new URLSearchParams();
    if (state.filters.tag) params.set("tag", state.filters.tag);
    if (state.filters.view && state.filters.view !== "library") params.set("view", state.filters.view);
    params.set("offset", `${requestedPage * MEME_PAGE_SIZE}`);
    params.set("limit", `${MEME_PAGE_SIZE}`);

    const response = await fetch(`/api/memes?${params.toString()}`);
    if (!(await expectAuthorized(response, "Failed to load memes."))) {
      throw new Error("Failed to load memes");
    }

    const payload = await response.json();
    if (fetchSequence !== memePageFetchSequence) {
      return;
    }

    state.library.pageIndex = requestedPage;
    state.library.counts = payload.counts || state.library.counts;
    state.library.hasMore = !!payload.has_more;
    state.memes = payload.memes || [];
    renderMemes();
  } finally {
    if (fetchSequence !== memePageFetchSequence) {
      return;
    }

    state.library.loading = false;
    memePendingPageIndex = state.library.pageIndex;
    syncMemeGridObserver();
  }
}

async function loadInitialMemes() {
  renderSidebarViewState();
  renderContentMode();
  if (isAdminAuditView()) {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Audit Logs";
    adminViewCopy.textContent = "A full activity log with actor, action, target meme, and quick-open access for review.";
    adminViewStatus.textContent = "Loading audit logs...";
    adminViewTable.innerHTML = "";
    syncAdminPagination();
    const events = await fetchAuditLogs();
    adminViewStatus.textContent = events ? "" : "Could not load audit logs.";
    renderContentMode();
    return;
  }
  if (isAdminDeleteQueueView()) {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Delete Review Queue";
    adminViewCopy.textContent = "Review pending meme deletions, inspect the media, and either keep the meme or approve the delete.";
    adminViewStatus.textContent = "Loading delete queue...";
    adminViewTable.innerHTML = "";
    syncAdminPagination();
    const entries = await fetchDeleteQueue();
    adminViewStatus.textContent = entries ? "" : "Could not load delete queue.";
    renderContentMode();
    return;
  }
  await fetchMemes({ page: 0 });
  renderContentMode();
}

async function applyTagSearch(rawValue) {
  state.filters.tag = normalizeTagValue(rawValue);
  await loadInitialMemes();
}

function queueTagSearch(rawValue) {
  if (topTagSearchDebounce) {
    window.clearTimeout(topTagSearchDebounce);
  }

  const normalizedValue = normalizeTagValue(rawValue);
  topTagSearchDebounce = window.setTimeout(() => {
    applyTagSearch(normalizedValue).catch((error) => {
      console.error(error);
    });
  }, 160);
}

function isImageMeme(meme) {
  return meme.contentType.startsWith("image/");
}

function isVideoMeme(meme) {
  return meme.contentType.startsWith("video/");
}

function isMP3Meme(meme) {
  return meme.contentType === "audio/mpeg" || meme.originalName.toLowerCase().endsWith(".mp3");
}

function isFileMeme(meme) {
  return !isImageMeme(meme) && !isVideoMeme(meme) && !isMP3Meme(meme);
}

function isUntaggedMeme(meme) {
  return !Array.isArray(meme.tags) || meme.tags.length === 0;
}

function getSourceMemes() {
  return state.memes;
}

function getVisibleMemes() {
  return getSourceMemes();
}

function renderSidebarCounts(counts = state.library.counts) {
  totalCount.textContent = `${counts.total || 0}`;
  favoriteCount.textContent = `${counts.favorites || 0}`;
  videoCount.textContent = `${counts.videos || 0}`;
  imageCount.textContent = `${counts.images || 0}`;
  mp3Count.textContent = `${counts.mp3s || 0}`;
  untaggedCount.textContent = `${counts.untagged || 0}`;
  otherCount.textContent = `${counts.files || 0}`;
}

function renderSidebarViewState() {
  sidebarNavItems.forEach((item) => {
    item.classList.toggle("is-active", item.dataset.view === state.filters.view);
  });
}

function renderTopTagSuggestions() {
  tagSearchSuggestions.innerHTML = "";

  if (topTagSuggestionState.length === 0) {
    tagSearchSuggestions.classList.add("hidden");
    activeTopTagSuggestionIndex = -1;
    return;
  }

  topTagSuggestionState.forEach((tag, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "tag-suggestion";
    if (index === activeTopTagSuggestionIndex) {
      button.classList.add("is-active");
    }
    button.innerHTML = `<span>${tag}</span><span class="tag-suggestion-hint">tag</span>`;
    button.addEventListener("click", async () => {
      tagSearchInput.value = tag;
      topTagSuggestionState = [];
      renderTopTagSuggestions();
      await applyTagSearch(tag);
      tagSearchInput.focus();
    });
    tagSearchSuggestions.appendChild(button);
  });

  tagSearchSuggestions.classList.remove("hidden");
}

async function fetchTopTagSuggestions() {
  const needle = normalizeTagValue(tagSearchInput.value);
  if (!needle) {
    if (topTagSuggestionAbortController) {
      topTagSuggestionAbortController.abort();
      topTagSuggestionAbortController = null;
    }
    topTagSuggestionState = [];
    renderTopTagSuggestions();
    return;
  }

  if (topTagSuggestionAbortController) {
    topTagSuggestionAbortController.abort();
  }

  topTagSuggestionAbortController = new AbortController();

  try {
    const response = await fetch(`/api/tags?q=${encodeURIComponent(needle)}`, {
      signal: topTagSuggestionAbortController.signal,
    });
    if (!response.ok) {
      return;
    }

    const payload = await response.json();
    if (normalizeTagValue(tagSearchInput.value) !== needle) {
      return;
    }
    topTagSuggestionState = (payload.tags || []).slice(0, 10);
    renderTopTagSuggestions();
  } catch (error) {
    if (error.name !== "AbortError") {
      console.error(error);
    }
  }
}

function renderMemes() {
  const visibleMemes = getVisibleMemes();
  renderSidebarCounts();
  renderSidebarViewState();
  emptyState.classList.toggle("hidden", visibleMemes.length !== 0);
  renderContentMode();
  contentPanel.scrollTop = 0;
  queueRenderLoadedMemes({ force: true });
}

function buildMemeCardElement(meme) {
  const fragment = cardTemplate.content.cloneNode(true);
  const card = fragment.querySelector(".meme-card");
  const cardHitarea = fragment.querySelector(".card-hitarea");
  const previewFrame = fragment.querySelector(".preview-frame");
  const favoriteButton = fragment.querySelector(".favorite-button");
  const tagList = fragment.querySelector(".tag-list");

  card.dataset.memeId = meme.id;
  applyFavoriteStateToButton(favoriteButton, meme.favorite);
  favoriteButton.disabled = !canView();
  favoriteButton.title = canView() ? "" : "You do not have permission to favorite memes";

  previewFrame.appendChild(buildPreview(meme));

  cardHitarea.addEventListener("click", () => {
    openModal(meme.id);
  });

  cardHitarea.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      openModal(meme.id);
    }
  });

  if (canView()) {
    favoriteButton.addEventListener("click", async (event) => {
      event.stopPropagation();
      const currentMeme = getMemeById(meme.id);
      await persistFavorite(meme.id, !currentMeme?.favorite);
    });
  }

  card._previewFrame = previewFrame;
  card._favoriteButton = favoriteButton;
  card._tagList = tagList;
  updateMemeCardElement(card, meme);
  return card;
}

function updateMemeCardElement(card, meme) {
  if (!card || !meme) {
    return card;
  }

  card.dataset.memeId = meme.id;
  applyFavoriteStateToButton(card._favoriteButton, meme.favorite);
  card._favoriteButton.disabled = !canView();
  card._favoriteButton.title = canView() ? "" : "You do not have permission to favorite memes";

  const existingPreview = card._previewFrame?.firstElementChild;
  const nextPreviewSrc = meme.previewPath || meme.filePath;
  const previewMatches = existingPreview &&
    existingPreview.dataset?.previewSrc === nextPreviewSrc &&
    existingPreview.dataset?.previewKind === previewKindForMeme(meme);
  if (!previewMatches && card._previewFrame) {
    card._previewFrame.replaceChildren(buildPreview(meme));
    const preview = card._previewFrame.firstElementChild;
    if (preview) {
      preview.dataset.previewSrc = nextPreviewSrc;
      preview.dataset.previewKind = previewKindForMeme(meme);
    }
  }

  card._tags = meme.tags || [];
  return card;
}

function previewKindForMeme(meme) {
  if (meme.contentType.startsWith("image/")) {
    return "image";
  }
  if (meme.contentType.startsWith("video/")) {
    return meme.previewPath ? "thumbnail" : "video";
  }
  return "file";
}

function renderLoadedMemes() {
  const memes = getVisibleMemes();
  if (!memes.length) {
    memeGrid.replaceChildren();
    memeGridTopSpacer.classList.add("hidden");
    memeGridBottomSpacer.classList.add("hidden");
    return;
  }

  memeGridTopSpacer.classList.add("hidden");
  memeGridBottomSpacer.classList.add("hidden");
  const fragment = document.createDocumentFragment();

  memes.forEach((meme) => {
    fragment.appendChild(buildMemeCardElement(meme));
  });

  memeGrid.replaceChildren(fragment);
  Array.from(memeGrid.querySelectorAll(".meme-card")).forEach((card) => {
    layoutCardTags(card._tagList, card._tags);
  });
}

function queueRenderLoadedMemes(options = {}) {
  if (options.force) {
    if (memeGridRenderFrame) {
      window.cancelAnimationFrame(memeGridRenderFrame);
      memeGridRenderFrame = null;
    }
    renderLoadedMemes();
    return;
  }

  if (memeGridRenderFrame) {
    return;
  }

  memeGridRenderFrame = window.requestAnimationFrame(() => {
    memeGridRenderFrame = null;
    renderLoadedMemes();
  });
}

function createCardTagChip(label, className = "tag-chip") {
  const chip = document.createElement("span");
  chip.className = className;
  chip.textContent = label;
  return chip;
}

function layoutCardTags(tagList, tags, maxRows = 2) {
  tagList.innerHTML = "";

  if (!tags.length) {
    return;
  }

  const chips = tags.map((tag) => createCardTagChip(tag));

  chips.forEach((chip) => {
    tagList.appendChild(chip);
  });

  if (maxRows === 1) {
    if (tagList.scrollWidth <= tagList.clientWidth) {
      return;
    }

    let visibleCount = chips.length;
    const moreChip = createCardTagChip("", "tag-chip tag-chip-more");

    while (visibleCount > 0) {
      chips[visibleCount - 1].remove();
      visibleCount -= 1;

      const hiddenCount = chips.length - visibleCount;
      moreChip.textContent = `+${hiddenCount}`;
      moreChip.setAttribute("aria-label", `${hiddenCount} more tags`);
      tagList.appendChild(moreChip);

      if (tagList.scrollWidth <= tagList.clientWidth) {
        return;
      }

      moreChip.remove();
    }

    moreChip.textContent = `+${chips.length}`;
    moreChip.setAttribute("aria-label", `${chips.length} more tags`);
    tagList.appendChild(moreChip);
    return;
  }

  const rowTolerance = 4;
  const firstChip = chips[0];
  if (!firstChip) {
    return;
  }

  const baseTop = firstChip.offsetTop;
  const rowHeight = firstChip.offsetHeight || 18;
  const maxTop = baseTop + ((maxRows - 1) * rowHeight) + rowTolerance;

  let visibleCount = chips.length;
  for (let index = 0; index < chips.length; index += 1) {
    if (chips[index].offsetTop > maxTop) {
      visibleCount = index;
      break;
    }
  }

  if (visibleCount === chips.length) {
    return;
  }

  const hiddenCount = chips.length - visibleCount;
  chips.slice(visibleCount).forEach((chip) => chip.remove());

  const moreChip = createCardTagChip(`+${hiddenCount}`, "tag-chip tag-chip-more");
  moreChip.setAttribute("aria-label", `${hiddenCount} more tags`);
  tagList.appendChild(moreChip);

  while (tagList.lastElementChild && moreChip.offsetTop > maxTop && visibleCount > 0) {
    moreChip.remove();
    visibleCount -= 1;
    tagList.lastElementChild?.remove();
    const nextHiddenCount = chips.length - visibleCount;
    moreChip.textContent = `+${nextHiddenCount}`;
    moreChip.setAttribute("aria-label", `${nextHiddenCount} more tags`);
    tagList.appendChild(moreChip);
  }
}

function truncateWithCounter(value, maxLength = 48) {
  const text = `${value || ""}`;
  if (text.length <= maxLength) {
    return text;
  }

  const hiddenCount = text.length - maxLength;
  const visibleLength = Math.max(12, maxLength - (` +${hiddenCount}`.length + 1));
  return `${text.slice(0, visibleLength)}… +${hiddenCount}`;
}

function buildPreview(meme) {
  if (meme.contentType.startsWith("image/")) {
    const img = document.createElement("img");
    img.src = meme.previewPath || meme.filePath;
    img.alt = meme.originalName;
    return img;
  }

  if (meme.contentType.startsWith("video/")) {
    if (meme.previewPath) {
      const img = document.createElement("img");
      img.src = meme.previewPath;
      img.alt = meme.originalName;
      return img;
    }

    const video = document.createElement("video");
    video.src = meme.filePath;
    video.preload = "metadata";
    video.muted = true;
    return video;
  }

  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${meme.contentType}</span>`;
  return icon;
}

function pickIcon(contentType) {
  if (contentType.startsWith("audio/")) return "AUDIO";
  if (contentType.includes("zip") || contentType.includes("compressed")) return "ZIP";
  if (contentType.includes("pdf")) return "PDF";
  return "FILE";
}

function clampMediaVolume(value) {
  if (!Number.isFinite(value)) {
    return DEFAULT_MEDIA_VOLUME;
  }
  return Math.min(1, Math.max(0, value));
}

function loadPreferredMediaVolume() {
  try {
    const rawValue = window.localStorage.getItem(MEDIA_VOLUME_STORAGE_KEY);
    if (rawValue == null || rawValue === "") {
      return DEFAULT_MEDIA_VOLUME;
    }

    return clampMediaVolume(Number(rawValue));
  } catch (error) {
    return DEFAULT_MEDIA_VOLUME;
  }
}

function persistPreferredMediaVolume(volume) {
  const normalizedVolume = clampMediaVolume(volume);
  try {
    window.localStorage.setItem(MEDIA_VOLUME_STORAGE_KEY, `${normalizedVolume}`);
  } catch (error) {
    console.warn("Could not persist preferred media volume", error);
  }
}

function syncStoredMediaVolumeFromElement(media) {
  if (!(media instanceof HTMLMediaElement)) {
    return;
  }

  const volume = media.muted ? 0 : media.volume;
  persistPreferredMediaVolume(volume);
}

function applyDefaultMediaVolume(media) {
  const preferredVolume = loadPreferredMediaVolume();
  media.volume = preferredVolume;
  media.muted = preferredVolume === 0;
  media.defaultMuted = false;
}

function buildModalPreview(meme) {
  if (meme.contentType.startsWith("image/")) {
    const img = document.createElement("img");
    img.src = meme.filePath;
    img.alt = meme.originalName;
    return img;
  }

  if (meme.contentType.startsWith("video/")) {
    const video = document.createElement("video");
    video.src = meme.filePath;
    video.autoplay = true;
    video.loop = true;
    video.playsInline = true;
    video.controls = false;
    video.preload = "metadata";
    applyDefaultMediaVolume(video);
    video.addEventListener("loadedmetadata", () => {
      applyDefaultMediaVolume(video);
    });
    video.addEventListener("volumechange", () => {
      syncStoredMediaVolumeFromElement(video);
    });
    return video;
  }

  if (meme.contentType.startsWith("audio/")) {
    const audio = document.createElement("audio");
    audio.src = meme.filePath;
    audio.controls = false;
    audio.autoplay = true;
    audio.loop = true;
    audio.preload = "metadata";
    applyDefaultMediaVolume(audio);
    audio.addEventListener("loadedmetadata", () => {
      applyDefaultMediaVolume(audio);
    });
    audio.addEventListener("volumechange", () => {
      syncStoredMediaVolumeFromElement(audio);
    });
    return audio;
  }

  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${meme.contentType}</span><span>Use Open Original to access this file.</span>`;
  return icon;
}

function buildRandomReelPreview(meme) {
  if (meme.contentType.startsWith("image/")) {
    const img = document.createElement("img");
    img.src = meme.filePath;
    img.alt = meme.originalName;
    return img;
  }

  if (meme.contentType.startsWith("video/")) {
    const video = document.createElement("video");
    video.src = meme.filePath;
    video.autoplay = true;
    video.loop = true;
    video.playsInline = true;
    video.controls = false;
    video.preload = "metadata";
    applyDefaultMediaVolume(video);
    video.addEventListener("loadedmetadata", () => {
      applyDefaultMediaVolume(video);
    });
    video.addEventListener("volumechange", () => {
      syncStoredMediaVolumeFromElement(video);
    });
    return video;
  }

  if (meme.contentType.startsWith("audio/")) {
    const audio = document.createElement("audio");
    audio.src = meme.filePath;
    audio.controls = false;
    audio.autoplay = true;
    audio.preload = "metadata";
    applyDefaultMediaVolume(audio);
    audio.addEventListener("loadedmetadata", () => {
      applyDefaultMediaVolume(audio);
    });
    audio.addEventListener("volumechange", () => {
      syncStoredMediaVolumeFromElement(audio);
    });
    return audio;
  }

  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${meme.originalName}</span><span>Open original to view this file type.</span>`;
  return icon;
}

function loadRandomReelSessionID() {
  if (randomReelSessionID) return randomReelSessionID;
  try {
    randomReelSessionID = window.localStorage.getItem("memeindex.randomReelSession") || null;
  } catch (error) {
    randomReelSessionID = null;
  }
  return randomReelSessionID;
}

function persistRandomReelSessionID(sessionID) {
  randomReelSessionID = sessionID || null;
  try {
    if (randomReelSessionID) {
      window.localStorage.setItem("memeindex.randomReelSession", randomReelSessionID);
    } else {
      window.localStorage.removeItem("memeindex.randomReelSession");
    }
  } catch (error) {
    console.warn("Could not persist random reel session", error);
  }
}

function discardRandomReelSession() {
  const sessionID = loadRandomReelSessionID();
  persistRandomReelSessionID(null);
  if (!sessionID) {
    return;
  }

  fetch(`/api/reel-session?session_id=${encodeURIComponent(sessionID)}`, {
    method: "DELETE",
    keepalive: true,
  }).catch((error) => {
    console.warn("Could not delete random reel session", error);
  });
}

async function resetRandomReelSession() {
  const sessionID = loadRandomReelSessionID();
  persistRandomReelSessionID(null);
  if (!sessionID) {
    return;
  }

  try {
    await fetch(`/api/reel-session?session_id=${encodeURIComponent(sessionID)}`, {
      method: "DELETE",
    });
  } catch (error) {
    console.warn("Could not reset random reel session", error);
  }
}

async function fetchRandomReelStep(direction = "next") {
  const params = new URLSearchParams();
  const sessionID = loadRandomReelSessionID();
  if (sessionID) {
    params.set("session_id", sessionID);
  }
  params.set("direction", direction);

  const response = await fetch(`/api/memes/random?${params.toString()}`);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error("Failed to load random meme");
  }

  const payload = await response.json();
  persistRandomReelSessionID(payload.session_id || null);
  if (payload.session_replaced && (payload.reason === "stale" || payload.reason === "missing")) {
    persistRandomReelSessionID(payload.session_id || null);
  }
  return payload;
}

function setRandomReelLoading(loading, label = "Loading meme...") {
  randomReelStage.classList.toggle("is-loading", loading);
  if (!randomReelLoader) {
    return;
  }

  const labelNode = randomReelLoader.querySelector(".loading-label");
  if (labelNode) {
    labelNode.textContent = label;
  }
  randomReelLoader.classList.toggle("hidden", !loading);
  randomReelLoader.setAttribute("aria-hidden", String(!loading));
}

function renderRandomReelMeme(meme) {
  if (!meme) return;

  randomReelActiveMemeID = meme.id;
  randomReelMedia.innerHTML = "";
  const preview = buildRandomReelPreview(meme);
  randomReelMedia.appendChild(preview);
  if (preview instanceof HTMLMediaElement) {
    preview.addEventListener("play", syncRandomReelMediaControls);
    preview.addEventListener("pause", syncRandomReelMediaControls);
    preview.addEventListener("volumechange", syncRandomReelMediaControls);
    preview.addEventListener("loadedmetadata", syncRandomReelMediaControls);
  }
  randomReelTitle.textContent = truncateWithCounter(meme.originalName, 52);
  randomReelTitle.title = meme.originalName;
  randomReelMeta.textContent = `${formatSize(meme.sizeBytes)} * ${meme.contentType}`;
  randomReelOpen.href = meme.filePath;
  randomReelTags.innerHTML = "";
  layoutCardTags(randomReelTags, (meme.tags && meme.tags.length > 0) ? meme.tags : ["untagged"], 1);

  randomReelPrev.disabled = !randomReelCanGoPrev;
  applyFavoriteStateToButton(randomReelFavorite, !!meme.favorite);
  randomReelFavorite.disabled = !canView();
  randomReelFavorite.title = canView() ? "" : "You do not have permission to favorite memes";
  syncRandomReelMediaControls();
  randomReelHint.textContent = randomReelCanGoPrev
    ? "Scroll down for a new random meme. Scroll up to go back."
    : "You are at the start of this reel session. Scroll down for something new.";
  randomReelHint.classList.toggle("is-edge", !randomReelCanGoPrev);
  showRandomReelUI();
}

function syncRandomReelFavoriteUI(updatedMeme) {
  if (!updatedMeme?.id || updatedMeme.id !== randomReelActiveMemeID) {
    return;
  }

  applyFavoriteStateToButton(randomReelFavorite, !!updatedMeme.favorite);
}

function getRandomReelMediaControlTarget() {
  return randomReelMedia.querySelector("video, audio");
}

function syncRandomReelMediaControls() {
  const media = getRandomReelMediaControlTarget();
  const supportsMediaControls = !!media;

  randomReelPlay.disabled = !supportsMediaControls;
  randomReelVolumeToggle.disabled = !supportsMediaControls;
  randomReelVolume.disabled = !supportsMediaControls;
  randomReelPlay.classList.toggle("hidden", !supportsMediaControls);
  randomReelVolumeWrap.classList.toggle("hidden", !supportsMediaControls);

  if (!supportsMediaControls) {
    randomReelPlay.setAttribute("aria-label", "Play media");
    randomReelPlay.setAttribute("data-tooltip", "Play");
    randomReelPlayIcon.innerHTML = "&#9654;";
    randomReelVolumeToggle.setAttribute("aria-label", "Mute media");
    randomReelVolumeIcon.innerHTML = "&#128266;";
    randomReelVolume.value = `${Math.round(loadPreferredMediaVolume() * 100)}`;
    return;
  }

  const paused = media.paused;
  const muted = media.muted || media.volume === 0;

  randomReelPlay.setAttribute("aria-label", paused ? "Play media" : "Pause media");
  randomReelPlay.setAttribute("data-tooltip", paused ? "Play" : "Pause");
  randomReelPlayIcon.innerHTML = paused ? "&#9654;" : "&#10074;&#10074;";

  randomReelVolumeToggle.setAttribute("aria-label", muted ? "Unmute media" : "Mute media");
  randomReelVolumeIcon.innerHTML = muted ? "&#128263;" : "&#128266;";
  randomReelVolume.value = `${Math.round((media.muted ? 0 : media.volume) * 100)}`;
}

function clearRandomReelUIHideTimer() {
  if (randomReelUITimeout) {
    window.clearTimeout(randomReelUITimeout);
    randomReelUITimeout = null;
  }
}

function clearMemeModalUIHideTimer() {
  if (memeModalUITimeout) {
    window.clearTimeout(memeModalUITimeout);
    memeModalUITimeout = null;
  }
}

function setRandomReelScrollLock(locked) {
  document.body.classList.toggle("random-reel-open", locked);
}

function applyRandomReelDrag(deltaY) {
  if (!randomReelStage) return;

  const limitedDelta = Math.max(-140, Math.min(140, deltaY));
  const dragDistance = Math.abs(limitedDelta);
  const dragProgress = Math.min(dragDistance / 120, 1);

  randomReelStage.classList.add("is-dragging");
  randomReelStage.classList.toggle("is-dragging-next", limitedDelta < 0);
  randomReelStage.classList.toggle("is-dragging-prev", limitedDelta > 0);
  randomReelStage.style.setProperty("--reel-drag-y", `${limitedDelta}px`);
  randomReelStage.style.setProperty("--reel-drag-progress", dragProgress.toFixed(3));
}

function resetRandomReelDrag() {
  if (!randomReelStage) return;

  randomReelStage.classList.remove("is-dragging", "is-dragging-next", "is-dragging-prev");
  randomReelStage.style.removeProperty("--reel-drag-y");
  randomReelStage.style.removeProperty("--reel-drag-progress");
}

function beginRandomReelTransition(direction) {
  resetRandomReelDrag();
  randomReelStage.classList.remove("is-stepping-next", "is-stepping-prev");
  void randomReelStage.offsetWidth;
  randomReelStage.classList.add(direction > 0 ? "is-stepping-next" : "is-stepping-prev");
  setRandomReelLoading(true, "Loading meme...");
}

function endRandomReelTransition() {
  randomReelStage.classList.remove("is-stepping-next", "is-stepping-prev");
  setRandomReelLoading(false);
  resetRandomReelDrag();
}

function hideRandomReelUI() {
  clearRandomReelUIHideTimer();
  if (!randomReelModal?.open) return;
  randomReelModal.classList.add("random-reel-ui-hidden");
}

function showRandomReelUI(autohide = true) {
  clearRandomReelUIHideTimer();
  randomReelModal.classList.remove("random-reel-ui-hidden");
  if (!autohide || !randomReelModal?.open) {
    return;
  }
  randomReelUITimeout = window.setTimeout(() => {
    randomReelModal.classList.add("random-reel-ui-hidden");
  }, 4000);
}

function hideMemeModalUI() {
  clearMemeModalUIHideTimer();
  if (!memeModal?.open) return;
  memeModal.classList.add("meme-modal-ui-hidden");
}

function showMemeModalUI(autohide = true) {
  clearMemeModalUIHideTimer();
  memeModal?.classList.remove("meme-modal-ui-hidden");
  if (!autohide || !memeModal?.open) {
    return;
  }
  memeModalUITimeout = window.setTimeout(() => {
    hideMemeModalUI();
  }, 2500);
}

async function stepRandomReel(direction) {
  if (!randomReelModal.open || randomReelStepLock) return;
  randomReelStepLock = true;

  if (direction < 0 && !randomReelCanGoPrev) {
    randomReelEdgeBanner.classList.remove("is-bump");
    void randomReelEdgeBanner.offsetWidth;
    randomReelEdgeBanner.classList.add("is-bump");
    randomReelHint.classList.add("is-bump");
    window.setTimeout(() => {
      randomReelHint.classList.remove("is-bump");
      randomReelEdgeBanner.classList.remove("is-bump");
    }, 220);
    randomReelStepLock = false;
    return;
  }

  try {
    beginRandomReelTransition(direction);
    const payload = await fetchRandomReelStep(direction < 0 ? "prev" : "next");
    if (!payload?.meme) return;
    randomReelCanGoPrev = !!payload.can_go_prev;
    renderRandomReelMeme(payload.meme);
  } finally {
    endRandomReelTransition();
    randomReelStepLock = false;
  }
}

async function openRandomReel() {
  if (!randomReelModal.open) {
    randomReelModal.showModal();
  }
  setRandomReelScrollLock(true);
  showRandomReelUI(false);
  setRandomReelLoading(true, "Loading reel...");

  try {
    await resetRandomReelSession();
    const payload = await fetchRandomReelStep("next");
    if (!payload?.meme) {
      closeRandomReel();
      window.alert("No memes available yet.");
      return;
    }

    randomReelCanGoPrev = !!payload.can_go_prev;
    renderRandomReelMeme(payload.meme);
    showRandomReelUI();
  } catch (error) {
    closeRandomReel();
    throw error;
  } finally {
    setRandomReelLoading(false);
  }
}

function closeRandomReel() {
  clearRandomReelUIHideTimer();
  randomReelModal.classList.remove("random-reel-ui-hidden");
  const media = randomReelMedia.querySelector("video, audio");
  if (media) {
    media.pause();
  }
  if (randomReelModal.open) {
    randomReelModal.close();
  }
  setRandomReelScrollLock(false);
  randomReelActiveMemeID = null;
  randomReelCanGoPrev = false;
  randomReelTouchStartY = null;
  randomReelTouchDeltaY = 0;
  randomReelTouchActive = false;
  randomReelTouchBlocked = false;
  randomReelStepLock = false;
  resetRandomReelDrag();
  randomReelHint.classList.remove("is-edge", "is-bump");
  randomReelEdgeBanner.classList.remove("is-bump");
  discardRandomReelSession();
}

function clearUploadPreview() {
  if (uploadPreviewURL) {
    URL.revokeObjectURL(uploadPreviewURL);
    uploadPreviewURL = null;
  }
  uploadPreview.innerHTML = `<div class="file-icon upload-empty-preview"><strong>DROP</strong><span>Choose one or more files to preview the first item before uploading.</span></div>`;
}

function setUploadDragActive(active) {
  uploadPreviewWrap?.classList.toggle("is-drag-active", active);
}

function setUploadFiles(files) {
  if (!uploadFileInput) return;
  if (!files || files.length === 0) {
    uploadFileInput.value = "";
    renderUploadPreview(null, 0);
    return;
  }

  try {
    const dataTransfer = new DataTransfer();
    for (const file of files) {
      dataTransfer.items.add(file);
    }
    uploadFileInput.files = dataTransfer.files;
  } catch (error) {
    console.warn("Could not assign dropped files to upload input", error);
  }

  renderUploadPreview(files[0], files.length);
}

function renderUploadPreview(file, totalFiles = 1) {
  clearUploadPreview();
  if (!file) {
    return;
  }

  uploadPreview.innerHTML = "";

  uploadPreviewURL = URL.createObjectURL(file);
  const type = file.type || "";
  const fileName = file.name || "Selected file";
  const appendSelectionCount = () => {
    if (totalFiles <= 1) {
      return;
    }
    const extra = document.createElement("div");
    extra.className = "upload-preview-count";
    extra.textContent = `+${totalFiles - 1} more selected`;
    uploadPreview.appendChild(extra);
  };

  if (type.startsWith("image/")) {
    const img = document.createElement("img");
    img.src = uploadPreviewURL;
    img.alt = fileName;
    uploadPreview.appendChild(img);
    appendSelectionCount();
    return;
  }

  if (type.startsWith("video/")) {
    const video = document.createElement("video");
    video.src = uploadPreviewURL;
    video.controls = true;
    video.preload = "metadata";
    applyDefaultMediaVolume(video);
    uploadPreview.appendChild(video);
    appendSelectionCount();
    return;
  }

  if (type.startsWith("audio/") || fileName.toLowerCase().endsWith(".mp3")) {
    const audio = document.createElement("audio");
    audio.src = uploadPreviewURL;
    audio.controls = true;
    audio.preload = "metadata";
    applyDefaultMediaVolume(audio);
    uploadPreview.appendChild(audio);
    appendSelectionCount();
    return;
  }

  const icon = document.createElement("div");
  icon.className = "file-icon";
  icon.innerHTML = `<strong>${pickIcon(type || fileName)}</strong><span>${fileName}</span><span>Preview available after upload via Open Original for this file type.</span>`;
  uploadPreview.appendChild(icon);
  appendSelectionCount();
}

function getMemeById(id) {
  return state.memes.find((item) => item.id === id);
}

function getCardByMemeId(id) {
  return memeGrid.querySelector(`.meme-card[data-meme-id="${id}"]`);
}

function applyFavoriteStateToButton(button, favorite) {
  if (!button) return;
  button.classList.toggle("is-active", favorite);
  button.setAttribute("aria-label", favorite ? "Favorited" : "Favorite");
  button.setAttribute("data-tooltip", favorite ? "Favorited" : "Favorite");
}

function syncFavoriteUI(updatedMeme) {
  if (!updatedMeme?.id) return;

  if (updatedMeme.favorite) {
    state.library.counts.favorites += 1;
  } else {
    state.library.counts.favorites = Math.max(0, (state.library.counts.favorites || 0) - 1);
  }
  renderSidebarCounts();

  const card = getCardByMemeId(updatedMeme.id);
  const favoriteButton = card?.querySelector(".favorite-button");
  applyFavoriteStateToButton(favoriteButton, updatedMeme.favorite);

  if (state.filters.view === "favorites") {
    if (!updatedMeme.favorite && card) {
      state.memes = state.memes.filter((meme) => meme.id !== updatedMeme.id);
      card.remove();
    }
    emptyState.classList.toggle("hidden", state.memes.length !== 0);
  }

  queueRenderLoadedMemes({ force: true });
  syncMemeGridObserver();

  if (activeMemeId === updatedMeme.id) {
    applyFavoriteStateToButton(modalFavorite, updatedMeme.favorite);
    modalSnapshot = {
      ...(modalSnapshot || {}),
      favorite: !!updatedMeme.favorite,
    };
  }
}

function updateMemeInState(updatedMeme) {
  if (!updatedMeme?.id) return null;

  const index = state.memes.findIndex((item) => item.id === updatedMeme.id);
  if (index < 0) return null;

  state.memes[index] = {
    ...state.memes[index],
    ...updatedMeme,
  };

  return state.memes[index];
}

function setModalAuditVisibility(visible) {
  if (!modalAuditSection) return;
  modalAuditSection.classList.toggle("hidden", !visible);
  modalAuditSection.style.display = visible ? "" : "none";
  if (!visible && modalAuditList) {
    modalAuditList.innerHTML = "";
  }
}

async function openAdminMemeByID(id) {
  if (!id) return;
  const existing = getMemeById(id);
  if (existing) {
    openModalWithMeme(existing);
    return;
  }

  const response = await fetch(`/api/admin/memes/${encodeURIComponent(id)}`);
  if (!(await expectAuthorized(response, "Failed to load meme."))) {
    return;
  }
  const meme = await response.json();
  openModalWithMeme(meme);
}

function openModalWithMeme(meme) {
  if (!meme) return;

  activeMemeId = meme.id;
  setModalAuditVisibility(canManageUsers());
  modalPreview.innerHTML = "";
  const preview = buildModalPreview(meme);
  modalPreview.appendChild(preview);
  if (preview instanceof HTMLMediaElement) {
    preview.addEventListener("play", syncModalMediaControls);
    preview.addEventListener("pause", syncModalMediaControls);
    preview.addEventListener("timeupdate", syncModalMediaControls);
    preview.addEventListener("durationchange", syncModalMediaControls);
    preview.addEventListener("ended", syncModalMediaControls);
    preview.addEventListener("volumechange", syncModalMediaControls);
    preview.addEventListener("loadedmetadata", syncModalMediaControls);
  }
  modalTitle.textContent = meme.originalName;
  modalMeta.textContent = `${formatSize(meme.sizeBytes)} * ${meme.contentType}`;
  setModalTags(meme.tags || []);
  modalTagsInput.value = "";
  renderTagEditor();
  renderTagSuggestions();
  modalNotesInput.value = meme.notes || "";
  modalOpenLink.href = meme.filePath;
  applyFavoriteStateToButton(modalFavorite, meme.favorite);
  modalTagsInput.disabled = !canAddTags();
  modalNotesInput.readOnly = !canEditMetadata();
  modalFavorite.disabled = !canView();
  modalSave.disabled = !canEditMetadata();
  modalDelete.disabled = !canDeleteMemes();
  modalFavorite.title = canView() ? "" : "You do not have permission to favorite memes";
  modalSave.title = canEditMetadata() ? "" : "You do not have permission to edit metadata";
  modalDelete.title = canDeleteMemes() ? "" : "You do not have permission to delete memes";
  modalSnapshot = {
    favorite: !!meme.favorite,
    notes: meme.notes || "",
    tags: getModalTagValues().sort().join(", "),
  };
  memeModal.classList.remove("details-open");
  modalPanelToggle?.setAttribute("aria-expanded", "false");
  modalPanelToggle?.setAttribute("aria-label", "Open details");

  if (!memeModal.open) {
    memeModal.showModal();
  }
  overlayClose.classList.remove("hidden");
  showMemeModalUI();
  syncModalMediaControls();
  loadModalAudit(meme.id).catch((error) => {
    console.error(error);
    if (modalAuditList) {
      modalAuditList.innerHTML = `<p class="users-empty">Could not load activity.</p>`;
    }
  });
}

function openModal(id) {
  const meme = getMemeById(id);
  if (!meme) return;
  openModalWithMeme(meme);
}

function hasUnsavedModalChanges() {
  if (!canEditMetadata()) return false;
  if (!activeMemeId || !modalSnapshot) return false;
  return (
    modalFavorite.classList.contains("is-active") !== modalSnapshot.favorite ||
    modalNotesInput.value !== modalSnapshot.notes ||
    getModalTagValues().sort().join(", ") !== modalSnapshot.tags
  );
}

function closeModal() {
  clearMemeModalUIHideTimer();
  if (hasUnsavedModalChanges()) {
    const shouldClose = window.confirm("Discard unsaved changes?");
    if (!shouldClose) return false;
  }

  const media = modalPreview.querySelector("video, audio");
  if (media) {
    media.pause();
  }
  if (memeModal.open) {
    memeModal.close();
  }
  memeModal.classList.remove("meme-modal-ui-hidden");
  memeModal.classList.remove("details-open");
  modalPanelToggle?.setAttribute("aria-expanded", "false");
  modalPanelToggle?.setAttribute("aria-label", "Open details");
  activeMemeId = null;
  modalSnapshot = null;
  setModalAuditVisibility(false);
  overlayClose.classList.add("hidden");
  return true;
}

function syncModalPanelToggle() {
  if (!modalPanelToggle && !modalDrawerToggle) {
    return;
  }

  const expanded = memeModal.classList.contains("details-open");
  modalPanelToggle?.setAttribute("aria-expanded", String(expanded));
  modalPanelToggle?.setAttribute("aria-label", expanded ? "Close details" : "Open details");
  if (modalDrawerToggle) {
    modalDrawerToggle.setAttribute("aria-expanded", String(expanded));
    modalDrawerToggle.setAttribute("aria-label", expanded ? "Close details" : "Open details");
    modalDrawerToggle.textContent = "Details";
  }
}

function toggleModalDetailsPanel(forceOpen) {
  if (!memeModal) {
    return;
  }

  const nextState = typeof forceOpen === "boolean"
    ? forceOpen
    : !memeModal.classList.contains("details-open");
  memeModal.classList.toggle("details-open", nextState);
  syncModalPanelToggle();
}

function formatMediaTime(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "0:00";
  }

  const totalSeconds = Math.floor(seconds);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const remainingSeconds = totalSeconds % 60;

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(remainingSeconds).padStart(2, "0")}`;
  }

  return `${minutes}:${String(remainingSeconds).padStart(2, "0")}`;
}

function getModalMediaControlTarget() {
  return modalPreview.querySelector("video, audio");
}

function syncModalMediaControls() {
  const media = getModalMediaControlTarget();
  const supportsMediaControls = !!media;

  modalMediaControls?.classList.toggle("hidden", !supportsMediaControls);
  modalPlay.disabled = !supportsMediaControls;
  modalVolumeToggle.disabled = !supportsMediaControls;
  modalVolume.disabled = !supportsMediaControls;
  modalVolumeWrap.classList.toggle("hidden", !supportsMediaControls);
  const isVideo = media instanceof HTMLVideoElement;
  modalProgressWrap?.classList.toggle("hidden", !isVideo);

  if (!supportsMediaControls) {
    showMemeModalUI();
    modalPlay.setAttribute("aria-label", "Play media");
    modalPlay.setAttribute("data-tooltip", "Play");
    modalPlayIcon.innerHTML = "&#9654;";
    modalVolumeToggle.setAttribute("aria-label", "Mute media");
    modalVolumeIcon.innerHTML = "&#128266;";
    modalVolume.value = `${Math.round(loadPreferredMediaVolume() * 100)}`;
    if (modalCurrentTime) modalCurrentTime.textContent = "0:00";
    if (modalDuration) modalDuration.textContent = "0:00";
    if (modalProgress) modalProgress.value = "0";
    return;
  }

  const paused = media.paused;
  const muted = media.muted || media.volume === 0;

  modalPlay.setAttribute("aria-label", paused ? "Play media" : "Pause media");
  modalPlay.setAttribute("data-tooltip", paused ? "Play" : "Pause");
  modalPlayIcon.innerHTML = paused ? "&#9654;" : "&#10074;&#10074;";

  modalVolumeToggle.setAttribute("aria-label", muted ? "Unmute media" : "Mute media");
  modalVolumeIcon.innerHTML = muted ? "&#128263;" : "&#128266;";
  modalVolume.value = `${Math.round((media.muted ? 0 : media.volume) * 100)}`;

  if (!isVideo) {
    return;
  }

  const duration = Number.isFinite(media.duration) ? media.duration : 0;
  const currentTime = Number.isFinite(media.currentTime) ? media.currentTime : 0;
  const progressValue = duration > 0
    ? Math.round((currentTime / duration) * MODAL_PROGRESS_SCALE_MAX)
    : 0;

  if (modalCurrentTime) {
    modalCurrentTime.textContent = formatMediaTime(currentTime);
  }
  if (modalDuration) {
    modalDuration.textContent = formatMediaTime(duration);
  }
  if (modalProgress) {
    modalProgress.value = `${Math.max(0, Math.min(MODAL_PROGRESS_SCALE_MAX, progressValue))}`;
    modalProgress.disabled = duration <= 0;
  }
}

function splitTags(raw) {
  return raw
    .split(",")
    .map((tag) => tag.trim().toLowerCase())
    .filter(Boolean);
}

function getAllKnownTags() {
  const tags = new Set();
  for (const meme of state.memes) {
    for (const tag of meme.tags || []) {
      tags.add(tag);
    }
  }
  return [...tags].sort();
}

function normalizeTagValue(tag) {
  return tag.trim().toLowerCase();
}

function createModalTag(tag) {
  return {
    id: `tag-${modalTagSequence += 1}`,
    value: normalizeTagValue(tag),
  };
}

function setModalTags(tags) {
  const seen = new Set();
  modalTagState = [];

  for (const rawTag of tags || []) {
    const value = normalizeTagValue(rawTag);
    if (!value || seen.has(value)) continue;
    seen.add(value);
    modalTagState.push(createModalTag(value));
  }
}

function getModalTagValues() {
  return modalTagState.map((tag) => tag.value);
}

function addModalTag(rawTag) {
  if (!canAddTags()) return;
  const tag = normalizeTagValue(rawTag);
  if (!tag || modalTagState.some((entry) => entry.value === tag)) return;
  modalTagState = [...modalTagState, createModalTag(tag)];
  modalTagsInput.value = "";
  modalSuggestionState = [];
  activeSuggestionIndex = -1;
  renderTagEditor();
  renderTagSuggestions();
}

function removeModalTag(tagIdToRemove) {
  if (!canRemoveTags()) return;
  modalTagState = modalTagState.filter((tag) => tag.id !== tagIdToRemove);
  if (!normalizeTagValue(modalTagsInput.value)) {
    modalSuggestionState = [];
  }
  renderTagEditor();
  renderTagSuggestions();
}

function renderTagEditor() {
  modalTagChips.innerHTML = "";
  for (const tag of modalTagState) {
    const chip = document.createElement("span");
    chip.className = "tag-token";
    chip.dataset.tagId = tag.id;

    const label = document.createElement("span");
    label.className = "tag-token-label";
    label.textContent = tag.value;

    const removeButton = document.createElement("button");
    removeButton.type = "button";
    removeButton.className = "tag-token-remove";
    removeButton.setAttribute("aria-label", `Remove ${tag.value}`);
    removeButton.dataset.tagId = tag.id;
    removeButton.textContent = "x";
    removeButton.disabled = !canRemoveTags();
    removeButton.addEventListener("pointerdown", (event) => {
      if (!canRemoveTags()) return;
      event.preventDefault();
      event.stopPropagation();
      removeModalTag(tag.id);
      modalTagsInput.focus();
    });

    chip.append(label, removeButton);
    modalTagChips.appendChild(chip);
  }
}

function renderTagSuggestions() {
  modalTagSuggestions.innerHTML = "";

  if (modalSuggestionState.length === 0) {
    modalTagSuggestions.classList.add("hidden");
    activeSuggestionIndex = -1;
    return;
  }

  modalSuggestionState.forEach((tag, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "tag-suggestion";
    if (index === activeSuggestionIndex) {
      button.classList.add("is-active");
    }
    button.innerHTML = `<span>${tag}</span><span class="tag-suggestion-hint">existing</span>`;
    button.addEventListener("click", () => {
      addModalTag(tag);
      modalTagsInput.focus();
    });
    modalTagSuggestions.appendChild(button);
  });

  modalTagSuggestions.classList.remove("hidden");
}

async function fetchTagSuggestions() {
  if (!canAddTags()) return;
  const needle = normalizeTagValue(modalTagsInput.value);
  if (!needle) {
    if (tagSuggestionAbortController) {
      tagSuggestionAbortController.abort();
      tagSuggestionAbortController = null;
    }
    modalSuggestionState = [];
    renderTagSuggestions();
    return;
  }

  if (tagSuggestionAbortController) {
    tagSuggestionAbortController.abort();
  }

  tagSuggestionAbortController = new AbortController();

  try {
    const response = await fetch(`/api/tags?q=${encodeURIComponent(needle)}`, {
      signal: tagSuggestionAbortController.signal,
    });
    if (!response.ok) {
      return;
    }

    const payload = await response.json();
    if (normalizeTagValue(modalTagsInput.value) !== needle) {
      return;
    }
    modalSuggestionState = (payload.tags || []).filter((tag) => !modalTagState.some((entry) => entry.value === tag));
    renderTagSuggestions();
  } catch (error) {
    if (error.name !== "AbortError") {
      console.error(error);
    }
  }
}

function syncUploadTagsField() {
  uploadTagsHidden.value = uploadTagState.map((tag) => tag.value).join(", ");
}

function resetUploadTags() {
  uploadTagState = [];
  uploadSuggestionState = [];
  activeUploadSuggestionIndex = -1;
  if (uploadTagSuggestionAbortController) {
    uploadTagSuggestionAbortController.abort();
    uploadTagSuggestionAbortController = null;
  }
  uploadTagsInput.value = "";
  syncUploadTagsField();
  renderUploadTagEditor();
  renderUploadTagSuggestions();
}

function getUploadTagValues() {
  return uploadTagState.map((tag) => tag.value);
}

function addUploadTag(rawTag) {
  const tag = normalizeTagValue(rawTag);
  if (!tag || uploadTagState.some((entry) => entry.value === tag)) return;
  uploadTagState = [...uploadTagState, createModalTag(tag)];
  uploadTagsInput.value = "";
  uploadSuggestionState = [];
  activeUploadSuggestionIndex = -1;
  syncUploadTagsField();
  renderUploadTagEditor();
  renderUploadTagSuggestions();
}

function removeUploadTag(tagIdToRemove) {
  uploadTagState = uploadTagState.filter((tag) => tag.id !== tagIdToRemove);
  if (!normalizeTagValue(uploadTagsInput.value)) {
    uploadSuggestionState = [];
  }
  syncUploadTagsField();
  renderUploadTagEditor();
  renderUploadTagSuggestions();
}

function renderUploadTagEditor() {
  uploadTagChips.innerHTML = "";
  for (const tag of uploadTagState) {
    const chip = document.createElement("span");
    chip.className = "tag-token";

    const label = document.createElement("span");
    label.className = "tag-token-label";
    label.textContent = tag.value;

    const removeButton = document.createElement("button");
    removeButton.type = "button";
    removeButton.className = "tag-token-remove";
    removeButton.setAttribute("aria-label", `Remove ${tag.value}`);
    removeButton.textContent = "x";
    removeButton.addEventListener("pointerdown", (event) => {
      event.preventDefault();
      event.stopPropagation();
      removeUploadTag(tag.id);
      uploadTagsInput.focus();
    });

    chip.append(label, removeButton);
    uploadTagChips.appendChild(chip);
  }
}

function renderUploadTagSuggestions() {
  uploadTagSuggestions.innerHTML = "";

  if (uploadSuggestionState.length === 0) {
    uploadTagSuggestions.classList.add("hidden");
    activeUploadSuggestionIndex = -1;
    return;
  }

  uploadSuggestionState.forEach((tag, index) => {
    const button = document.createElement("button");
    button.type = "button";
    button.className = "tag-suggestion";
    if (index === activeUploadSuggestionIndex) {
      button.classList.add("is-active");
    }
    button.innerHTML = `<span>${tag}</span><span class="tag-suggestion-hint">existing</span>`;
    button.addEventListener("click", () => {
      addUploadTag(tag);
      uploadTagsInput.focus();
    });
    uploadTagSuggestions.appendChild(button);
  });

  uploadTagSuggestions.classList.remove("hidden");
}

async function fetchUploadTagSuggestions() {
  if (!canAdd()) return;
  const needle = normalizeTagValue(uploadTagsInput.value);
  if (!needle) {
    if (uploadTagSuggestionAbortController) {
      uploadTagSuggestionAbortController.abort();
      uploadTagSuggestionAbortController = null;
    }
    uploadSuggestionState = [];
    renderUploadTagSuggestions();
    return;
  }

  if (uploadTagSuggestionAbortController) {
    uploadTagSuggestionAbortController.abort();
  }

  uploadTagSuggestionAbortController = new AbortController();

  try {
    const response = await fetch(`/api/tags?q=${encodeURIComponent(needle)}`, {
      signal: uploadTagSuggestionAbortController.signal,
    });
    if (!response.ok) {
      return;
    }

    const payload = await response.json();
    if (normalizeTagValue(uploadTagsInput.value) !== needle) {
      return;
    }
    uploadSuggestionState = (payload.tags || []).filter((tag) => !uploadTagState.some((entry) => entry.value === tag));
    renderUploadTagSuggestions();
  } catch (error) {
    if (error.name !== "AbortError") {
      console.error(error);
    }
  }
}

async function persistCard(id, payload) {
  if (!canEditMetadata()) {
    window.alert("You do not have permission to edit metadata.");
    return false;
  }

  const response = await fetch(`/api/memes/${id}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(payload),
  });

  if (!(await expectAuthorized(response, "Save failed."))) {
    return false;
  }

  await loadInitialMemes();
  return true;
}

async function persistFavorite(id, favorite) {
  if (!canView()) {
    window.alert("You do not have permission to favorite memes.");
    return false;
  }

  const response = await fetch(`/api/memes/${id}/favorite`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ favorite }),
  });

  if (!(await expectAuthorized(response, "Favorite update failed."))) {
    return false;
  }

  const updatedMeme = await response.json();
  updateMemeInState(updatedMeme);
  syncFavoriteUI(updatedMeme);
  return updatedMeme;
}

async function deleteMeme(id) {
  if (!canDeleteMemes()) {
    window.alert("You do not have permission to delete memes.");
    return;
  }

  const meme = getMemeById(id);
  const confirmed = window.confirm(`Delete ${meme?.originalName || "this file"}?`);
  if (!confirmed) return;

  const response = await fetch(`/api/memes/${id}`, { method: "DELETE" });
  if (!(await expectAuthorized(response, "Delete failed."))) {
    return;
  }

  if (response.status === 202) {
    closeModal();
    await loadInitialMemes();
    return;
  }

  closeModal();
  await loadInitialMemes();
}

uploadForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!canAdd()) {
    uploadStatus.textContent = "You do not have permission to upload.";
    return;
  }
  const totalFiles = uploadFileInput.files?.length || 0;
  uploadStatus.textContent = totalFiles > 1 ? `Uploading ${totalFiles} files...` : "Uploading...";
  syncUploadTagsField();

  const formData = new FormData(uploadForm);
  const response = await fetch("/api/memes", {
    method: "POST",
    body: formData,
  });

  if (!(await expectAuthorized(response, "Upload failed."))) {
    uploadStatus.textContent = "Upload failed.";
    return;
  }

  const payload = await response.json();
  uploadForm.reset();
  resetUploadTags();
  clearUploadPreview();
  const createdCount = Number(payload.created || 0);
  const skippedCount = Number(payload.skipped || 0);
  if (createdCount > 0 && skippedCount > 0) {
    uploadStatus.textContent = `${createdCount} uploaded, ${skippedCount} duplicate${skippedCount === 1 ? "" : "s"} skipped.`;
  } else if (createdCount > 0) {
    uploadStatus.textContent = createdCount > 1 ? `${createdCount} files uploaded.` : "Upload complete.";
  } else if (skippedCount > 0) {
    uploadStatus.textContent = `${skippedCount} duplicate${skippedCount === 1 ? "" : "s"} skipped.`;
  } else {
    uploadStatus.textContent = "Upload complete.";
  }
  await loadInitialMemes();
  uploadModal.close();
});

openUploadModalButton.addEventListener("click", () => {
  if (!canAdd()) return;
  uploadStatus.textContent = "";
  uploadForm.reset();
  resetUploadTags();
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.showModal();
});

openRandomReelButton?.addEventListener("click", () => {
  openRandomReel().catch((error) => {
    console.error(error);
  });
});

uploadModalClose.addEventListener("click", () => {
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.close();
});

uploadModal.addEventListener("click", (event) => {
  if (event.target !== uploadModal) return;
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.close();
});

uploadModal.addEventListener("cancel", (event) => {
  event.preventDefault();
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.close();
});

usersModalClose?.addEventListener("click", () => {
  usersModal?.close();
});

usersModal?.addEventListener("click", (event) => {
  if (event.target !== usersModal) return;
  usersModal.close();
});

usersModal?.addEventListener("cancel", (event) => {
  event.preventDefault();
  usersModal.close();
});

deleteQueueClose?.addEventListener("click", () => {
  deleteQueueModal?.close();
});

deleteQueueModal?.addEventListener("click", (event) => {
  if (event.target !== deleteQueueModal) return;
  deleteQueueModal.close();
});

deleteQueueModal?.addEventListener("cancel", (event) => {
  event.preventDefault();
  deleteQueueModal.close();
});

auditLogsClose?.addEventListener("click", () => {
  auditLogsModal?.close();
});

auditLogsModal?.addEventListener("click", (event) => {
  if (event.target !== auditLogsModal) return;
  auditLogsModal.close();
});

auditLogsModal?.addEventListener("cancel", (event) => {
  event.preventDefault();
  auditLogsModal.close();
});

usersAddForm?.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!canManageUsers()) return;

  const userID = usersAddID?.value?.trim();
  if (!userID) {
    usersModalStatus.textContent = "Enter a Discord user ID first.";
    return;
  }

  usersModalStatus.textContent = "Adding user...";
  const response = await fetch("/api/users", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ user_id: userID }),
  });
  if (!(await expectAuthorized(response, "Failed to add user."))) {
    usersModalStatus.textContent = "Could not add user.";
    return;
  }

  usersModalStatus.textContent = `Added ${userID}.`;
  if (usersAddID) {
    usersAddID.value = "";
  }
  await fetchManagedUsers();
});

uploadFileInput.addEventListener("change", () => {
  renderUploadPreview(uploadFileInput.files?.[0], uploadFileInput.files?.length ?? 0);
});

uploadPreviewWrap?.addEventListener("dragenter", (event) => {
  event.preventDefault();
  event.stopPropagation();
  uploadDragDepth += 1;
  setUploadDragActive(true);
});

uploadPreviewWrap?.addEventListener("dragover", (event) => {
  event.preventDefault();
  event.stopPropagation();
  if (event.dataTransfer) {
    event.dataTransfer.dropEffect = "copy";
  }
  setUploadDragActive(true);
});

uploadPreviewWrap?.addEventListener("dragleave", (event) => {
  event.preventDefault();
  event.stopPropagation();
  uploadDragDepth = Math.max(0, uploadDragDepth - 1);
  if (uploadDragDepth === 0) {
    setUploadDragActive(false);
  }
});

uploadPreviewWrap?.addEventListener("drop", (event) => {
  event.preventDefault();
  event.stopPropagation();
  uploadDragDepth = 0;
  setUploadDragActive(false);

  const files = [...(event.dataTransfer?.files || [])].filter((file) => file && file.size >= 0);
  if (files.length === 0) {
    return;
  }

  setUploadFiles(files);
  uploadStatus.textContent = files.length > 1
    ? `${files.length} files ready to upload.`
    : `${files[0].name} ready to upload.`;
});

uploadTagsInput.addEventListener("input", () => {
  activeUploadSuggestionIndex = -1;
  fetchUploadTagSuggestions();
});

uploadTagsInput.addEventListener("keydown", (event) => {
  if (event.key === "ArrowDown" && uploadSuggestionState.length > 0) {
    event.preventDefault();
    activeUploadSuggestionIndex = (activeUploadSuggestionIndex + 1) % uploadSuggestionState.length;
    renderUploadTagSuggestions();
    return;
  }

  if (event.key === "ArrowUp" && uploadSuggestionState.length > 0) {
    event.preventDefault();
    activeUploadSuggestionIndex = activeUploadSuggestionIndex <= 0 ? uploadSuggestionState.length - 1 : activeUploadSuggestionIndex - 1;
    renderUploadTagSuggestions();
    return;
  }

  if ((event.key === "Enter" || event.key === "Tab" || event.key === ",") && uploadTagsInput.value.trim()) {
    event.preventDefault();
    if (activeUploadSuggestionIndex >= 0 && uploadSuggestionState[activeUploadSuggestionIndex]) {
      addUploadTag(uploadSuggestionState[activeUploadSuggestionIndex]);
    } else {
      addUploadTag(uploadTagsInput.value);
    }
    return;
  }

  if (event.key === "Backspace" && !uploadTagsInput.value && uploadTagState.length > 0) {
    removeUploadTag(uploadTagState[uploadTagState.length - 1].id);
  }

  if (event.key === "Escape") {
    uploadTagSuggestions.classList.add("hidden");
    activeUploadSuggestionIndex = -1;
  }
});

tagSearchInput.addEventListener("input", async (event) => {
  activeTopTagSuggestionIndex = -1;
  fetchTopTagSuggestions();

  const nextValue = event.target.value;
  if (!normalizeTagValue(nextValue)) {
    if (topTagSearchDebounce) {
      window.clearTimeout(topTagSearchDebounce);
      topTagSearchDebounce = null;
    }
    await applyTagSearch("");
    return;
  }

  queueTagSearch(nextValue);
});

tagSearchInput.addEventListener("keydown", async (event) => {
  if (event.key === "ArrowDown" && topTagSuggestionState.length > 0) {
    event.preventDefault();
    activeTopTagSuggestionIndex = (activeTopTagSuggestionIndex + 1) % topTagSuggestionState.length;
    renderTopTagSuggestions();
    return;
  }

  if (event.key === "ArrowUp" && topTagSuggestionState.length > 0) {
    event.preventDefault();
    activeTopTagSuggestionIndex = activeTopTagSuggestionIndex <= 0 ? topTagSuggestionState.length - 1 : activeTopTagSuggestionIndex - 1;
    renderTopTagSuggestions();
    return;
  }

  if (event.key === "Enter" && topTagSuggestionState.length > 0 && activeTopTagSuggestionIndex >= 0) {
    event.preventDefault();
    const selectedTag = topTagSuggestionState[activeTopTagSuggestionIndex];
    tagSearchInput.value = selectedTag;
    topTagSuggestionState = [];
    renderTopTagSuggestions();
    if (topTagSearchDebounce) {
      window.clearTimeout(topTagSearchDebounce);
      topTagSearchDebounce = null;
    }
    await applyTagSearch(selectedTag);
    return;
  }

  if (event.key === "Enter") {
    event.preventDefault();
    topTagSuggestionState = [];
    renderTopTagSuggestions();
    if (topTagSearchDebounce) {
      window.clearTimeout(topTagSearchDebounce);
      topTagSearchDebounce = null;
    }
    await applyTagSearch(tagSearchInput.value);
    return;
  }

  if (event.key === "Escape") {
    if (topTagSearchDebounce) {
      window.clearTimeout(topTagSearchDebounce);
      topTagSearchDebounce = null;
    }
    topTagSuggestionState = [];
    renderTopTagSuggestions();
  }
});

sidebarNavItems.forEach((item) => {
  item.addEventListener("click", () => {
    state.filters.view = item.dataset.view || "library";
    loadInitialMemes().catch((error) => {
      console.error(error);
    });
    if (drawerMediaQuery.matches) {
      closeSidebarDrawer();
    }
  });
});

sidebarToggle?.addEventListener("click", () => {
  if (drawerMediaQuery.matches) {
    if (document.body.classList.contains("drawer-open")) {
      closeSidebarDrawer();
    } else {
      openSidebarDrawer();
    }
    return;
  }
  setSidebarCollapsed(!document.body.classList.contains("sidebar-collapsed"));
});

drawerToggle?.addEventListener("click", () => {
  if (!drawerMediaQuery.matches) return;
  if (document.body.classList.contains("drawer-open")) {
    closeSidebarDrawer();
  } else {
    openSidebarDrawer();
  }
});

drawerBackdrop?.addEventListener("click", () => {
  closeSidebarDrawer();
});

authTrigger?.addEventListener("click", (event) => {
  event.stopPropagation();
  toggleAuthMenu();
});

authVersion?.addEventListener("click", (event) => {
  event.stopPropagation();
  forceFreshHTMLReload();
});

authManageUsers?.addEventListener("click", (event) => {
  event.stopPropagation();
  closeAuthMenu();
  openUsersModal().catch((error) => {
    console.error(error);
    usersModalStatus.textContent = "Could not load users.";
  });
});

authDeleteQueue?.addEventListener("click", (event) => {
  event.stopPropagation();
  closeAuthMenu();
  state.admin.queue.offset = 0;
  state.filters.view = "admin-delete-queue";
  loadInitialMemes().catch((error) => {
    console.error(error);
    adminViewStatus.textContent = "Could not load delete queue.";
  });
});

authAuditLogs?.addEventListener("click", (event) => {
  event.stopPropagation();
  closeAuthMenu();
  state.admin.audit.offset = 0;
  state.filters.view = "admin-audit-logs";
  loadInitialMemes().catch((error) => {
    console.error(error);
    adminViewStatus.textContent = "Could not load audit logs.";
  });
});

adminPagePrev?.addEventListener("click", () => {
  const pageState = getActiveAdminPageState();
  if (!pageState || pageState.offset <= 0) return;
  pageState.offset = Math.max(0, pageState.offset - pageState.limit);
  loadInitialMemes().catch((error) => {
    console.error(error);
    adminViewStatus.textContent = "Could not load admin page.";
  });
});

adminPageNext?.addEventListener("click", () => {
  const pageState = getActiveAdminPageState();
  if (!pageState || !pageState.hasMore) return;
  pageState.offset += pageState.limit;
  loadInitialMemes().catch((error) => {
    console.error(error);
    adminViewStatus.textContent = "Could not load admin page.";
  });
});

document.addEventListener("click", (event) => {
  if (!authPanelContains(event.target)) {
    closeAuthMenu();
  }
});

overlayClose.addEventListener("click", () => {
  showMemeModalUI(false);
  closeModal();
});

modalCloseButton?.addEventListener("click", () => {
  showMemeModalUI(false);
  closeModal();
});

modalPanelToggle?.addEventListener("click", () => {
  showMemeModalUI();
  toggleModalDetailsPanel();
});

modalDrawerToggle?.addEventListener("click", () => {
  showMemeModalUI();
  toggleModalDetailsPanel();
});

modalDrawerClose?.addEventListener("click", () => {
  showMemeModalUI(false);
  closeModal();
});

memeModal.addEventListener("close", () => {
  clearMemeModalUIHideTimer();
  activeMemeId = null;
  modalSnapshot = null;
  memeModal.classList.remove("meme-modal-ui-hidden");
  memeModal.classList.remove("details-open");
  syncModalPanelToggle();
  overlayClose.classList.add("hidden");
});

memeModal.addEventListener("click", (event) => {
  const clickedDrawerControl = modalPanelToggle?.contains(event.target)
    || modalDrawerToggle?.contains(event.target)
    || modalDrawerClose?.contains(event.target)
    || modalCloseButton?.contains(event.target);

  if (memeModal.classList.contains("details-open") && modalBody && !modalBody.contains(event.target) && !clickedDrawerControl) {
    showMemeModalUI();
    toggleModalDetailsPanel(false);
    return;
  }

  if (event.target !== memeModal) {
    showMemeModalUI();
    return;
  }
  closeModal();
});

memeModal.addEventListener("cancel", (event) => {
  event.preventDefault();
  closeModal();
});

memeModal?.addEventListener("mousemove", () => {
  if (!memeModal.open) {
    return;
  }
  showMemeModalUI();
});

modalPreviewWrap?.addEventListener("mouseenter", () => {
  if (!memeModal.open) {
    return;
  }
  showMemeModalUI();
});

modalPreview?.addEventListener("click", async (event) => {
  const media = modalPreview.querySelector("video");
  if (!media || event.target !== media) {
    return;
  }

  showMemeModalUI();

  try {
    if (media.paused) {
      await media.play();
    } else {
      media.pause();
    }
  } catch (error) {
    console.error(error);
  }

  syncModalMediaControls();
});

modalPlay?.addEventListener("click", async () => {
  showMemeModalUI();
  const media = getModalMediaControlTarget();
  if (!media) return;

  try {
    if (media.paused) {
      await media.play();
    } else {
      media.pause();
    }
  } catch (error) {
    console.error(error);
  }

  syncModalMediaControls();
});

modalVolumeToggle?.addEventListener("click", () => {
  showMemeModalUI();
  const media = getModalMediaControlTarget();
  if (!media) return;

  media.muted = !media.muted;
  syncStoredMediaVolumeFromElement(media);
  syncModalMediaControls();
});

modalVolume?.addEventListener("input", () => {
  showMemeModalUI();
  const media = getModalMediaControlTarget();
  if (!media) return;

  const volume = Number(modalVolume.value) / 100;
  media.volume = volume;
  media.muted = volume === 0;
  syncStoredMediaVolumeFromElement(media);
  syncModalMediaControls();
});

modalProgress?.addEventListener("input", () => {
  showMemeModalUI();
  const media = getModalMediaControlTarget();
  if (!(media instanceof HTMLVideoElement)) {
    return;
  }

  const duration = Number.isFinite(media.duration) ? media.duration : 0;
  if (duration <= 0) {
    return;
  }

  const progressRatio = Number(modalProgress.value) / MODAL_PROGRESS_SCALE_MAX;
  media.currentTime = Math.max(0, Math.min(duration, duration * progressRatio));
  syncModalMediaControls();
});

randomReelClose?.addEventListener("click", () => {
  closeRandomReel();
});

randomReelPrev?.addEventListener("click", () => {
  showRandomReelUI();
  stepRandomReel(-1).catch((error) => {
    console.error(error);
  });
});

randomReelNext?.addEventListener("click", () => {
  showRandomReelUI();
  stepRandomReel(1).catch((error) => {
    console.error(error);
  });
});

randomReelFavorite?.addEventListener("click", async () => {
  showRandomReelUI();
  if (!canView() || !randomReelActiveMemeID) return;
  const meme = getMemeById(randomReelActiveMemeID);
  const updatedMeme = await persistFavorite(randomReelActiveMemeID, !meme?.favorite);
  if (!updatedMeme) return;
  syncRandomReelFavoriteUI(updatedMeme);
});

randomReelPlay?.addEventListener("click", async () => {
  showRandomReelUI();
  const media = getRandomReelMediaControlTarget();
  if (!media) return;

  try {
    if (media.paused) {
      await media.play();
    } else {
      media.pause();
    }
  } catch (error) {
    console.error(error);
  }

  syncRandomReelMediaControls();
});

randomReelVolumeToggle?.addEventListener("click", () => {
  showRandomReelUI();
  const media = getRandomReelMediaControlTarget();
  if (!media) return;

  media.muted = !media.muted;
  syncStoredMediaVolumeFromElement(media);
  syncRandomReelMediaControls();
});

randomReelVolume?.addEventListener("input", () => {
  showRandomReelUI();
  const media = getRandomReelMediaControlTarget();
  if (!media) return;

  const volume = Number(randomReelVolume.value) / 100;
  media.volume = volume;
  media.muted = volume === 0;
  syncStoredMediaVolumeFromElement(media);
  syncRandomReelMediaControls();
});

randomReelMedia?.addEventListener("click", async (event) => {
  if (randomReelControlsContains(event.target)) {
    return;
  }

  const media = getRandomReelMediaControlTarget();
  if (!media || !event.target || !(event.target === media || media.contains(event.target))) {
    showRandomReelUI();
    syncRandomReelMediaControls();
    return;
  }

  showRandomReelUI();

  try {
    if (media.paused) {
      await media.play();
    } else {
      media.pause();
    }
  } catch (error) {
    console.error(error);
  }

  syncRandomReelMediaControls();
});

randomReelStage?.addEventListener("click", (event) => {
  if (randomReelControlsContains(event.target) || randomReelMedia.contains(event.target)) {
    return;
  }
  showRandomReelUI();
  syncRandomReelMediaControls();
});

randomReelModal?.addEventListener("click", (event) => {
  if (event.target !== randomReelModal) {
    showRandomReelUI();
    return;
  }
  closeRandomReel();
});

randomReelModal?.addEventListener("mousemove", () => {
  if (!randomReelModal.open) {
    return;
  }
  showRandomReelUI();
});

randomReelModal?.addEventListener("mouseenter", () => {
  if (!randomReelModal.open) {
    return;
  }
  showRandomReelUI();
});

randomReelModal?.addEventListener("cancel", (event) => {
  event.preventDefault();
  closeRandomReel();
});

randomReelModal?.addEventListener("wheel", (event) => {
  if (!randomReelModal.open || randomReelWheelLock || Math.abs(event.deltaY) < 18) {
    return;
  }

  event.preventDefault();
  showRandomReelUI();
  randomReelWheelLock = true;
  stepRandomReel(event.deltaY > 0 ? 1 : -1).catch((error) => {
    console.error(error);
  });
  window.clearTimeout(randomReelWheelTimeout);
  randomReelWheelTimeout = window.setTimeout(() => {
    randomReelWheelLock = false;
  }, 280);
}, { passive: false });

randomReelModal?.addEventListener("touchstart", (event) => {
  if (!randomReelModal.open) {
    return;
  }

  const touch = event.touches?.[0];
  if (!touch) {
    return;
  }

  randomReelTouchBlocked = randomReelControlsContains(event.target);
  randomReelTouchStartY = touch.clientY;
  randomReelTouchDeltaY = 0;
  randomReelTouchActive = true;
  resetRandomReelDrag();
  showRandomReelUI();
}, { passive: true });

randomReelModal?.addEventListener("touchmove", (event) => {
  if (!randomReelModal.open || !randomReelTouchActive || randomReelTouchBlocked) {
    return;
  }

  const touch = event.touches?.[0];
  if (!touch || randomReelTouchStartY === null) {
    return;
  }

  randomReelTouchDeltaY = touch.clientY - randomReelTouchStartY;
  if (Math.abs(randomReelTouchDeltaY) > 12) {
    applyRandomReelDrag(randomReelTouchDeltaY);
    event.preventDefault();
  }
}, { passive: false });

randomReelModal?.addEventListener("touchend", (event) => {
  if (!randomReelModal.open || !randomReelTouchActive) {
    return;
  }

  const endTouch = event.changedTouches?.[0];
  const deltaY = endTouch && randomReelTouchStartY !== null
    ? endTouch.clientY - randomReelTouchStartY
    : randomReelTouchDeltaY;

  randomReelTouchStartY = null;
  randomReelTouchDeltaY = 0;
  randomReelTouchActive = false;

  const touchBlocked = randomReelTouchBlocked;
  randomReelTouchBlocked = false;
  if (touchBlocked || Math.abs(deltaY) < 56) {
    resetRandomReelDrag();
    return;
  }

  showRandomReelUI();
  stepRandomReel(deltaY < 0 ? 1 : -1).catch((error) => {
    console.error(error);
  });
}, { passive: true });

randomReelModal?.addEventListener("touchcancel", () => {
  randomReelTouchStartY = null;
  randomReelTouchDeltaY = 0;
  randomReelTouchActive = false;
  randomReelTouchBlocked = false;
  resetRandomReelDrag();
}, { passive: true });

window.addEventListener("keydown", (event) => {
  if (event.key === "Escape") {
    closeAuthMenu();
  }

  if (!randomReelModal?.open) return;

  if (event.key === "ArrowDown" || event.key === "PageDown" || event.key === " ") {
    event.preventDefault();
    showRandomReelUI();
    stepRandomReel(1).catch((error) => {
      console.error(error);
    });
    return;
  }

  if (event.key === "ArrowUp" || event.key === "PageUp") {
    event.preventDefault();
    showRandomReelUI();
    stepRandomReel(-1).catch((error) => {
      console.error(error);
    });
    return;
  }

  if (event.key === "Escape") {
    event.preventDefault();
    closeRandomReel();
  }
});

modalSave.addEventListener("click", async () => {
  if (!canEditMetadata()) return;
  if (!activeMemeId) return;
  const saved = await persistCard(activeMemeId, {
    favorite: modalFavorite.classList.contains("is-active"),
    notes: modalNotesInput.value,
    tags: getModalTagValues(),
  });
  if (!saved) return;
  modalSnapshot = {
    favorite: modalFavorite.classList.contains("is-active"),
    notes: modalNotesInput.value,
    tags: [...getModalTagValues()].sort().join(", "),
  };
  closeModal();
});

modalFavorite.addEventListener("click", async () => {
  if (!canView()) return;
  if (!activeMemeId) return;
  const meme = getMemeById(activeMemeId);
  const updatedMeme = await persistFavorite(activeMemeId, !meme?.favorite);
  if (!updatedMeme) return;
  openModal(activeMemeId);
});

modalDelete.addEventListener("click", async () => {
  if (!canDeleteMemes()) return;
  if (!activeMemeId) return;
  await deleteMeme(activeMemeId);
});

modalTagsInput.addEventListener("input", () => {
  if (!canAddTags()) return;
  activeSuggestionIndex = -1;
  fetchTagSuggestions();
});

modalTagsInput.addEventListener("keydown", (event) => {
  if (event.key === "ArrowDown" && modalSuggestionState.length > 0) {
    event.preventDefault();
    activeSuggestionIndex = (activeSuggestionIndex + 1) % modalSuggestionState.length;
    renderTagSuggestions();
    return;
  }

  if (event.key === "ArrowUp" && modalSuggestionState.length > 0) {
    event.preventDefault();
    activeSuggestionIndex = activeSuggestionIndex <= 0 ? modalSuggestionState.length - 1 : activeSuggestionIndex - 1;
    renderTagSuggestions();
    return;
  }

  if ((event.key === "Enter" || event.key === "Tab" || event.key === ",") && modalTagsInput.value.trim()) {
    if (!canAddTags()) {
      return;
    }
    event.preventDefault();
    if (activeSuggestionIndex >= 0 && modalSuggestionState[activeSuggestionIndex]) {
      addModalTag(modalSuggestionState[activeSuggestionIndex]);
    } else {
      addModalTag(modalTagsInput.value);
    }
    return;
  }

  if (event.key === "Backspace" && !modalTagsInput.value && modalTagState.length > 0) {
    if (!canRemoveTags()) {
      return;
    }
    removeModalTag(modalTagState[modalTagState.length - 1].id);
  }

  if (event.key === "Escape") {
    modalTagSuggestions.classList.add("hidden");
    activeSuggestionIndex = -1;
  }
});

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

syncResponsiveSidebar();

fetchAuthSession()
  .then(() => loadInitialMemes())
  .catch((error) => {
    console.error(error);
    uploadStatus.textContent = "Could not load existing memes.";
  });

window.addEventListener("resize", () => {
  syncResponsiveSidebar();
  queueRenderLoadedMemes({ force: true });
  syncMemeGridObserver();
});

memePagePrev?.addEventListener("click", () => {
  if (state.library.loading || state.library.pageIndex === 0) {
    return;
  }

  fetchMemes({ page: state.library.pageIndex - 1 }).catch((error) => {
    console.error(error);
  });
});

memePageNext?.addEventListener("click", () => {
  if (state.library.loading || !state.library.hasMore) {
    return;
  }

  fetchMemes({ page: state.library.pageIndex + 1 }).catch((error) => {
    console.error(error);
  });
});

modalDetailsDrawerMediaQuery.addEventListener("change", () => {
  if (!modalDetailsDrawerMediaQuery.matches) {
    toggleModalDetailsPanel(false);
  } else {
    syncModalPanelToggle();
  }
});

function authPanelContains(target) {
  return !!target && document.querySelector("#auth-panel")?.contains(target);
}

function randomReelActionsContains(target) {
  return !!target && document.querySelector(".random-reel-actions")?.contains(target);
}

function randomReelCopyContains(target) {
  return !!target && document.querySelector(".random-reel-copy")?.contains(target);
}

function randomReelControlsContains(target) {
  return !!target && (
    randomReelClose?.contains(target) ||
    randomReelActionsContains(target) ||
    randomReelCopyContains(target) ||
    randomReelTags?.contains(target) ||
    randomReelHint?.contains(target)
  );
}
