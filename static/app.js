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
		sort: "newest",
		viewMode: "grid",
  },
  admin: {
    tab: "dashboard",
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
    tagReview: {
      offset: 0,
      limit: 50,
      total: 0,
      hasMore: false,
    },
    tagQueueStatus: null,
    linkRetryStatus: null,
    backupStatus: null,
    dashboard: null,
    tagHygiene: null,
    shares: [],
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
		query: "",
    view: "home",
  },
};

const LIBRARY_VIEW_TITLES = Object.freeze({
  library: "All Items",
  favorites: "Favorites",
  videos: "Videos",
  images: "Images",
  mp3s: "Audio",
  untagged: "Untagged",
  files: "Files",
});

const uploadForm = document.querySelector("#upload-form");
const uploadStatus = document.querySelector("#upload-status");
const uploadModal = document.querySelector("#upload-modal");
const uploadBusyOverlay = document.querySelector("#upload-busy-overlay");
const uploadBusyMessage = document.querySelector("#upload-busy-message");
const openUploadModalButton = document.querySelector("#open-upload-modal");
const addVaultModal = document.querySelector("#add-vault-modal");
const addVaultClose = document.querySelector("#add-vault-close");
const homeDashboard = document.querySelector("#home-dashboard");
const dashboardStats = document.querySelector("#dashboard-stats");
const dashboardRecent = document.querySelector("#dashboard-recent");
const dashboardFavorites = document.querySelector("#dashboard-favorites");
const dashboardRandom = document.querySelector("#dashboard-random");
const dashboardRandomRefresh = document.querySelector("#dashboard-random-refresh");
const dashboardStorageLabel = document.querySelector("#dashboard-storage-label");
const dashboardStorageBar = document.querySelector("#dashboard-storage-bar");
const dashboardTagsSection = document.querySelector("#dashboard-tags-section");
const dashboardTags = document.querySelector("#dashboard-tags");
const libraryHeading = document.querySelector("#library-heading");
const libraryTitle = document.querySelector("#library-title");
const filterPanel = document.querySelector("#filter-panel");
const filterToggle = document.querySelector("#filter-toggle");
const librarySort = document.querySelector("#library-sort");
const gridViewButton = document.querySelector("#grid-view-button");
const listViewButton = document.querySelector("#list-view-button");
const linkUploadModal = document.querySelector("#link-upload-modal");
const linkUploadBusyOverlay = document.querySelector("#link-upload-busy-overlay");
const linkUploadBusyMessage = document.querySelector("#link-upload-busy-message");
const linkUploadModalClose = document.querySelector("#link-upload-modal-close");
const linkUploadForm = document.querySelector("#link-upload-form");
const linkUploadInput = document.querySelector("#link-upload-input");
const linkUploadTagChips = document.querySelector("#link-upload-tag-chips");
const linkUploadTagsInput = document.querySelector("#link-upload-tags-input");
const linkUploadTagSuggestions = document.querySelector("#link-upload-tag-suggestions");
const linkUploadTagsHidden = document.querySelector("#link-upload-tags-hidden");
const linkUploadNotesInput = document.querySelector("#link-upload-notes-input");
const linkUploadStatus = document.querySelector("#link-upload-status");
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
const authAdmin = document.querySelector("#auth-admin");
const authInstall = document.querySelector("#auth-install");
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
const adminTabs = document.querySelectorAll(".admin-tab");
const adminViewStatus = document.querySelector("#admin-view-status");
const adminTagQueuePanel = document.querySelector("#admin-tag-queue-panel");
const adminTagQueueKicker = document.querySelector("#admin-tag-queue-kicker");
const adminTagQueueTitle = document.querySelector("#admin-tag-queue-title");
const adminTagQueueSummary = document.querySelector("#admin-tag-queue-summary");
const adminTagQueueReset = document.querySelector("#admin-tag-queue-reset");
const adminTagQueueGrid = document.querySelector("#admin-tag-queue-grid");
const adminTagQueueList = document.querySelector("#admin-tag-queue-list");
const adminLinkQueuePanel = document.querySelector("#admin-link-queue-panel");
const adminLinkQueueKicker = document.querySelector("#admin-link-queue-kicker");
const adminLinkQueueTitle = document.querySelector("#admin-link-queue-title");
const adminLinkQueueSummary = document.querySelector("#admin-link-queue-summary");
const adminLinkQueueGrid = document.querySelector("#admin-link-queue-grid");
const adminLinkQueueList = document.querySelector("#admin-link-queue-list");
const adminDashboardPanel = document.querySelector("#admin-dashboard-panel");
const adminDashboardGrid = document.querySelector("#admin-dashboard-grid");
const adminDashboardTags = document.querySelector("#admin-dashboard-tags");
const adminDashboardRecent = document.querySelector("#admin-dashboard-recent");
const adminDashboardUploadChart = document.querySelector("#admin-dashboard-upload-chart");
const adminDashboardHealth = document.querySelector("#admin-dashboard-health");
const adminDashboardActivity = document.querySelector("#admin-dashboard-activity");
const adminTagHygienePanel = document.querySelector("#admin-tag-hygiene-panel");
const adminTagHygienePairs = document.querySelector("#admin-tag-hygiene-pairs");
const adminTagHygieneTags = document.querySelector("#admin-tag-hygiene-tags");
const adminTagMergeForm = document.querySelector("#admin-tag-merge-form");
const adminTagMergeSource = document.querySelector("#admin-tag-merge-source");
const adminTagMergeTarget = document.querySelector("#admin-tag-merge-target");
const adminTagMergeSubmit = document.querySelector("#admin-tag-merge-submit");
const adminUsersPanel = document.querySelector("#admin-users-panel");
const adminUsersAddForm = document.querySelector("#admin-users-add-form");
const adminUsersAddID = document.querySelector("#admin-users-add-id");
const adminUsersStatus = document.querySelector("#admin-users-status");
const adminUsersList = document.querySelector("#admin-users-list");
const adminBackupPanel = document.querySelector("#admin-backup-panel");
const adminBackupExport = document.querySelector("#admin-backup-export");
const adminBackupDownload = document.querySelector("#admin-backup-download");
const adminBackupImport = document.querySelector("#admin-backup-import");
const adminBackupFile = document.querySelector("#admin-backup-file");
const adminBackupStatus = document.querySelector("#admin-backup-status");
const adminViewTable = document.querySelector("#admin-view-table");
const adminPagination = document.querySelector("#admin-pagination");
const adminPagePrev = document.querySelector("#admin-page-prev");
const adminPageNext = document.querySelector("#admin-page-next");
const adminPageLabel = document.querySelector("#admin-page-label");
const toastRegion = document.querySelector("#toast-region");
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
const sidebarPopularTagsSection = document.querySelector("#sidebar-popular-tags-section");
const sidebarPopularTags = document.querySelector("#sidebar-popular-tags");
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
const modalMobileSummary = document.querySelector("#modal-mobile-summary");
const modalMobileTitle = document.querySelector("#modal-mobile-title");
const modalMobileMeta = document.querySelector("#modal-mobile-meta");
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
const modalAITagTools = document.querySelector("#modal-ai-tag-tools");
const modalSuggestTagsButton = document.querySelector("#modal-suggest-tags");
const modalDismissAllSuggestedTagsButton = document.querySelector("#modal-dismiss-all-suggested-tags");
const modalSuggestTagsStatus = document.querySelector("#modal-suggest-tags-status");
const modalAITagSuggestions = document.querySelector("#modal-ai-tag-suggestions");
const modalNotesInput = document.querySelector("#modal-notes-input");
const modalSourceField = document.querySelector("#modal-source-field");
const modalSourceLink = document.querySelector("#modal-source-link");
const modalShare = document.querySelector("#modal-share");
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
const randomReelShare = document.querySelector("#random-reel-share");
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
let adminTagReviewSessionActive = false;
let adminTagReviewAdvancing = false;
let modalLLMTagSuggestionLoading = false;
let modalTagState = [];
let modalSuggestionState = [];
let activeSuggestionIndex = -1;
let tagSuggestionAbortController = null;
let modalTagSequence = 0;
let uploadTagState = [];
let uploadSuggestionState = [];
let activeUploadSuggestionIndex = -1;
let uploadTagSuggestionAbortController = null;
let linkUploadTagState = [];
let linkUploadSuggestionState = [];
let activeLinkUploadSuggestionIndex = -1;
let linkUploadTagSuggestionAbortController = null;
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
let randomReelPreloadCache = new Map();
let memeGridObserver = null;
let memeGridRenderFrame = null;
let memePageFetchSequence = 0;
let memePendingPageIndex = 0;
let managedUsersState = [];
let deleteQueueState = [];
let auditLogState = [];
let adminTagQueuePollInterval = null;
let adminLinkQueuePollInterval = null;
let adminBackupPollInterval = null;
let adminBackupImportBusy = false;
let toastSequence = 0;
let deferredInstallPrompt = null;
const activeToastTimeouts = new Map();
const ADMIN_TAG_HYGIENE_DISMISSED_KEY = "memeindex.adminTagHygieneDismissed";

function getActiveAdminPageState() {
  if (state.filters.view !== "admin") return null;
  if (state.admin.tab === "audit-logs") return state.admin.audit;
  if (state.admin.tab === "delete-queue") return state.admin.queue;
  return null;
}

function setAdminViewStatus(message = "") {
  if (!adminViewStatus) {
    return;
  }
  adminViewStatus.textContent = message;
  adminViewStatus.classList.toggle("hidden", !String(message || "").trim());
}

function showToast(message, type = "info", options = {}) {
  if (!toastRegion || !message) {
    return 0;
  }

  const toastType = ["success", "error", "info"].includes(type) ? type : "info";
  const title = options.title || (
    toastType === "success"
      ? "Success"
      : toastType === "error"
        ? "Problem"
        : "Heads Up"
  );
  const duration = Number.isFinite(options.duration) ? options.duration : (toastType === "error" ? 4200 : 2600);
  const toastID = ++toastSequence;
  const toast = document.createElement("article");
  toast.className = `toast toast-${toastType}`;
  toast.dataset.toastId = String(toastID);
  toast.setAttribute("role", toastType === "error" ? "alert" : "status");
  const icon = toastType === "success" ? "&#10003;" : toastType === "error" ? "!" : "i";
  toast.innerHTML = `
    <span class="toast-icon" aria-hidden="true">${icon}</span>
    <div class="toast-copy"><strong class="toast-title">${escapeHTML(title)}</strong><span class="toast-message">${escapeHTML(message)}</span></div>
    <button class="toast-dismiss" type="button" aria-label="Dismiss notification">&#10005;</button>
  `;
  toast.querySelector(".toast-dismiss")?.addEventListener("click", () => dismissToast(toastID));
  toastRegion.appendChild(toast);

  window.requestAnimationFrame(() => {
    toast.classList.add("is-visible");
  });

  const timeoutID = window.setTimeout(() => {
    dismissToast(toastID);
  }, Math.max(900, duration));
  activeToastTimeouts.set(toastID, timeoutID);
  return toastID;
}

function dismissToast(toastID) {
  const selector = `[data-toast-id="${String(toastID)}"]`;
  const toast = toastRegion?.querySelector(selector);
  const timeoutID = activeToastTimeouts.get(toastID);
  if (timeoutID) {
    window.clearTimeout(timeoutID);
    activeToastTimeouts.delete(toastID);
  }
  if (!toast) {
    return;
  }

  toast.classList.remove("is-visible");
  window.setTimeout(() => {
    toast.remove();
  }, 180);
}

function getDismissedTagHygienePairs() {
  try {
    const raw = window.localStorage.getItem(ADMIN_TAG_HYGIENE_DISMISSED_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter(Boolean) : [];
  } catch (error) {
    return [];
  }
}

function setDismissedTagHygienePairs(values) {
  try {
    window.localStorage.setItem(ADMIN_TAG_HYGIENE_DISMISSED_KEY, JSON.stringify([...new Set(values.filter(Boolean))]));
  } catch (error) {
    console.error(error);
  }
}

function tagHygienePairKey(primary, candidate) {
  return `${normalizeTagValue(primary)}::${normalizeTagValue(candidate)}`;
}
const drawerMediaQuery = window.matchMedia("(max-width: 1100px)");
const modalDetailsDrawerMediaQuery = window.matchMedia("(max-width: 1100px)");
const mobileSearchHeaderMediaQuery = window.matchMedia("(max-width: 760px)");
const MEME_PAGE_SIZE = 100;
const HOME_DASHBOARD_REFRESH_MS = 10000;
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

function isStandaloneApp() {
  return window.matchMedia("(display-mode: standalone)").matches || window.navigator.standalone === true;
}

function isIOSDevice() {
  const userAgent = window.navigator.userAgent || "";
  return /iphone|ipad|ipod/i.test(userAgent) || (window.navigator.platform === "MacIntel" && window.navigator.maxTouchPoints > 1);
}

function syncInstallAction() {
  if (!authInstall) return;
  const canOfferInstall = !isStandaloneApp() && (!!deferredInstallPrompt || isIOSDevice());
  authInstall.classList.toggle("hidden", !canOfferInstall);
}

async function installMemeIndex() {
  closeAuthMenu();
  if (isStandaloneApp()) {
    syncInstallAction();
    return;
  }

  if (deferredInstallPrompt) {
    deferredInstallPrompt.prompt();
    const choice = await deferredInstallPrompt.userChoice;
    deferredInstallPrompt = null;
    syncInstallAction();
    if (choice.outcome === "accepted") {
      showToast("MemeIndex was added to your home screen.", "success", { title: "App Installed", duration: 4200 });
    }
    return;
  }

  if (isIOSDevice()) {
    showToast("In Safari, tap the Share button, choose Add to Home Screen, then turn on Open as Web App.", "info", { title: "Install MemeIndex", duration: 9000 });
    return;
  }

  showToast("Open your browser menu and choose Install app or Add to Home screen.", "info", { title: "Install MemeIndex", duration: 7000 });
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

function activeAdminTab() {
  return state.admin.tab || "dashboard";
}

function isAdminView() {
  return state.filters.view === "admin";
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
  authAdmin?.classList.toggle("hidden", !canManageUsers());
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

function redirectToForbidden() {
  window.location.href = "/forbidden";
}

async function redirectToForbiddenIfViewAccessWasRevoked() {
  try {
    const response = await fetch("/api/auth/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (response.status === 401) {
      window.location.href = "/auth/login";
      return true;
    }
    if (!response.ok) {
      return false;
    }

    const payload = await response.json();
    const permissions = payload.permissions || {};
    if (payload.enabled && payload.authenticated !== false && !permissions.canView) {
      redirectToForbidden();
      return true;
    }
  } catch (error) {
    console.error(error);
  }

  return false;
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

  if (state.auth.enabled && state.auth.authenticated && !state.auth.permissions?.canView) {
    redirectToForbidden();
    return;
  }

  renderAuthState();
}

async function expectAuthorized(response, failureMessage) {
  if (response.status === 401) {
    window.location.href = "/auth/login";
    return false;
  }

  if (response.status === 403) {
    if (await redirectToForbiddenIfViewAccessWasRevoked()) {
      return false;
    }
    if (!state.auth.permissions?.canView) {
      redirectToForbidden();
      return false;
    }
    window.alert("You do not have permission to do that.");
    return false;
  }

  if (!response.ok) {
    window.alert(failureMessage);
    return false;
  }

  return true;
}

async function readAPIErrorMessage(response, fallbackMessage) {
  const contentType = response.headers.get("content-type") || "";
  const body = (await response.text()).trim();
  if (!body) {
    return fallbackMessage;
  }

  const bodyLower = body.toLowerCase();
  if (contentType.includes("text/html") || bodyLower.includes("<html")) {
    if (bodyLower.includes("cloudflare") && bodyLower.includes("502")) {
      return "Cloudflare returned a 502 while the link was being processed. The app or tunnel lost the origin request. Check the app and cloudflared logs, then try again.";
    }
    if (bodyLower.includes("cloudflare") && bodyLower.includes("524")) {
      return "Cloudflare timed out waiting for the server to finish processing this request. Try a smaller/faster link or run the request through the direct host instead of the tunnel.";
    }
    return fallbackMessage;
  }

  return body;
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

function renderAdminBackupStatus() {
  if (!adminBackupStatus || !adminBackupExport || !adminBackupImport) return;

  const status = state.admin.backupStatus;
  const running = status?.state === "running";
  adminBackupExport.disabled = running || adminBackupImportBusy;
  adminBackupExport.textContent = running ? "Backup Running..." : "Create Backup";
  adminBackupImport.disabled = running || adminBackupImportBusy;
  adminBackupDownload?.classList.toggle("hidden", !status?.download_available);
  if (adminBackupDownload && status?.filename) {
    adminBackupDownload.setAttribute("download", status.filename);
  }

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
    const started = status.started_at ? ` Started ${formatDateTime(status.started_at)}.` : "";
    adminBackupStatus.textContent = `The server is building a backup.${started} You can leave this page and return later.${availableSuffix}`;
    return;
  }
  if (status.state === "ready" && status.download_available) {
    const details = [status.filename || "Backup ready"];
    if (status.size_bytes) details.push(formatSize(Number(status.size_bytes)));
    if (status.completed_at) details.push(`completed ${formatDateTime(status.completed_at)}`);
    adminBackupStatus.textContent = `${details.join(" · ")}. This file remains available until a newer backup completes.`;
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
  adminBackupStatus.textContent = "No server backup has been created yet.";
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
  renderAdminBackupStatus();
  return state.admin.backupStatus;
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
  if (adminBackupPollInterval) {
    window.clearInterval(adminBackupPollInterval);
    adminBackupPollInterval = null;
  }
  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "backup";
  if (!visible) return;

  adminBackupPollInterval = window.setInterval(() => {
    fetchAdminBackupStatus().catch((error) => {
      console.error(error);
    });
  }, 3000);
}

async function importPortableBackup(file) {
  if (!file || !adminBackupImport) return;
  const confirmed = window.confirm(
    `Import ${file.name}?\n\nThis replaces every meme and all database data on this server. This cannot be undone unless you export the current server first.`
  );
  if (!confirmed) {
    adminBackupFile.value = "";
    return;
  }

  adminBackupImportBusy = true;
  renderAdminBackupStatus();
  adminBackupStatus.textContent = "Importing backup. Keep this page open; large libraries can take several minutes...";
  try {
    const response = await fetch("/api/admin/backup/import", {
      method: "POST",
      headers: { "Content-Type": "application/gzip" },
      body: file,
    });
    if (!(await expectAuthorized(response, "Failed to import backup."))) {
      adminBackupStatus.textContent = await readAPIErrorMessage(response, "Failed to import backup.");
      return;
    }
    adminBackupStatus.textContent = "Backup imported successfully. Reloading the restored library...";
    showToast("Backup imported successfully.", "success", { title: "Backup & Restore" });
    window.setTimeout(forceFreshHTMLReload, 900);
  } finally {
    adminBackupImportBusy = false;
    adminBackupFile.value = "";
    await fetchAdminBackupStatus();
  }
}

async function fetchAdminTagHygiene() {
  const response = await fetch("/api/admin/tag-hygiene");
  if (!(await expectAuthorized(response, "Failed to load tag hygiene tools."))) {
    return null;
  }

  const payload = await response.json();
  state.admin.tagHygiene = payload || null;
  renderAdminTagHygiene();
  return state.admin.tagHygiene;
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
    && !permissions.canManageUsers;
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

function formatRelativeQueueState(value) {
  switch (value) {
    case "waiting_for_ollama":
      return "Waiting for Ollama";
    case "processing":
      return "Processing";
    case "retrying":
      return "Retrying";
    case "idle":
      return "Idle";
    case "disabled":
      return "Disabled";
    case "starting":
      return "Starting";
    default:
      return value ? String(value) : "Unknown";
  }
}

function formatQueueError(value) {
  const message = String(value || "").trim();
  if (!message) {
    return "No recent worker error";
  }

  return message
    .replace(/^tag suggestion service is unavailable:\s*/i, "")
    .replace(/^ERROR:\s*/i, "")
    .replace(/\s+/g, " ")
    .trim();
}

function formatDurationWords(totalSeconds) {
  const seconds = Number(totalSeconds || 0);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return "Unknown";
  }
  if (seconds % 3600 === 0) {
    const hours = seconds / 3600;
    return `${hours} hour${hours === 1 ? "" : "s"}`;
  }
  if (seconds % 60 === 0) {
    const minutes = seconds / 60;
    return `${minutes} minute${minutes === 1 ? "" : "s"}`;
  }
  return `${seconds} second${seconds === 1 ? "" : "s"}`;
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
      </div>
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

function renderAdminTagQueueStatus() {
  if (!adminTagQueuePanel || !adminTagQueueGrid || !adminTagQueueSummary || !adminTagQueueList) {
    return;
  }

  const tab = activeAdminTab();
  const isQueueTab = tab === "tag-queue";
  const isReviewTab = tab === "tag-review";
  const visible = isAdminView() && canManageUsers() && (isQueueTab || isReviewTab);
  adminTagQueuePanel.classList.toggle("hidden", !visible);
  adminTagQueueReset?.classList.toggle("hidden", !isQueueTab);
  if (adminTagQueueReset) {
    adminTagQueueReset.disabled = !isQueueTab;
  }
  if (!visible) {
    return;
  }

  if (adminTagQueueKicker) {
    adminTagQueueKicker.textContent = isReviewTab ? "Suggested Tags" : "Tag Queue";
  }
  if (adminTagQueueTitle) {
    adminTagQueueTitle.textContent = isReviewTab ? "Pending Review" : "Suggestion Worker";
  }

  const status = state.admin.tagQueueStatus;
  if (!status) {
    adminTagQueueSummary.textContent = isReviewTab ? "Loading pending suggestions..." : "Loading queue status...";
    adminTagQueueGrid.innerHTML = `<p class="users-empty">${isReviewTab ? "Loading suggested-tag review backlog..." : "Loading tag suggestion worker status..."}</p>`;
    adminTagQueueList.innerHTML = "";
    return;
  }

  const summaryParts = [];
  if (isReviewTab) {
    summaryParts.push(`${String(status.pending_suggestion_memes || 0)} pending`);
  } else {
    summaryParts.push(status.enabled ? "Suggestions enabled" : "Suggestions disabled");
  }
  if (!isReviewTab && status.model) {
    summaryParts.push(status.model);
  }
  adminTagQueueSummary.textContent = summaryParts.join(" * ");

  const currentTarget = status.current_meme_name || status.current_meme_id || "Nothing right now";
  const lastSuccessDate = new Date(status.last_success_at || "");
  const lastSuccess = !Number.isNaN(lastSuccessDate.getTime()) && lastSuccessDate.getUTCFullYear() > 1
    ? formatDateTime(lastSuccessDate)
    : "No completed suggestions yet";
  const lastError = formatQueueError(status.last_error);

  adminTagQueueGrid.innerHTML = `
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Worker</span>
      <strong class="admin-tag-queue-value">${escapeHTML(formatRelativeQueueState(status.ollama_state || ""))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Queued</span>
      <strong class="admin-tag-queue-value">${escapeHTML(String(status.queue_length || 0))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Backlog</span>
      <strong class="admin-tag-queue-value">${escapeHTML(String(status.untagged_without_suggestions || 0))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Pending Review</span>
      <strong class="admin-tag-queue-value">${escapeHTML(String(status.pending_suggestion_memes || 0))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Ollama</span>
      <strong class="admin-tag-queue-value">${escapeHTML(status.ollama_ready ? "Ready" : "Not Ready")}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Current Meme</span>
      <strong class="admin-tag-queue-value">${escapeHTML(currentTarget)}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Last Success</span>
      <strong class="admin-tag-queue-value">${escapeHTML(lastSuccess)}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Last Error</span>
      <strong class="admin-tag-queue-value">${escapeHTML(lastError)}</strong>
    </article>
  `;
  adminTagQueueGrid.classList.toggle("hidden", isReviewTab);

  const queuedMemes = Array.isArray(status.queued_memes) ? status.queued_memes : [];
  const pendingReviewMemes = Array.isArray(status.pending_review_memes) ? status.pending_review_memes : [];
  const pendingReviewTotal = Number(status.pending_suggestion_memes || 0);
  const pendingReviewOffset = Number(status.pending_review_offset || 0);
  const pendingReviewLimit = Math.max(1, Number(status.pending_review_limit || state.admin.tagReview.limit || 50));
  const pendingReviewStart = pendingReviewTotal > 0 ? pendingReviewOffset + 1 : 0;
  const pendingReviewEnd = pendingReviewOffset + pendingReviewMemes.length;
  const pendingReviewPage = Math.floor(pendingReviewOffset / pendingReviewLimit) + 1;
  const pendingReviewPages = Math.max(1, Math.ceil(pendingReviewTotal / pendingReviewLimit));
  const queueSection = queuedMemes.length === 0
    ? `
      <section class="admin-tag-queue-section">
        <div class="admin-tag-queue-list-head">
          <strong>Queue Contents</strong>
          <span>No items are waiting right now.</span>
        </div>
      </section>
    `
    : `
      <section class="admin-tag-queue-section">
        <div class="admin-tag-queue-list-head">
          <strong>Queue Contents</strong>
          <span>${escapeHTML(String(queuedMemes.length))} item${queuedMemes.length === 1 ? "" : "s"} waiting</span>
        </div>
        <div class="admin-tag-queue-items">
          ${queuedMemes.map((item, index) => `
            <article class="admin-tag-queue-item">
              <span class="admin-tag-queue-rank">#${index + 1}</span>
              <div class="admin-tag-queue-item-copy">
                <strong>${escapeHTML(item.name || item.id || "Unknown meme")}</strong>
                <code>${escapeHTML(item.id || "")}</code>
              </div>
            </article>
          `).join("")}
        </div>
      </section>
    `;

  const pendingSection = pendingReviewMemes.length === 0
    ? `
      <section class="admin-tag-queue-section">
        <div class="admin-tag-queue-list-head">
          <strong>Pending Review</strong>
          <span>No memes are waiting on suggestion review.</span>
        </div>
      </section>
    `
    : `
      <section class="admin-tag-queue-section">
        <div class="admin-tag-queue-list-head">
          <strong>Pending Review</strong>
          <span>Showing ${escapeHTML(String(pendingReviewStart))}-${escapeHTML(String(pendingReviewEnd))} of ${escapeHTML(String(pendingReviewTotal))}</span>
        </div>
        <div class="admin-tag-queue-items">
          ${pendingReviewMemes.map((item) => `
            <article class="admin-tag-queue-item admin-tag-review-item" data-admin-review-id="${escapeHTML(item.id || "")}" role="button" tabindex="0">
              <span class="admin-tag-queue-rank">Review</span>
              <div class="admin-tag-queue-item-copy">
                <strong>${escapeHTML(item.name || item.id || "Unknown meme")}</strong>
                <code>${escapeHTML(item.id || "")}</code>
                <div class="admin-tag-review-tags">
                  ${(Array.isArray(item.suggested_tags) ? item.suggested_tags : []).map((tag) => `
                    <span class="tag-chip">${escapeHTML(tag)}</span>
                  `).join("")}
                </div>
              </div>
            </article>
          `).join("")}
        </div>
        <nav class="admin-tag-review-pagination" aria-label="Suggested tag review pages">
          <button type="button" class="admin-tag-page-button" data-admin-review-page="prev" ${pendingReviewOffset <= 0 ? "disabled" : ""}>Previous</button>
          <span>Page ${escapeHTML(String(pendingReviewPage))} of ${escapeHTML(String(pendingReviewPages))}</span>
          <button type="button" class="admin-tag-page-button" data-admin-review-page="next" ${status.pending_review_has_more ? "" : "disabled"}>Next</button>
        </nav>
      </section>
    `;

  adminTagQueueList.innerHTML = isReviewTab ? pendingSection : queueSection;
  adminTagQueueList.querySelectorAll("[data-admin-review-id]").forEach((element) => {
    const openReviewMeme = () => {
      const memeID = element.getAttribute("data-admin-review-id") || "";
      if (!memeID) {
        return;
      }
      adminTagReviewSessionActive = true;
      openAdminMemeByID(memeID).then((opened) => {
        if (!opened) {
          adminTagReviewSessionActive = false;
        }
      }).catch((error) => {
        adminTagReviewSessionActive = false;
        console.error(error);
      });
    };
    element.addEventListener("click", openReviewMeme);
    element.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        openReviewMeme();
      }
    });
  });
  adminTagQueueList.querySelectorAll("[data-admin-review-page]").forEach((button) => {
    button.addEventListener("click", () => {
      if (button.disabled) return;
      const direction = button.getAttribute("data-admin-review-page");
      const delta = direction === "prev" ? -state.admin.tagReview.limit : state.admin.tagReview.limit;
      state.admin.tagReview.offset = Math.max(0, state.admin.tagReview.offset + delta);
      fetchAdminTagQueueStatus().catch((error) => console.error(error));
    });
  });
}

async function fetchAdminShares() {
  const response = await fetch("/api/admin/shares", { cache: "no-store" });
  if (!(await expectAuthorized(response, "Failed to load shared memes."))) return null;
  const payload = await response.json();
  state.admin.shares = Array.isArray(payload.shares) ? payload.shares : [];
  renderAdminShares();
  return state.admin.shares;
}

function renderAdminShares() {
  if (!adminViewTable || activeAdminTab() !== "shares") return;
  const shares = Array.isArray(state.admin.shares) ? state.admin.shares : [];
  const layout = document.createElement("div");
  layout.className = "admin-shares-layout";
  if (shares.length) {
    const toolbar = document.createElement("div");
    toolbar.className = "admin-shares-toolbar";
    toolbar.innerHTML = `<div><strong>${shares.length.toLocaleString()} active share link${shares.length === 1 ? "" : "s"}</strong><span>Revoke individual links below or remove public access from all of them.</span></div><button class="danger-button shared-revoke-all-button" type="button">Revoke All</button>`;
    toolbar.querySelector(".shared-revoke-all-button")?.addEventListener("click", async (event) => {
      if (!window.confirm(`Revoke all ${shares.length} active share link${shares.length === 1 ? "" : "s"}?\n\nEvery existing public link will stop working immediately.`)) return;
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
      <div class="admin-table-cell shared-meme-expiry-cell" data-label="Expires"><span>${escapeHTML(formatDateTime(share.expires_at))}</span></div>
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
  adminViewTable.replaceChildren(layout);
}

function dashboardTrend(current, previous, suffix = "vs previous 30 days") {
  const currentValue = Number(current || 0);
  const previousValue = Number(previous || 0);
  if (currentValue === 0 && previousValue === 0) return { direction: "flat", label: `No change ${suffix}` };
  if (previousValue === 0) return { direction: "up", label: `New activity ${suffix}` };
  const percent = Math.round(((currentValue - previousValue) / previousValue) * 100);
  return {
    direction: percent > 0 ? "up" : percent < 0 ? "down" : "flat",
    label: `${percent > 0 ? "+" : ""}${percent}% ${suffix}`,
  };
}

function adminChartTooltipAttributes(label) {
  const text = escapeHTML(label);
  return `data-chart-tooltip="${text}" aria-label="${text}"`;
}

function buildAdminChartPoints(points, coordinates, width, height, valueLabel) {
  // Each day's target extends to the midpoint of its neighbors, including both endpoints.
  return points.map((entry, index) => {
    const [x, y] = coordinates[index];
    const left = index === 0 ? 0 : (coordinates[index - 1][0] + x) / 2;
    const right = index === points.length - 1 ? width : (x + coordinates[index + 1][0]) / 2;
    const label = `${entry.date || "Unknown date"} (UTC day)\n${valueLabel(entry, index)}`;
    return `<g class="admin-chart-point" tabindex="0" role="img" ${adminChartTooltipAttributes(label)}>
      <circle class="admin-chart-dot" cx="${x.toFixed(2)}" cy="${y.toFixed(2)}" r="2.6" />
      <rect class="admin-chart-hit" x="${left.toFixed(2)}" y="0" width="${(right - left).toFixed(2)}" height="${height}" />
    </g>`;
  }).join("");
}

function bindAdminChartTooltips(root) {
  let tooltip = document.querySelector("#admin-chart-tooltip");
  if (!tooltip) {
    tooltip = document.createElement("div");
    tooltip.id = "admin-chart-tooltip";
    tooltip.className = "admin-chart-tooltip";
    tooltip.setAttribute("role", "tooltip");
    document.body.append(tooltip);
    // Fixed positioning keeps tooltips clear of the metric cards' clipped edges.
    window.addEventListener("scroll", () => { tooltip.hidden = true; }, true);
    window.addEventListener("resize", () => { tooltip.hidden = true; });
  }
  tooltip.hidden = true;
  root.querySelectorAll("[data-chart-tooltip]").forEach((target) => {
    const show = (event) => {
      tooltip.textContent = target.dataset.chartTooltip;
      tooltip.hidden = false;
      const anchor = (target.querySelector("circle") || target).getBoundingClientRect();
      const x = event.type.startsWith("pointer") ? event.clientX : anchor.left + anchor.width / 2;
      const y = event.type.startsWith("pointer") ? event.clientY : anchor.top;
      const bounds = tooltip.getBoundingClientRect();
      tooltip.style.left = `${Math.max(8, Math.min(x - bounds.width / 2, window.innerWidth - bounds.width - 8))}px`;
      const top = y - bounds.height - 12;
      tooltip.style.top = `${Math.max(8, Math.min(top < 8 ? y + 16 : top, window.innerHeight - bounds.height - 8))}px`;
    };
    const hide = () => { tooltip.hidden = true; };
    target.addEventListener("pointerenter", show);
    target.addEventListener("pointermove", show);
    target.addEventListener("pointerleave", hide);
    target.addEventListener("focus", (event) => {
      // Focusing a point may scroll it into view; position after that scroll settles.
      requestAnimationFrame(() => { if (document.activeElement === target) show(event); });
    });
    target.addEventListener("blur", hide);
    target.addEventListener("click", hide);
    target.addEventListener("keydown", (event) => { if (event.key === "Escape") hide(); });
  });
}

function buildAdminMetricSparkline(series, key, label, valueFormatter = (value) => Number(value || 0).toLocaleString()) {
  const points = Array.isArray(series) ? series : [];
  if (!points.length) return "";
  const width = 240;
  const height = 44;
  const padding = 5;
  const values = points.map((entry) => Number(entry?.[key] || 0));
  const minimum = Math.min(...values);
  const maximum = Math.max(...values);
  const range = maximum - minimum;
  const coordinates = values.map((value, index) => {
    const x = points.length === 1 ? width / 2 : (index / (points.length - 1)) * width;
    const normalized = range === 0 ? .5 : (value - minimum) / range;
    const y = height - padding - (normalized * (height - (padding * 2)));
    return [x, y];
  });
  const line = coordinates.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `0,${height} ${line} ${width},${height}`;
  const hitAreas = buildAdminChartPoints(points, coordinates, width, height, (entry, index) => `${label}: ${valueFormatter(values[index])}`);
  const gradientID = `admin-metric-fill-${key}`;
  return `
    <svg class="admin-metric-spark" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="group" aria-label="${escapeHTML(label)} over the last 30 days">
      <defs><linearGradient id="${gradientID}" x1="0" y1="0" x2="0" y2="1"><stop class="admin-metric-spark-fill-start" offset="0"/><stop class="admin-metric-spark-fill-end" offset="1"/></linearGradient></defs>
      <polygon class="admin-metric-spark-area" points="${area}" fill="url(#${gradientID})" />
      <polyline class="admin-metric-spark-line" points="${line}" />
      ${hitAreas}
    </svg>
  `;
}

function buildAdminUploadChart(series) {
  const points = Array.isArray(series) ? series : [];
  if (!points.length) return `<p class="users-empty">Upload history will appear here.</p>`;
  const width = 720;
  const height = 205;
  const max = Math.max(1, ...points.map((entry) => Number(entry.uploads || 0)));
  const coordinates = points.map((entry, index) => {
    const x = points.length === 1 ? width / 2 : (index / (points.length - 1)) * width;
    const y = height - ((Number(entry.uploads || 0) / max) * (height - 18)) - 6;
    return [x, y];
  });
  const line = coordinates.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `0,${height} ${line} ${width},${height}`;
  const total = points.reduce((sum, entry) => sum + Number(entry.uploads || 0), 0);
  return `
    <div class="admin-chart-summary"><strong>${escapeHTML(String(total))}</strong><span>uploads in 30 days</span></div>
    <svg class="admin-upload-svg" viewBox="0 0 ${width} ${height}" role="group" aria-label="Daily uploads over the last 30 days">
      <defs><linearGradient id="admin-upload-fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#65ca73" stop-opacity=".42"/><stop offset="1" stop-color="#65ca73" stop-opacity="0"/></linearGradient></defs>
      <path class="admin-chart-grid" d="M0 35H720M0 85H720M0 135H720M0 185H720" />
      <polygon points="${area}" fill="url(#admin-upload-fill)" />
      <polyline points="${line}" fill="none" stroke="#72d17c" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" />
      ${buildAdminChartPoints(points, coordinates, width, height, (entry) => `Uploads: ${Number(entry.uploads || 0).toLocaleString()}`)}
    </svg>
    <div class="admin-chart-axis"><span>${escapeHTML(points[0]?.date || "")}</span><span>${escapeHTML(points[Math.floor(points.length / 2)]?.date || "")}</span><span>${escapeHTML(points[points.length - 1]?.date || "")}</span></div>
  `;
}

function buildAdminTopTags(tags) {
  const topTags = (Array.isArray(tags) ? tags : []).slice(0, 12);
  if (!topTags.length) return `<p class="users-empty">No tags have been used yet.</p>`;
  const colors = ["#47b76a", "#55a1f3", "#9b72e4", "#e1ad39", "#32c2c9", "#f08d46", "#e66791", "#79c95b", "#4f78d6", "#c869d4", "#d6ca4f", "#45a98f"];
  const segments = topTags.map((entry, index) => ({ label: entry.tag || "", count: Number(entry.count || 0), color: colors[index] }));
  const displayedAssignments = Math.max(1, segments.reduce((sum, entry) => sum + entry.count, 0));
  const tooltipLabel = (entry) => `${entry.label}\n${entry.count.toLocaleString()} tag assignments (${((entry.count / displayedAssignments) * 100).toFixed(1)}% of top tags)`;
  let angle = -Math.PI / 2;
  const position = (radius, radians) => `${(66 + radius * Math.cos(radians)).toFixed(4)},${(66 + radius * Math.sin(radians)).toFixed(4)}`;
  const slices = segments.filter((entry) => entry.count > 0).map((entry) => {
    const end = angle + (entry.count / displayedAssignments) * Math.PI * 2;
    const middle = (angle + end) / 2;
    // Two arcs per edge also handle a single tag occupying the entire ring.
    const path = `M${position(65, angle)} A65,65 0 0 1 ${position(65, middle)} A65,65 0 0 1 ${position(65, end)} L${position(37.5, end)} A37.5,37.5 0 0 0 ${position(37.5, middle)} A37.5,37.5 0 0 0 ${position(37.5, angle)} Z`;
    angle = end;
    return `<path class="admin-tag-slice" d="${path}" fill="${entry.color}" tabindex="0" role="img" ${adminChartTooltipAttributes(tooltipLabel(entry))} />`;
  }).join("");
  return `
    <div class="admin-category-donut">
      <svg viewBox="0 0 132 132" role="group" aria-label="Top tags by tag assignments">${slices}</svg>
    </div>
    <div class="admin-category-legend">${segments.map((entry) => `<button type="button" data-admin-tag="${escapeHTML(entry.label)}" ${adminChartTooltipAttributes(tooltipLabel(entry))}><i style="--tag-color:${entry.color}"></i><span class="tag-chip">${escapeHTML(entry.label)}</span><strong>${Math.round((entry.count / displayedAssignments) * 100)}%</strong><small>${entry.count.toLocaleString()}</small></button>`).join("")}</div>
  `;
}

function renderAdminDashboard() {
  if (!adminDashboardPanel || !adminDashboardGrid || !adminDashboardTags || !adminDashboardRecent || !adminDashboardUploadChart || !adminDashboardHealth || !adminDashboardActivity) return;

  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "dashboard";
  const tooltip = document.querySelector("#admin-chart-tooltip");
  if (tooltip) tooltip.hidden = true;
  adminDashboardPanel.classList.toggle("hidden", !visible);
  if (!visible) return;

  const dashboard = state.admin.dashboard;
  if (!dashboard) {
    adminDashboardGrid.innerHTML = `<p class="users-empty">Loading dashboard...</p>`;
    [adminDashboardTags, adminDashboardRecent, adminDashboardUploadChart, adminDashboardHealth, adminDashboardActivity].forEach((node) => { node.innerHTML = ""; });
    return;
  }

  const counts = dashboard.counts || {};
  const uploadTrend = dashboardTrend(dashboard.uploaded_last_30_days, dashboard.uploaded_previous_30_days);
  const storageTrend = dashboardTrend(dashboard.bytes_last_30_days, dashboard.bytes_previous_30_days);
  const metrics = [
    { icon: "M", label: "Total Memes", value: Number(counts.total || 0).toLocaleString(), note: uploadTrend.label, trend: uploadTrend.direction, accent: "green", seriesKey: "memes", seriesLabel: "Cumulative meme count" },
    { icon: "#", label: "Tags", value: Number(dashboard.unique_tags || 0).toLocaleString(), note: `${Number(dashboard.total_tag_assignments || 0).toLocaleString()} assignments`, trend: "flat", accent: "purple", seriesKey: "tags", seriesLabel: "Cumulative unique tag count" },
    { icon: "U", label: "Users", value: Number(dashboard.user_count || 0).toLocaleString(), note: `${Number(dashboard.active_users_30d || 0)} active in 30 days`, trend: Number(dashboard.new_users_30d || 0) > 0 ? "up" : "flat", accent: "blue", seriesKey: "users", seriesLabel: "Cumulative user count" },
    { icon: "S", label: "Storage Used", value: formatSize(Number(dashboard.total_size_bytes || 0)), note: storageTrend.label, trend: storageTrend.direction, accent: "amber", seriesKey: "storage_bytes", seriesLabel: "Cumulative storage used", valueFormatter: (value) => `${formatSize(value)} (${value.toLocaleString()} bytes)` },
    { icon: "F", label: "Favorites", value: Number(counts.favorites || 0).toLocaleString(), note: "Across all users", trend: "flat", accent: "red", seriesKey: "favorites", seriesLabel: "Cumulative favorite assignments" },
    { icon: "L", label: "Active Share Links", value: Number(dashboard.active_share_count || 0).toLocaleString(), note: "Public links available now", trend: "flat", accent: "cyan" },
  ];
  adminDashboardGrid.innerHTML = metrics.map((metric) => `
    <article class="admin-dashboard-card" data-accent="${metric.accent}">
      <div class="admin-dashboard-card-head"><span class="admin-dashboard-card-icon">${metric.icon}</span><span class="admin-dashboard-label">${escapeHTML(metric.label)}</span></div>
      <strong class="admin-dashboard-value">${escapeHTML(metric.value)}</strong>
      <span class="admin-dashboard-note" data-trend="${metric.trend}">${metric.trend === "up" ? "&#8593; " : metric.trend === "down" ? "&#8595; " : ""}${escapeHTML(metric.note)}</span>
      ${metric.seriesKey ? buildAdminMetricSparkline(dashboard.metric_series, metric.seriesKey, metric.seriesLabel, metric.valueFormatter) : ""}
    </article>
  `).join("");

  adminDashboardUploadChart.innerHTML = buildAdminUploadChart(dashboard.upload_series);

  adminDashboardTags.innerHTML = buildAdminTopTags(dashboard.top_tags);
  bindAdminChartTooltips(adminDashboardPanel);

  const health = Array.isArray(dashboard.system_health) ? dashboard.system_health : [];
  const healthyCount = health.filter((entry) => entry.healthy).length;
  adminDashboardHealth.innerHTML = `
    <div class="admin-health-summary" data-healthy="${healthyCount === health.length}"><strong>${healthyCount === health.length ? "All systems operational" : `${healthyCount} of ${health.length} healthy`}</strong><span>Updated ${escapeHTML(formatDateTime(dashboard.generated_at))}</span></div>
    <div class="admin-health-list">${health.map((entry) => `<div><span>${escapeHTML(entry.name || "Service")}</span><strong data-healthy="${!!entry.healthy}">${escapeHTML(entry.status || "Unknown")}</strong></div>`).join("")}</div>
  `;

  const activity = Array.isArray(dashboard.recent_activity) ? dashboard.recent_activity : [];
  const uploadActors = new Map(activity.filter((event) => event.action === "uploaded").map((event) => [event.meme_id, event.actor?.display_name || event.actor?.username || "Unknown"]));
  const recentMemes = Array.isArray(dashboard.recent_memes) ? dashboard.recent_memes : [];
  if (!recentMemes.length) {
    adminDashboardRecent.innerHTML = `<p class="users-empty">No memes uploaded yet.</p>`;
  } else {
    adminDashboardRecent.innerHTML = `<div class="admin-upload-table-head"><span>Meme</span><span>Uploaded by</span><span>Tags</span><span>Size</span><span>Uploaded</span></div>${recentMemes.map((meme) => `
      <button class="admin-dashboard-recent-row" type="button" data-admin-meme-id="${escapeHTML(meme.id || "")}" title="Open ${escapeHTML(meme.original_name || "Unknown meme")}">
        <span class="admin-recent-meme"><span class="admin-recent-thumb">${meme.preview_path ? `<img src="${escapeHTML(meme.preview_path)}" alt="" />` : "FILE"}</span><strong title="${escapeHTML(meme.original_name || "Unknown meme")}">${escapeHTML(meme.original_name || "Unknown meme")}</strong></span>
        <span>${escapeHTML(uploadActors.get(meme.id) || "Unknown")}</span>
        <span class="admin-recent-tags">${(meme.tags || []).slice(0, 2).map((tag) => `<span class="tag-chip">${escapeHTML(tag)}</span>`).join("") || "&mdash;"}</span>
        <span>${escapeHTML(formatSize(Number(meme.size_bytes || 0)))}</span>
        <span>${escapeHTML(formatDateTime(meme.created_at))}</span>
      </button>
    `).join("")}`;
  }

  if (!activity.length) {
    adminDashboardActivity.innerHTML = `<p class="users-empty">No tracked activity yet.</p>`;
  } else {
    const actionLabels = { uploaded: "uploaded", favorited: "favorited", unfavorited: "unfavorited", deleted: "deleted", delete_requested: "requested deletion of", tag_added: "tagged", tag_removed: "untagged" };
    adminDashboardActivity.innerHTML = activity.slice(0, 7).map((event) => `
      <button class="admin-activity-row" type="button" data-admin-meme-id="${escapeHTML(event.meme_id || "")}" title="${escapeHTML(event.meme_original_name || event.description || "Activity")}">
        <span class="admin-activity-icon" data-action="${escapeHTML(event.action || "activity")}"></span>
        <span><strong>${escapeHTML(event.actor?.display_name || event.actor?.username || "System")}</strong> ${escapeHTML(actionLabels[event.action] || event.description || "updated")} <b>${escapeHTML(event.meme_original_name || "a meme")}</b></span>
        <time>${escapeHTML(formatDateTime(event.created_at))}</time>
      </button>
    `).join("");
  }

  document.querySelectorAll("[data-admin-meme-id]").forEach((button) => button.addEventListener("click", () => openAdminMemeByID(button.dataset.adminMemeId)));
  document.querySelectorAll("[data-admin-tag]").forEach((button) => button.addEventListener("click", () => {
    state.filters.tag = button.dataset.adminTag || "";
    state.filters.view = "library";
    loadInitialMemes().catch((error) => console.error(error));
  }));
}

function renderAdminTagHygiene() {
  if (!adminTagHygienePanel || !adminTagHygienePairs || !adminTagHygieneTags) {
    return;
  }

  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "tag-hygiene";
  adminTagHygienePanel.classList.toggle("hidden", !visible);
  if (!visible) {
    return;
  }

  const report = state.admin.tagHygiene;
  if (!report) {
    adminTagHygienePairs.innerHTML = `<p class="users-empty">Loading tag hygiene suggestions...</p>`;
    adminTagHygieneTags.innerHTML = "";
    return;
  }

  const dismissedPairs = new Set(getDismissedTagHygienePairs());
  const pairs = (Array.isArray(report.pairs) ? report.pairs : []).filter((pair) => {
    return !dismissedPairs.has(tagHygienePairKey(pair.primary || "", pair.candidate || ""));
  });
  if (pairs.length === 0) {
    adminTagHygienePairs.innerHTML = `<p class="users-empty">No likely spelling or separator variants found right now.</p>`;
  } else {
    adminTagHygienePairs.innerHTML = pairs.map((pair) => `
      <article class="admin-tag-hygiene-pair">
        <div class="admin-tag-hygiene-pair-copy">
          <strong class="tag-chip">${escapeHTML(pair.candidate || "")}</strong>
          <span>suggested merge into</span>
          <strong class="tag-chip">${escapeHTML(pair.primary || "")}</strong>
        </div>
        <div class="admin-tag-hygiene-pair-actions">
          <button
            class="ghost-button admin-tag-hygiene-dismiss-button"
            type="button"
            data-dismiss-primary="${escapeHTML(pair.primary || "")}"
            data-dismiss-candidate="${escapeHTML(pair.candidate || "")}"
          >
            Dismiss
          </button>
          <button
            class="ghost-button admin-tag-hygiene-merge-button"
            type="button"
            data-source-tag="${escapeHTML(pair.candidate || "")}"
            data-target-tag="${escapeHTML(pair.primary || "")}"
          >
            Merge
          </button>
        </div>
      </article>
    `).join("");
  }

  const tags = Array.isArray(report.tags) ? report.tags : [];
  if (tags.length === 0) {
    adminTagHygieneTags.innerHTML = `<p class="users-empty">No tags found yet.</p>`;
  } else {
    adminTagHygieneTags.innerHTML = tags.map((tag) => `
      <article class="admin-tag-hygiene-tag">
        <div class="admin-tag-hygiene-tag-copy">
          <strong class="tag-chip">${escapeHTML(tag.tag || "")}</strong>
          <span>${escapeHTML(String(tag.count || 0))} meme${Number(tag.count || 0) === 1 ? "" : "s"}</span>
        </div>
        <div class="admin-tag-hygiene-similar">
          ${(Array.isArray(tag.similar) ? tag.similar : []).map((similar) => `
            <button
              class="admin-tag-hygiene-chip"
              type="button"
              data-source-tag="${escapeHTML(similar)}"
              data-target-tag="${escapeHTML(tag.tag || "")}"
            >
              ${escapeHTML(similar)}
            </button>
          `).join("")}
        </div>
      </article>
    `).join("");
  }

  adminTagHygienePanel.querySelectorAll("[data-source-tag][data-target-tag]").forEach((element) => {
    element.addEventListener("click", () => {
      const sourceTag = element.getAttribute("data-source-tag") || "";
      const targetTag = element.getAttribute("data-target-tag") || "";
      if (adminTagMergeSource) {
        adminTagMergeSource.value = sourceTag;
      }
      if (adminTagMergeTarget) {
        adminTagMergeTarget.value = targetTag;
      }
    });
  });

  adminTagHygienePanel.querySelectorAll("[data-dismiss-primary][data-dismiss-candidate]").forEach((element) => {
    element.addEventListener("click", () => {
      const primary = element.getAttribute("data-dismiss-primary") || "";
      const candidate = element.getAttribute("data-dismiss-candidate") || "";
      dismissTagHygienePair(primary, candidate);
    });
  });
}

function dismissTagHygienePair(primary, candidate) {
  const pairKey = tagHygienePairKey(primary, candidate);
  if (!pairKey || pairKey === "::") {
    return;
  }
  const dismissed = getDismissedTagHygienePairs();
  dismissed.push(pairKey);
  setDismissedTagHygienePairs(dismissed);
  renderAdminTagHygiene();
  showToast(`Dismissed ${candidate} -> ${primary}.`, "info", { title: "Tag Hygiene", duration: 2600 });
}

async function mergeAdminTags(sourceTag, targetTag) {
  const normalizedSource = normalizeTagValue(sourceTag);
  const normalizedTarget = normalizeTagValue(targetTag);
  if (!normalizedSource || !normalizedTarget) {
    setAdminViewStatus("Both source and target tags are required.");
    showToast("Both source and target tags are required.", "error");
    return;
  }

  if (adminTagMergeSubmit) {
    adminTagMergeSubmit.disabled = true;
  }
  setAdminViewStatus("");
  showToast(`Merging ${normalizedSource} into ${normalizedTarget}...`, "info", { title: "Tag Hygiene", duration: 1800 });

  const response = await fetch("/api/admin/tag-hygiene", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      source_tag: normalizedSource,
      target_tag: normalizedTarget,
    }),
  });
  if (!(await expectAuthorized(response, "Failed to merge tags."))) {
    setAdminViewStatus("");
    showToast("Could not merge tags.", "error", { title: "Tag Hygiene" });
    if (adminTagMergeSubmit) {
      adminTagMergeSubmit.disabled = false;
    }
    return;
  }

  const payload = await response.json();
  setAdminViewStatus("");
  showToast(`Merged ${payload.source_tag || normalizedSource} into ${payload.target_tag || normalizedTarget} across ${Number(payload.affected_memes || 0)} meme${Number(payload.affected_memes || 0) === 1 ? "" : "s"}.`, "success", { title: "Tag Hygiene", duration: 3400 });
  if (adminTagMergeSource) {
    adminTagMergeSource.value = "";
  }
  if (adminTagMergeTarget) {
    adminTagMergeTarget.value = "";
  }
  await fetchAdminTagHygiene();
  await fetchAdminDashboard();
  if (adminTagMergeSubmit) {
    adminTagMergeSubmit.disabled = false;
  }
}

async function fetchAdminTagQueueStatus() {
  if (!canManageUsers()) {
    return null;
  }

  const reviewParams = new URLSearchParams({
    review_offset: String(state.admin.tagReview.offset || 0),
    review_limit: String(state.admin.tagReview.limit || 50),
  });
  const response = await fetch(`/api/admin/tag-suggestions/status?${reviewParams.toString()}`);
  if (!(await expectAuthorized(response, "Failed to load tag suggestion queue status."))) {
    return null;
  }

  const payload = await response.json();
  const reviewTotal = Number(payload?.pending_suggestion_memes || 0);
  const reviewLimit = Math.max(1, Number(payload?.pending_review_limit || state.admin.tagReview.limit || 50));
  const reviewOffset = Math.max(0, Number(payload?.pending_review_offset || 0));
  if (reviewTotal > 0 && reviewOffset >= reviewTotal) {
    state.admin.tagReview.limit = reviewLimit;
    state.admin.tagReview.offset = Math.floor((reviewTotal - 1) / reviewLimit) * reviewLimit;
    return fetchAdminTagQueueStatus();
  }
  state.admin.tagReview.offset = reviewOffset;
  state.admin.tagReview.limit = reviewLimit;
  state.admin.tagReview.total = reviewTotal;
  state.admin.tagReview.hasMore = !!payload?.pending_review_has_more;
  state.admin.tagQueueStatus = payload || null;
  renderAdminTagQueueStatus();
  return state.admin.tagQueueStatus;
}

async function fetchAdminLinkRetryStatus() {
  if (!canManageUsers()) {
    return null;
  }

  const response = await fetch("/api/admin/link-downloads/status");
  if (!(await expectAuthorized(response, "Failed to load link retry queue status."))) {
    return null;
  }

  const payload = await response.json();
  state.admin.linkRetryStatus = payload || null;
  renderAdminLinkRetryStatus();
  return state.admin.linkRetryStatus;
}

async function resetAdminTagSuggestions() {
  if (!canManageUsers()) {
    return;
  }

  const confirmed = window.confirm("Clear all pending tag suggestions and requeue every untagged meme?");
  if (!confirmed) {
    return;
  }

  if (adminTagQueueReset) {
    adminTagQueueReset.disabled = true;
  }
  setAdminViewStatus("");
  showToast("Resetting tag suggestions and reseeding the queue...", "info", { title: "Tag Queue", duration: 1800 });

  const response = await fetch("/api/admin/tag-suggestions/reset", {
    method: "POST",
  });
  if (!(await expectAuthorized(response, "Failed to reset tag suggestions."))) {
    setAdminViewStatus("");
    showToast("Could not reset tag suggestions.", "error", { title: "Tag Queue" });
    if (adminTagQueueReset) {
      adminTagQueueReset.disabled = false;
    }
    return;
  }

  const payload = await response.json();
  const cleared = Number(payload?.cleared_suggestions || 0);
  const queued = Number(payload?.queued_untagged || 0);
  setAdminViewStatus("");
  showToast(`Cleared ${cleared} suggestion set${cleared === 1 ? "" : "s"} and queued ${queued} untagged meme${queued === 1 ? "" : "s"}.`, "success", { title: "Tag Queue", duration: 3400 });
  await fetchAdminTagQueueStatus();
  if (adminTagQueueReset) {
    adminTagQueueReset.disabled = false;
  }
}

function syncAdminTagQueuePolling() {
  if (adminTagQueuePollInterval) {
    window.clearInterval(adminTagQueuePollInterval);
    adminTagQueuePollInterval = null;
  }

  if (!canManageUsers() || !isAdminView()) {
    renderAdminTagQueueStatus();
    return;
  }

  adminTagQueuePollInterval = window.setInterval(() => {
    fetchAdminTagQueueStatus().catch((error) => {
      console.error(error);
    });
  }, 8000);
}

function renderAdminLinkRetryStatus() {
  if (!adminLinkQueuePanel || !adminLinkQueueGrid || !adminLinkQueueSummary || !adminLinkQueueList) {
    return;
  }

  const tab = activeAdminTab();
  const isRetryTab = tab === "link-retries";
  const isRejectedTab = tab === "rejected-links";
  const visible = isAdminView() && canManageUsers() && (isRetryTab || isRejectedTab);
  adminLinkQueuePanel.classList.toggle("hidden", !visible);
  if (!visible) {
    return;
  }

  if (adminLinkQueueKicker) {
    adminLinkQueueKicker.textContent = isRejectedTab ? "Rejected Links" : "Link Retries";
  }
  if (adminLinkQueueTitle) {
    adminLinkQueueTitle.textContent = isRejectedTab ? "Rejected Import Queue" : "Retry Queue";
  }

  const status = state.admin.linkRetryStatus;
  if (!status) {
    adminLinkQueueSummary.textContent = isRejectedTab ? "Loading rejected links..." : "Loading retry queue...";
    adminLinkQueueGrid.innerHTML = `<p class="users-empty">${isRejectedTab ? "Loading rejected link downloads..." : "Loading failed link retries..."}</p>`;
    adminLinkQueueList.innerHTML = "";
    return;
  }

  const queued = Array.isArray(status.queued) ? status.queued : [];
  const rejected = Array.isArray(status.rejected) ? status.rejected : [];
  adminLinkQueueSummary.textContent = isRejectedTab
    ? `${rejected.length} rejected * ${status.max_attempts || 0} max attempts`
    : `${queued.length} queued * retry every ${status.retry_interval_seconds || 0}s`;

  adminLinkQueueGrid.innerHTML = `
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Queued</span>
      <strong class="admin-tag-queue-value">${escapeHTML(String(queued.length))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Rejected</span>
      <strong class="admin-tag-queue-value">${escapeHTML(String(rejected.length))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Retry Interval</span>
      <strong class="admin-tag-queue-value">${escapeHTML(formatDurationWords(Number(status.retry_interval_seconds || 0)))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Max Attempts</span>
      <strong class="admin-tag-queue-value">${escapeHTML(String(status.max_attempts || 0))}</strong>
    </article>
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">Processing</span>
      <strong class="admin-tag-queue-value">${escapeHTML(status.processing_id ? "Active" : "Idle")}</strong>
    </article>
  `;

  const items = isRejectedTab ? rejected : queued;
  if (items.length === 0) {
    adminLinkQueueList.innerHTML = `
      <section class="admin-tag-queue-section">
        <div class="admin-tag-queue-list-head">
          <strong>${isRejectedTab ? "Rejected Links" : "Retry Queue"}</strong>
          <span>${isRejectedTab ? "Nothing is rejected right now." : "No failed links are waiting right now."}</span>
        </div>
      </section>
    `;
    return;
  }

  adminLinkQueueList.innerHTML = `
    <section class="admin-tag-queue-section">
      <div class="admin-tag-queue-list-head">
        <strong>${isRejectedTab ? "Rejected Links" : "Retry Queue"}</strong>
        <span>${escapeHTML(String(items.length))} item${items.length === 1 ? "" : "s"}</span>
      </div>
      <div class="admin-tag-queue-items">
        ${items.map((item, index) => `
          <article class="admin-tag-queue-item admin-link-queue-item">
            <span class="admin-tag-queue-rank">${isRejectedTab ? "Rejected" : `#${index + 1}`}</span>
            <div class="admin-tag-queue-item-copy">
              <strong>${escapeHTML(item.source_url || "Unknown link")}</strong>
              <span>${escapeHTML(String(item.attempts || 0))}/${escapeHTML(String(item.max_attempts || 0))} attempts * ${escapeHTML(isRejectedTab ? formatDateTime(item.requested_at) : formatDateTime(item.next_attempt_at))}</span>
              ${item.tags?.length ? `<div class="admin-tag-review-tags">${item.tags.map((tag) => `<span class="tag-chip">${escapeHTML(tag)}</span>`).join("")}</div>` : ""}
              ${item.last_error ? `<code>${escapeHTML(item.last_error)}</code>` : ""}
              ${isRejectedTab ? `<div class="admin-link-queue-actions"><button type="button" class="ghost-button" data-link-retry-id="${escapeHTML(item.id || "")}">Retry Again</button></div>` : ""}
            </div>
          </article>
        `).join("")}
      </div>
    </section>
  `;

  adminLinkQueueList.querySelectorAll("[data-link-retry-id]").forEach((element) => {
    element.addEventListener("click", () => {
      const id = element.getAttribute("data-link-retry-id") || "";
      retryRejectedLinkDownload(id).catch((error) => {
        console.error(error);
      });
    });
  });
}

function syncAdminLinkQueuePolling() {
  if (adminLinkQueuePollInterval) {
    window.clearInterval(adminLinkQueuePollInterval);
    adminLinkQueuePollInterval = null;
  }

  if (!canManageUsers() || !isAdminView()) {
    renderAdminLinkRetryStatus();
    return;
  }

  adminLinkQueuePollInterval = window.setInterval(() => {
    fetchAdminLinkRetryStatus().catch((error) => {
      console.error(error);
    });
  }, 8000);
}

async function retryRejectedLinkDownload(id) {
  if (!id) {
    return;
  }
  const response = await fetch(`/api/admin/link-downloads/${encodeURIComponent(id)}/retry`, {
    method: "POST",
  });
  if (!(await expectAuthorized(response, "Failed to requeue rejected link."))) {
    return;
  }
  showToast("Rejected link moved back into the retry queue.", "success", { title: "Link Retries", duration: 2600 });
  await fetchAdminLinkRetryStatus();
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
      showToast(`Kept ${entry.meme.originalName}.`, "success", { title: "Delete Queue" });
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
  if (auditLogState.length === 0) {
    target.innerHTML = `<p class="users-empty">No audit activity recorded yet.</p>`;
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
    const memeTitle = event.meme_original_name || "Unknown or deleted meme";
    const canOpenEditor = !!event.meme_id && !!event.meme_file_path;
    const actionsMarkup = canOpenEditor
      ? `
        <div class="admin-row-actions">
          <button class="ghost-button audit-log-open-modal-button" type="button">Edit Meme</button>
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
	const homeMode = state.filters.view === "home";
	const libraryMode = !adminMode && !homeMode;
	if (libraryMode && libraryTitle) {
		libraryTitle.textContent = state.filters.tag
			? `#${state.filters.tag}`
			: (LIBRARY_VIEW_TITLES[state.filters.view] || "All Items");
	}
	document.body.classList.toggle("home-mode", homeMode);
	document.body.classList.toggle("admin-mode", adminMode);
	document.body.classList.toggle("library-mode", libraryMode);
  const canBrowseLibrary = canView();
  const usesSharedAdminTable = adminMode && ["delete-queue", "audit-logs", "shares"].includes(activeAdminTab());
  const showAdminUsersPanel = adminMode && activeAdminTab() === "users";
  const showAdminBackupPanel = adminMode && activeAdminTab() === "backup";
  adminView?.classList.toggle("hidden", !adminMode);
	homeDashboard?.classList.toggle("hidden", !homeMode);
	libraryHeading?.classList.toggle("hidden", !libraryMode);
	filterPanel?.classList.toggle("is-unavailable", !libraryMode);
  adminTabs.forEach((tab) => {
    const active = tab.dataset.adminTab === activeAdminTab();
    tab.classList.toggle("is-active", active);
    tab.setAttribute("aria-selected", String(active));
  });
  adminUsersPanel?.classList.toggle("hidden", !showAdminUsersPanel);
  adminBackupPanel?.classList.toggle("hidden", !showAdminBackupPanel);
  adminViewTable?.classList.toggle("hidden", !usesSharedAdminTable);
  const pageState = getActiveAdminPageState();
  adminPagination?.classList.toggle("hidden", !adminMode || !pageState);
  memeGridLoader?.classList.toggle("hidden", !libraryMode || !state.library.loading);
  memeGridStatus?.classList.toggle("hidden", !libraryMode || memeGridStatus.textContent === "");
  memeGridTopSpacer?.classList.toggle("hidden", !libraryMode);
  memeGridBottomSpacer?.classList.toggle("hidden", !libraryMode);
  memeGrid?.classList.toggle("hidden", !libraryMode);
  memeGridSentinel?.classList.toggle("hidden", !libraryMode);
  emptyState?.classList.toggle("hidden", !libraryMode || (canBrowseLibrary && state.memes.length !== 0));
  document.querySelector(".library-toolbar")?.classList.toggle("hidden", !libraryMode || !canBrowseLibrary);
	document.body.classList.toggle("library-list-view", state.library.viewMode === "list");
	gridViewButton?.classList.toggle("is-active", state.library.viewMode === "grid");
	listViewButton?.classList.toggle("is-active", state.library.viewMode === "list");
	gridViewButton?.setAttribute("aria-pressed", String(state.library.viewMode === "grid"));
	listViewButton?.setAttribute("aria-pressed", String(state.library.viewMode === "list"));
  renderAdminDashboard();
  renderAdminTagHygiene();
  renderAdminTagQueueStatus();
  renderAdminLinkRetryStatus();
  syncAdminTagQueuePolling();
  syncAdminLinkQueuePolling();
  syncAdminBackupPolling();
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
		if (state.filters.query) params.set("q", state.filters.query);
    if (state.filters.view && state.filters.view !== "library") params.set("view", state.filters.view);
		params.set("sort", state.library.sort);
    params.set("offset", `${requestedPage * MEME_PAGE_SIZE}`);
      params.set("limit", `${MEME_PAGE_SIZE}`);
  
      const response = await fetch(`/api/memes?${params.toString()}`);
      if (response.status === 403) {
        redirectToForbidden();
        return;
      }
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
    emptyState.textContent = "No memes match this view yet.";
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
  fetchSidebarPopularTags().catch((error) => console.error(error));
  if (isAdminView() && canManageUsers()) {
    fetchAdminTagQueueStatus().catch((error) => {
      console.error(error);
    });
    fetchAdminLinkRetryStatus().catch((error) => {
      console.error(error);
    });
  }
	if (state.filters.view === "home") {
		await fetchVaultDashboard();
		renderContentMode();
		return;
	}
  if (isAdminView() && canManageUsers() && activeAdminTab() === "dashboard") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Archive Dashboard";
    adminViewCopy.textContent = "A quick pulse-check on backlog size, tag coverage, recent uploads, and the tags that define your archive.";
    setAdminViewStatus("Loading dashboard...");
    adminViewTable.innerHTML = "";
    const dashboard = await fetchAdminDashboard();
    setAdminViewStatus(dashboard ? "" : "Could not load dashboard.");
    renderContentMode();
    return;
  }
  if (isAdminView() && canManageUsers() && activeAdminTab() === "tag-hygiene") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Tag Hygiene";
    adminViewCopy.textContent = "Review likely misspellings, separator variants, and close tag duplicates, then merge them into cleaner canonical tags.";
    setAdminViewStatus("Loading tag hygiene tools...");
    adminViewTable.innerHTML = "";
    const report = await fetchAdminTagHygiene();
    setAdminViewStatus(report ? "" : "Could not load tag hygiene tools.");
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "audit-logs") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Activity Log";
    adminViewCopy.textContent = "A full activity log with actor, action, target meme, and quick-open access for review.";
    setAdminViewStatus("Loading audit logs...");
    adminViewTable.innerHTML = "";
    syncAdminPagination();
    const events = await fetchAuditLogs();
    setAdminViewStatus(events ? "" : "Could not load audit logs.");
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "delete-queue") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Delete Requests";
    adminViewCopy.textContent = "Review pending meme deletions, inspect the media, and either keep the meme or approve the delete.";
    setAdminViewStatus("Loading delete queue...");
    adminViewTable.innerHTML = "";
    syncAdminPagination();
    const entries = await fetchDeleteQueue();
    setAdminViewStatus(entries ? "" : "Could not load delete queue.");
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "users") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "User Access";
    adminViewCopy.textContent = "Manage users and permissions from one place.";
    setAdminViewStatus("Loading users...");
    adminViewTable.innerHTML = "";
    const users = await fetchManagedUsers();
    setAdminViewStatus(users ? "" : "Could not load users.");
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "shares") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Shared Memes";
    adminViewCopy.textContent = "See every meme with active public access, copy its current link, or revoke it immediately.";
    setAdminViewStatus("Loading shared memes...");
    adminViewTable.innerHTML = "";
    const shares = await fetchAdminShares();
    setAdminViewStatus(shares?.length ? "" : "No memes are currently shared.");
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "tag-queue") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Tag Queue";
    adminViewCopy.textContent = "Monitor the background tag suggestion worker and the memes waiting to be processed.";
    setAdminViewStatus("");
    adminViewTable.innerHTML = "";
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "tag-review") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Suggested Tags";
    adminViewCopy.textContent = "Review memes with pending AI tag suggestions and open them directly in the edit modal.";
    setAdminViewStatus("");
    adminViewTable.innerHTML = "";
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "link-retries") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Link Retries";
    adminViewCopy.textContent = "Failed link imports waiting for another download attempt.";
    setAdminViewStatus("");
    adminViewTable.innerHTML = "";
    renderContentMode();
    return;
  }
  if (isAdminView() && activeAdminTab() === "rejected-links") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Rejected Links";
    adminViewCopy.textContent = "Links that exhausted their retry budget and need a manual requeue if you want to try again later.";
    setAdminViewStatus("");
    adminViewTable.innerHTML = "";
    renderContentMode();
    return;
  }
  if (isAdminView() && canManageUsers() && activeAdminTab() === "backup") {
    adminViewKicker.textContent = "Admin";
    adminViewTitle.textContent = "Backup & Restore";
    adminViewCopy.textContent = "Move the complete meme library and its database to another MemeIndex Docker instance.";
    setAdminViewStatus("");
    adminViewTable.innerHTML = "";
    renderAdminBackupStatus();
    await fetchAdminBackupStatus();
    renderContentMode();
    return;
  }
  await fetchMemes({ page: 0 });
  renderContentMode();
}

async function applyTagSearch(rawValue) {
	state.filters.tag = "";
	state.filters.query = String(rawValue || "").trim();
	if (state.filters.view === "home") state.filters.view = "library";
  await loadInitialMemes();
}

function syncMobileSearchHeader() {
  const alreadyCondensed = document.body.classList.contains("mobile-search-only");
  document.body.classList.toggle(
    "mobile-search-only",
    mobileSearchHeaderMediaQuery.matches && window.scrollY > (alreadyCondensed ? 0 : 48),
  );
}

async function fetchSidebarPopularTags() {
	if (!sidebarPopularTags || !sidebarPopularTagsSection) return;
	const response = await fetch("/api/tags/popular?limit=10", { cache: "no-store" });
	if (!(await expectAuthorized(response, "Failed to load popular tags."))) return;
	const payload = await response.json();
	renderSidebarPopularTags(Array.isArray(payload.tags) ? payload.tags : []);
}

function renderSidebarPopularTags(tags) {
	if (!sidebarPopularTags || !sidebarPopularTagsSection) return;
	sidebarPopularTags.replaceChildren();
	tags.slice(0, 10).forEach((tag) => {
		const name = String(tag.name || "").trim();
		if (!name) return;
		const button = document.createElement("button");
		button.type = "button";
		button.className = "nav-item sidebar-tag-item";
		button.dataset.sidebarTag = name;
		button.title = `Show items tagged ${name}`;
		button.innerHTML = '<span class="nav-icon sidebar-tag-icon" aria-hidden="true">#</span><span class="nav-label tag-chip"></span><span class="nav-count"></span>';
		button.querySelector(".nav-label").textContent = name;
		button.querySelector(".nav-count").textContent = Number(tag.count || 0).toLocaleString();
		button.addEventListener("click", () => navigateToTag(name));
		sidebarPopularTags.appendChild(button);
	});
	sidebarPopularTagsSection.classList.toggle("hidden", sidebarPopularTags.childElementCount === 0);
	renderSidebarViewState();
}

function navigateToTag(tag) {
	const normalizedTag = String(tag || "").trim();
	if (!normalizedTag) return;
	state.filters.query = "";
	state.filters.tag = normalizedTag;
	state.filters.view = "library";
	tagSearchInput.value = "";
	loadInitialMemes().catch((error) => console.error(error));
	if (drawerMediaQuery.matches) closeSidebarDrawer();
}

async function fetchVaultDashboard({ onlyIfNewestChanged = false } = {}) {
	const response = await fetch("/api/dashboard", { cache: "no-store" });
	if (!(await expectAuthorized(response, "Failed to load dashboard."))) return;
	const dashboard = await response.json();
	if (onlyIfNewestChanged) {
		const incomingNewestID = String(dashboard.recent_items?.[0]?.id || "");
		const renderedNewestID = String(dashboardRecent?.querySelector(".meme-card")?.dataset.memeId || "");
		if (incomingNewestID === renderedNewestID) return false;
	}
	state.library.counts = dashboard.counts || state.library.counts;
	renderSidebarCounts();
	const cards = [
		["items", "Total memes", Number(dashboard.total_items || 0).toLocaleString(), "Files and links"],
		["tags", "Tags", Number(dashboard.tag_count || 0).toLocaleString(), "Across your archive"],
		["storage", "Storage used", formatSize(Number(dashboard.storage_bytes || 0)), "Original files"],
		["favorites", "Favorites", Number(dashboard.favorites || 0).toLocaleString(), "Quick access"],
	];
	dashboardStats.replaceChildren(...cards.map(([icon, label, value, note]) => {
		const article = document.createElement("article");
		article.className = "dashboard-stat-card";
		article.dataset.icon = icon;
		article.innerHTML = `<i aria-hidden="true"></i><span>${label}</span><strong>${value}</strong><small>${note}</small>`;
		return article;
	}));
	renderDashboardMemeCollection(
		dashboardRecent,
		dashboard.recent_items,
		"Your newest items will appear here.",
	);
	renderDashboardMemeCollection(
		dashboardFavorites,
		dashboard.favorite_items,
		"Favorite a meme and it will appear here.",
	);
	renderDashboardMemeCollection(
		dashboardRandom,
		dashboard.random_items,
		"Add something to your vault to get random picks.",
	);
	dashboardStorageLabel.textContent = formatSize(Number(dashboard.storage_bytes || 0));
	dashboardStorageBar.style.width = dashboard.storage_bytes > 0 ? "100%" : "0%";
	const topTags = Array.isArray(dashboard.top_tags) ? dashboard.top_tags : [];
	dashboardTags?.replaceChildren(...topTags.map((tag, index) => {
		const button = document.createElement("button");
		button.type = "button";
		button.className = "dashboard-tag-card";
		button.innerHTML = `<i aria-hidden="true">${["☺", "▤", "◈", "✦", "◉"][index % 5]}</i><span class="tag-chip"></span><small></small>`;
		button.querySelector("span").textContent = tag.name;
		button.querySelector("small").textContent = `${tag.count} item${tag.count === 1 ? "" : "s"}`;
		button.addEventListener("click", () => navigateToTag(tag.name));
		return button;
	}));
	dashboardTagsSection?.classList.toggle("hidden", topTags.length === 0);
	return true;
}

let homeDashboardRefreshInFlight = false;

async function refreshHomeDashboardForNewMemes() {
	if (homeDashboardRefreshInFlight || document.hidden || state.filters.view !== "home" || !canView()) return;
	homeDashboardRefreshInFlight = true;
	try {
		await fetchVaultDashboard({ onlyIfNewestChanged: true });
	} finally {
		homeDashboardRefreshInFlight = false;
	}
}

function renderDashboardMemeCollection(container, items, emptyMessage) {
	if (!container) return;
	container.replaceChildren();
	const memes = Array.isArray(items) ? items : [];
	if (!memes.length) {
		const empty = document.createElement("p");
		empty.className = "dashboard-empty";
		empty.textContent = emptyMessage;
		container.appendChild(empty);
		return;
	}
	memes.forEach((meme) => {
		// Dashboard items share the library state and card component so favorites,
		// permissions, and the editor modal behave identically everywhere.
		upsertMemeInState(meme);
		const card = buildMemeCardElement(meme);
		card.classList.add("dashboard-meme-card");
		card.title = `Open ${meme.originalName || "meme"} in the editor`;
		container.appendChild(card);
	});
}

async function refreshDashboardRandomItems() {
	if (!dashboardRandomRefresh || !dashboardRandom) return;
	dashboardRandomRefresh.disabled = true;
	dashboardRandomRefresh.classList.add("is-loading");
	try {
		const response = await fetch("/api/dashboard", { cache: "no-store" });
		if (!(await expectAuthorized(response, "Could not shuffle random picks."))) return;
		const dashboard = await response.json();
		renderDashboardMemeCollection(
			dashboardRandom,
			dashboard.random_items,
			"Add something to your vault to get random picks.",
		);
	} finally {
		dashboardRandomRefresh.disabled = false;
		dashboardRandomRefresh.classList.remove("is-loading");
	}
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
    item.classList.toggle("is-active", !state.filters.tag && item.dataset.view === state.filters.view);
  });
  sidebarPopularTags?.querySelectorAll(".sidebar-tag-item").forEach((item) => {
    item.classList.toggle("is-active", item.dataset.sidebarTag === state.filters.tag);
  });
}

function highlightTagSuggestion(input, list, index, scroll = false) {
  const options = list.querySelectorAll(".tag-suggestion");
  options.forEach((option, optionIndex) => {
    const selected = optionIndex === index;
    option.classList.toggle("is-active", selected);
    option.setAttribute("aria-selected", String(selected));
  });
  const active = options[index];
  if (active && !list.classList.contains("hidden")) {
    input.setAttribute("aria-activedescendant", active.id);
    if (scroll) active.scrollIntoView({ block: "nearest" });
  } else {
    input.removeAttribute("aria-activedescendant");
  }
}

function dismissTagSuggestions(input, list, setIndex) {
  list.classList.add("hidden");
  input.setAttribute("aria-expanded", "false");
  input.removeAttribute("aria-activedescendant");
  setIndex(-1);
}

function navigateTagSuggestions(event, input, list, tags, index, setIndex) {
  if (event.isComposing || event.ctrlKey || event.altKey || event.metaKey) return false;
  if (!tags.length || list.classList.contains("hidden")) return false;
  if (event.key === "Escape") {
    event.preventDefault();
    event.stopPropagation();
    dismissTagSuggestions(input, list, setIndex);
    return true;
  }
  if (!["Tab", "ArrowDown", "ArrowUp"].includes(event.key)) return false;
  const backwards = event.key === "ArrowUp" || (event.key === "Tab" && event.shiftKey);
  const next = backwards ? (index <= 0 ? tags.length - 1 : index - 1) : (index + 1) % tags.length;
  event.preventDefault();
  setIndex(next);
  highlightTagSuggestion(input, list, next, true);
  return true;
}

function renderTagSuggestionList(input, list, tags, index, setIndex, selectTag) {
  if (!input.hasAttribute("aria-controls")) {
    input.setAttribute("role", "combobox");
    input.setAttribute("aria-autocomplete", "list");
    input.setAttribute("aria-controls", list.id);
    input.addEventListener("blur", () => dismissTagSuggestions(input, list, setIndex));
  }
  list.setAttribute("role", "listbox");
  list.setAttribute("aria-label", "Suggested tags");
  list.replaceChildren();
  if (!tags.length || document.activeElement !== input) {
    dismissTagSuggestions(input, list, setIndex);
    return;
  }
  tags.forEach((tag, optionIndex) => {
    const button = document.createElement("button");
    button.type = "button";
    button.tabIndex = -1;
    button.id = `${list.id}-option-${optionIndex}`;
    button.className = "tag-suggestion";
    button.setAttribute("role", "option");
    button.setAttribute("aria-label", tag);
    button.innerHTML = `<span class="tag-suggestion-name"><span class="tag-suggestion-mark" aria-hidden="true">#</span>${escapeHTML(tag)}</span><span class="tag-suggestion-hint" aria-hidden="true">Enter ↵</span>`;
    button.addEventListener("pointermove", () => {
      setIndex(optionIndex);
      highlightTagSuggestion(input, list, optionIndex);
    });
    // Keep focus in the combobox so Enter can select a mouse-highlighted option.
    button.addEventListener("pointerdown", (event) => event.preventDefault());
    button.addEventListener("click", () => { selectTag(tag); input.focus(); });
    list.appendChild(button);
  });
  const help = document.createElement("div");
  help.className = "tag-suggestion-help";
  help.setAttribute("role", "presentation");
  help.textContent = "Tab / ↑ ↓ to browse · Enter to select · Esc to close";
  list.appendChild(help);
  list.classList.remove("hidden");
  input.setAttribute("aria-expanded", "true");
  highlightTagSuggestion(input, list, index);
}

function renderTopTagSuggestions() {
  renderTagSuggestionList(tagSearchInput, tagSearchSuggestions, topTagSuggestionState, activeTopTagSuggestionIndex,
    (index) => { activeTopTagSuggestionIndex = index; }, (tag) => {
    tagSearchInput.value = tag;
    topTagSuggestionState = [];
    renderTopTagSuggestions();
    window.clearTimeout(topTagSearchDebounce);
    topTagSearchDebounce = null;
    applyTagSearch(tag).catch((error) => console.error(error));
  });
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
	const title = card.querySelector(".card-title");
	const meta = card.querySelector(".card-meta");
	if (title) title.textContent = meme.originalName || "Untitled item";
	if (meta) {
		const extension = String(meme.originalName || "").split(".").pop();
		meta.textContent = `${meme.sourceUrl ? "External source" : (extension || meme.contentType || "File").toUpperCase()} · ${formatSize(meme.sizeBytes || 0)}`;
	}
	card.querySelector(".card-hitarea")?.setAttribute("aria-label", `Open ${meme.originalName || "item"} details`);
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
  memeGrid.classList.toggle("has-single-item", memes.length === 1);
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
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${meme.contentType}</span><span>Use Share to create a 30-day access link.</span>`;
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
    video.preload = "auto";
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
    audio.preload = "auto";
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
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${meme.originalName}</span><span>Use Share to create a 30-day access link.</span>`;
  return icon;
}

function trimRandomReelPreloadCache(limit = 8) {
  while (randomReelPreloadCache.size > limit) {
    const oldestKey = randomReelPreloadCache.keys().next().value;
    const cached = randomReelPreloadCache.get(oldestKey);
    if (cached instanceof HTMLMediaElement) {
      cached.pause();
      cached.removeAttribute("src");
      cached.load();
    }
    randomReelPreloadCache.delete(oldestKey);
  }
}

function resetRandomReelPreloadCache() {
  randomReelPreloadCache.forEach((cached) => {
    if (cached instanceof HTMLMediaElement) {
      cached.pause();
      cached.removeAttribute("src");
      cached.load();
    }
  });
  randomReelPreloadCache.clear();
}

function warmRandomReelMedia(meme) {
  if (!meme?.id || !meme.filePath || randomReelPreloadCache.has(meme.id)) {
    return;
  }

  if (meme.contentType.startsWith("image/")) {
    const img = new Image();
    img.decoding = "async";
    img.src = meme.filePath;
    randomReelPreloadCache.set(meme.id, img);
    trimRandomReelPreloadCache();
    return;
  }

  if (meme.contentType.startsWith("video/") || meme.contentType.startsWith("audio/")) {
    const media = document.createElement(meme.contentType.startsWith("video/") ? "video" : "audio");
    media.preload = "auto";
    media.muted = true;
    media.src = meme.filePath;
    media.load();
    randomReelPreloadCache.set(meme.id, media);
    trimRandomReelPreloadCache();
  }
}

function primeRandomReelWindow(payload) {
  const prevMemes = Array.isArray(payload?.prev_memes) ? payload.prev_memes : [];
  const nextMemes = Array.isArray(payload?.next_memes) ? payload.next_memes : [];
  prevMemes.forEach(warmRandomReelMedia);
  nextMemes.forEach(warmRandomReelMedia);
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
  primeRandomReelWindow(payload);
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
  randomReelMeta.textContent = `${formatSize(meme.sizeBytes)} \u2022 ${meme.contentType}`;
  randomReelShare.disabled = !canView();
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
  const playLabel = randomReelPlay.querySelector(".random-reel-nav-label");

  randomReelPlay.disabled = !supportsMediaControls;
  randomReelVolumeToggle.disabled = !supportsMediaControls;
  randomReelVolume.disabled = !supportsMediaControls;
  randomReelPlay.classList.toggle("hidden", !supportsMediaControls);
  randomReelVolumeWrap.classList.toggle("hidden", !supportsMediaControls);

  if (!supportsMediaControls) {
    randomReelPlay.setAttribute("aria-label", "Play media");
    randomReelPlay.setAttribute("data-tooltip", "Play");
    randomReelPlayIcon.innerHTML = "&#9654;";
    if (playLabel) playLabel.textContent = "Play";
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
  if (playLabel) playLabel.textContent = paused ? "Play" : "Pause";

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
  resetRandomReelPreloadCache();
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
  uploadPreview.innerHTML = `
    <div class="file-icon upload-empty-preview">
      <span class="upload-empty-icon" aria-hidden="true">
        <svg viewBox="0 0 24 24"><path d="M12 15V4m0 0L8 8m4-4 4 4M5 14v4a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-4"/></svg>
      </span>
      <strong>Drop files to preview</strong>
      <span>Review your selection before it enters the vault.</span>
    </div>`;
}

function setUploadDragActive(active) {
  uploadPreviewWrap?.classList.toggle("is-drag-active", active);
}

function isModalBusy(modal) {
  return modal?.classList.contains("is-busy");
}

function setModalBusyState(modal, overlay, messageNode, busy, message) {
  if (!modal || !overlay) return;
  modal.classList.toggle("is-busy", busy);
  overlay.classList.toggle("hidden", !busy);
  overlay.setAttribute("aria-hidden", String(!busy));
  if (messageNode && typeof message === "string" && message.trim()) {
    messageNode.textContent = message;
  }
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
  icon.innerHTML = `<strong>${pickIcon(type || fileName)}</strong><span>${fileName}</span><span>Preview availability depends on the file type.</span>`;
  uploadPreview.appendChild(icon);
  appendSelectionCount();
}

function getMemeById(id) {
  return state.memes.find((item) => item.id === id);
}

function getCardsByMemeId(id) {
  return Array.from(document.querySelectorAll(`.meme-card[data-meme-id="${id}"]`));
}

function applyFavoriteStateToButton(button, favorite) {
  if (!button) return;
  button.classList.toggle("is-active", favorite);
  button.classList.toggle("is-favorite", favorite);
  button.setAttribute("aria-pressed", favorite ? "true" : "false");
  button.setAttribute("aria-label", favorite ? "Remove from favorites" : "Add to favorites");
  button.setAttribute("data-tooltip", favorite ? "Remove from Favorites" : "Add to Favorites");

  const label = button.querySelector(".modal-action-label");
  if (label) {
    label.textContent = favorite ? "Favorited" : "Favorite";
  }

  if (button.matches(".random-reel-favorite")) {
    const icon = button.querySelector(".random-reel-nav-icon");
    const reelLabel = button.querySelector(".random-reel-nav-label");
    if (icon) {
      icon.textContent = favorite ? "\u2665" : "\u2661";
    }
    if (reelLabel) {
      reelLabel.textContent = favorite ? "Favorited" : "Favorite";
    }
  }
}

function syncFavoriteUI(updatedMeme) {
  if (!updatedMeme?.id) return;

  if (updatedMeme.favorite) {
    state.library.counts.favorites += 1;
  } else {
    state.library.counts.favorites = Math.max(0, (state.library.counts.favorites || 0) - 1);
  }
  renderSidebarCounts();

  const cards = getCardsByMemeId(updatedMeme.id);
  cards.forEach((card) => {
    applyFavoriteStateToButton(card.querySelector(".favorite-button"), updatedMeme.favorite);
  });

  if (state.filters.view === "favorites") {
    if (!updatedMeme.favorite && cards.length > 0) {
      state.memes = state.memes.filter((meme) => meme.id !== updatedMeme.id);
      cards.forEach((card) => card.remove());
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

  if (state.filters.view === "home") {
    fetchVaultDashboard().catch((error) => console.error(error));
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

function upsertMemeInState(meme) {
  if (!meme?.id) return null;

  const existing = updateMemeInState(meme);
  if (existing) {
    return existing;
  }

  state.memes = [meme, ...state.memes];
  return meme;
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
  if (!id) return false;
  const response = await fetch(`/api/admin/memes/${encodeURIComponent(id)}`);
  if (!(await expectAuthorized(response, "Failed to load meme."))) {
    return false;
  }
  const meme = await response.json();
  openModalWithMeme(upsertMemeInState(meme) || meme);
  return true;
}

async function completeAdminTagReview(id) {
  const response = await fetch(`/api/memes/${encodeURIComponent(id)}/tag-suggestions`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: "dismiss_all" }),
  });
  if (!(await expectAuthorized(response, "Failed to complete suggested tag review."))) {
    showToast("The meme was saved, but its suggestion review could not be completed.", "error", { title: "Suggested Tags" });
    return false;
  }

  const meme = await response.json();
  updateMemeSuggestedTagsInState(meme.id, meme.suggestedTags || []);
  return true;
}

async function openNextAdminTagReview() {
  if (!adminTagReviewSessionActive || adminTagReviewAdvancing) return;
  adminTagReviewAdvancing = true;
  try {
    const status = await fetchAdminTagQueueStatus();
    const pending = Array.isArray(status?.pending_review_memes) ? status.pending_review_memes : [];
    const next = pending[0];
    if (!next?.id) {
      adminTagReviewSessionActive = false;
      showToast("Suggested tag review queue complete.", "success", { title: "Suggested Tags", duration: 2600 });
      return;
    }

    const opened = await openAdminMemeByID(next.id);
    if (!opened) {
      adminTagReviewSessionActive = false;
    }
  } finally {
    adminTagReviewAdvancing = false;
  }
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
  if (modalMobileTitle) {
    modalMobileTitle.textContent = truncateWithCounter(meme.originalName, 44);
    modalMobileTitle.title = meme.originalName;
  }
  if (modalMobileMeta) {
    modalMobileMeta.textContent = `${formatSize(meme.sizeBytes)} \u2022 ${meme.contentType}`;
  }
  setModalTags(meme.tags || []);
  modalTagsInput.value = "";
  renderTagEditor();
  renderTagSuggestions();
  modalNotesInput.value = meme.notes || "";
  modalShare.disabled = !canView();
  if (modalSourceField && modalSourceLink) {
    const hasSourceURL = Boolean((meme.sourceUrl || "").trim());
    modalSourceField.classList.toggle("hidden", !hasSourceURL);
    modalSourceLink.textContent = meme.sourceUrl || "";
    modalSourceLink.href = hasSourceURL ? meme.sourceUrl : "#";
  }
  applyFavoriteStateToButton(modalFavorite, meme.favorite);
  modalTagsInput.disabled = !canAddTags();
  modalNotesInput.readOnly = !canEditMetadata();
  modalFavorite.disabled = !canView();
  modalSave.disabled = !canEditMetadata();
  modalDelete.disabled = !canDeleteMemes();
  modalAITagTools?.classList.toggle("hidden", !supportsModalAITagSuggestions(meme) || !canAddTags());
  renderModalAITagSuggestions();
  if (modalSuggestTagsButton) {
    modalSuggestTagsButton.disabled = !canAddTags();
    modalSuggestTagsButton.textContent = (meme.suggestedTags && meme.suggestedTags.length > 0) ? "Refresh Suggestions" : "Suggest Tags";
    modalSuggestTagsButton.title = canAddTags() ? "" : "You do not have permission to add tags";
  }
  if (modalSuggestTagsStatus) {
    modalSuggestTagsStatus.textContent = "";
    modalSuggestTagsStatus.classList.add("hidden");
  }
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

function closeModal(options = {}) {
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
  memeModal.classList.remove("has-video", "has-audio");
  modalPanelToggle?.setAttribute("aria-expanded", "false");
  modalPanelToggle?.setAttribute("aria-label", "Open details");
  activeMemeId = null;
  modalSnapshot = null;
  setModalAuditVisibility(false);
  overlayClose.classList.add("hidden");
  if (!options.continueAdminReview) {
    adminTagReviewSessionActive = false;
  }
  clearMemeDeepLinkURL();
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
  const isVideo = media instanceof HTMLVideoElement;
  const isAudio = media instanceof HTMLAudioElement;

  memeModal?.classList.toggle("has-video", isVideo);
  memeModal?.classList.toggle("has-audio", isAudio);
  modalMediaControls?.classList.toggle("hidden", !supportsMediaControls);
  modalPlay.disabled = !supportsMediaControls;
  modalVolumeToggle.disabled = !supportsMediaControls;
  modalVolume.disabled = !supportsMediaControls;
  modalVolumeWrap.classList.toggle("hidden", !supportsMediaControls);
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
  renderModalAITagSuggestions();
}

function removeModalTag(tagIdToRemove) {
  if (!canRemoveTags()) return;
  modalTagState = modalTagState.filter((tag) => tag.id !== tagIdToRemove);
  if (!normalizeTagValue(modalTagsInput.value)) {
    modalSuggestionState = [];
  }
  renderTagEditor();
  renderTagSuggestions();
  renderModalAITagSuggestions();
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
    removeButton.textContent = "×";
    removeButton.disabled = !canRemoveTags();
    removeButton.addEventListener("click", (event) => {
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
  renderTagSuggestionList(modalTagsInput, modalTagSuggestions, modalSuggestionState, activeSuggestionIndex,
    (index) => { activeSuggestionIndex = index; }, addModalTag);
}

function supportsModalAITagSuggestions(meme) {
  if (!meme) return false;
  return typeof meme.contentType === "string" && (
    meme.contentType.startsWith("image/") ||
    meme.contentType.startsWith("video/")
  );
}

function renderModalAITagSuggestions() {
  if (!modalAITagSuggestions) {
    return;
  }

  modalAITagSuggestions.innerHTML = "";
  const activeMeme = getMemeById(activeMemeId);
  const storedSuggestions = Array.isArray(activeMeme?.suggestedTags) ? activeMeme.suggestedTags : [];
  const availableSuggestions = storedSuggestions.filter((tag) => !modalTagState.some((entry) => entry.value === tag));
  if (modalDismissAllSuggestedTagsButton) {
    modalDismissAllSuggestedTagsButton.classList.toggle("hidden", availableSuggestions.length === 0);
    modalDismissAllSuggestedTagsButton.disabled = !canEditMetadata() || availableSuggestions.length === 0;
  }
  if (availableSuggestions.length === 0) {
    modalAITagSuggestions.classList.add("hidden");
    return;
  }

  for (const tag of availableSuggestions) {
    const row = document.createElement("div");
    row.className = "modal-ai-tag-row";
    row.innerHTML = `
      <span class="modal-ai-tag-label tag-chip">${escapeHTML(tag)}</span>
      <div class="modal-ai-tag-actions">
        <button type="button" class="modal-ai-tag-action" data-action="add" data-tag="${escapeHTML(tag)}">Add</button>
        <button type="button" class="modal-ai-tag-action modal-ai-tag-action-muted" data-action="dismiss" data-tag="${escapeHTML(tag)}">Dismiss</button>
      </div>
    `;
    row.querySelectorAll("button").forEach((button) => {
      button.addEventListener("click", () => {
        const action = button.dataset.action;
        const selectedTag = button.dataset.tag || "";
        if (action === "add") {
          applyStoredTagSuggestion(selectedTag).catch((error) => {
            console.error(error);
          });
          return;
        }
        dismissStoredTagSuggestion(selectedTag).catch((error) => {
          console.error(error);
        });
      });
    });
    modalAITagSuggestions.appendChild(row);
  }

  modalAITagSuggestions.classList.remove("hidden");
}

function syncModalSuggestedTagButtonLabel() {
  if (modalSuggestTagsButton) {
    modalSuggestTagsButton.textContent = (getMemeById(activeMemeId)?.suggestedTags || []).length > 0 ? "Refresh Suggestions" : "Suggest Tags";
  }
}

function updateMemeSuggestedTagsInState(id, suggestedTags) {
  return updateMemeInState({
    id,
    suggestedTags: Array.isArray(suggestedTags) ? suggestedTags.map((tag) => normalizeTagValue(tag)).filter(Boolean) : [],
  });
}

function updateModalSnapshotWithSavedTag(tag) {
  if (!modalSnapshot) {
    return;
  }
  const normalized = normalizeTagValue(tag);
  if (!normalized) {
    return;
  }
  const savedTags = new Set(String(modalSnapshot.tags || "").split(", ").map((value) => normalizeTagValue(value)).filter(Boolean));
  savedTags.add(normalized);
  modalSnapshot.tags = [...savedTags].sort().join(", ");
}

async function fetchModalAITagSuggestions() {
  if (!canAddTags() || !activeMemeId || modalLLMTagSuggestionLoading) {
    return;
  }

  modalLLMTagSuggestionLoading = true;
  if (modalSuggestTagsButton) {
    modalSuggestTagsButton.disabled = true;
    modalSuggestTagsButton.textContent = "Thinking...";
  }
  if (modalSuggestTagsStatus) {
    modalSuggestTagsStatus.textContent = "Analyzing the current image or video thumbnail...";
    modalSuggestTagsStatus.classList.remove("hidden");
  }

  try {
    const response = await fetch(`/api/memes/${encodeURIComponent(activeMemeId)}/tag-suggestions/refresh`, {
      method: "POST",
    });
    if (response.status === 401) {
      window.location.href = "/auth/login";
      return;
    }
    if (response.status === 403) {
      window.alert("You do not have permission to do that.");
      return;
    }

    let payload = {};
    try {
      payload = await response.json();
    } catch (error) {
      payload = {};
    }

    if (!response.ok) {
      if (modalSuggestTagsStatus) {
        modalSuggestTagsStatus.textContent = payload.error || "Tag suggestions are unavailable right now.";
        modalSuggestTagsStatus.classList.remove("hidden");
      }
      return;
    }

    updateMemeInState({
      id: activeMemeId,
      suggestedTags: Array.isArray(payload.tags) ? payload.tags.map((tag) => normalizeTagValue(tag)).filter(Boolean) : [],
    });
    renderModalAITagSuggestions();

    const sourceLabel = payload.source === "video-thumbnail"
      ? "video thumbnail"
      : "image";
    const visibleSuggestions = (getMemeById(activeMemeId)?.suggestedTags || []).filter((tag) => !modalTagState.some((entry) => entry.value === tag));
    if (modalSuggestTagsStatus) {
      if (visibleSuggestions.length === 0) {
        modalSuggestTagsStatus.textContent = `No new suggestions came back from ${payload.model || "the local model"}.`;
      } else {
        modalSuggestTagsStatus.textContent = `Suggested ${visibleSuggestions.length} tag${visibleSuggestions.length === 1 ? "" : "s"} from the ${sourceLabel}${payload.model ? ` using ${payload.model}` : ""}.`;
      }
      modalSuggestTagsStatus.classList.remove("hidden");
    }
  } catch (error) {
    console.error(error);
    if (modalSuggestTagsStatus) {
      modalSuggestTagsStatus.textContent = "Tag suggestions failed. The local model may still be starting up.";
      modalSuggestTagsStatus.classList.remove("hidden");
    }
  } finally {
    modalLLMTagSuggestionLoading = false;
    if (modalSuggestTagsButton) {
      modalSuggestTagsButton.disabled = !canAddTags();
      syncModalSuggestedTagButtonLabel();
    }
  }
}

async function dismissStoredTagSuggestion(tag, options = {}) {
  if (!activeMemeId || !canEditMetadata()) {
    return;
  }

  const response = await fetch(`/api/memes/${encodeURIComponent(activeMemeId)}/tag-suggestions`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: "dismiss", tag }),
  });
  if (!(await expectAuthorized(response, "Failed to dismiss suggested tag."))) {
    return;
  }
  const meme = await response.json();
  updateMemeSuggestedTagsInState(meme.id, meme.suggestedTags || []);
  renderModalAITagSuggestions();
  syncModalSuggestedTagButtonLabel();
  if (!options.silent && modalSuggestTagsStatus) {
    modalSuggestTagsStatus.textContent = `Dismissed ${normalizeTagValue(tag)}.`;
    modalSuggestTagsStatus.classList.remove("hidden");
  }
}

async function applyStoredTagSuggestion(tag) {
  if (!activeMemeId || !canEditMetadata()) {
    return;
  }

  const response = await fetch(`/api/memes/${encodeURIComponent(activeMemeId)}/tag-suggestions`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action: "add", tag }),
  });
  if (!(await expectAuthorized(response, "Failed to apply suggested tag."))) {
    return;
  }
  const meme = await response.json();
  updateMemeSuggestedTagsInState(meme.id, meme.suggestedTags || []);
  addModalTag(tag);
  updateModalSnapshotWithSavedTag(tag);
  renderModalAITagSuggestions();
  syncModalSuggestedTagButtonLabel();
  if (modalSuggestTagsStatus) {
    modalSuggestTagsStatus.textContent = `Added ${normalizeTagValue(tag)}.`;
    modalSuggestTagsStatus.classList.remove("hidden");
  }
}

async function dismissAllStoredTagSuggestions() {
  if (!activeMemeId || !canEditMetadata()) {
    return;
  }
  const activeMeme = getMemeById(activeMemeId);
  const storedSuggestions = Array.isArray(activeMeme?.suggestedTags) ? activeMeme.suggestedTags : [];
  const availableSuggestions = storedSuggestions.filter((tag) => !modalTagState.some((entry) => entry.value === tag));
  if (availableSuggestions.length === 0) {
    return;
  }

  if (modalDismissAllSuggestedTagsButton) {
    modalDismissAllSuggestedTagsButton.disabled = true;
  }

  try {
    for (const tag of availableSuggestions) {
      // eslint-disable-next-line no-await-in-loop
      await dismissStoredTagSuggestion(tag, { silent: true });
    }
    if (modalSuggestTagsStatus) {
      modalSuggestTagsStatus.textContent = `Dismissed ${availableSuggestions.length} remaining suggestion${availableSuggestions.length === 1 ? "" : "s"}.`;
      modalSuggestTagsStatus.classList.remove("hidden");
    }
  } finally {
    if (modalDismissAllSuggestedTagsButton) {
      modalDismissAllSuggestedTagsButton.disabled = !canEditMetadata();
    }
    renderModalAITagSuggestions();
  }
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
    removeButton.textContent = "×";
    removeButton.addEventListener("click", (event) => {
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
  renderTagSuggestionList(uploadTagsInput, uploadTagSuggestions, uploadSuggestionState, activeUploadSuggestionIndex,
    (index) => { activeUploadSuggestionIndex = index; }, addUploadTag);
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

function syncLinkUploadTagsField() {
  linkUploadTagsHidden.value = linkUploadTagState.map((tag) => tag.value).join(", ");
}

function resetLinkUploadTags() {
  linkUploadTagState = [];
  linkUploadSuggestionState = [];
  activeLinkUploadSuggestionIndex = -1;
  if (linkUploadTagSuggestionAbortController) {
    linkUploadTagSuggestionAbortController.abort();
    linkUploadTagSuggestionAbortController = null;
  }
  linkUploadTagsInput.value = "";
  syncLinkUploadTagsField();
  renderLinkUploadTagEditor();
  renderLinkUploadTagSuggestions();
}

function addLinkUploadTag(rawTag) {
  const tag = normalizeTagValue(rawTag);
  if (!tag || linkUploadTagState.some((entry) => entry.value === tag)) return;
  linkUploadTagState = [...linkUploadTagState, createModalTag(tag)];
  linkUploadTagsInput.value = "";
  linkUploadSuggestionState = [];
  activeLinkUploadSuggestionIndex = -1;
  syncLinkUploadTagsField();
  renderLinkUploadTagEditor();
  renderLinkUploadTagSuggestions();
}

function removeLinkUploadTag(tagIdToRemove) {
  linkUploadTagState = linkUploadTagState.filter((tag) => tag.id !== tagIdToRemove);
  if (!normalizeTagValue(linkUploadTagsInput.value)) {
    linkUploadSuggestionState = [];
  }
  syncLinkUploadTagsField();
  renderLinkUploadTagEditor();
  renderLinkUploadTagSuggestions();
}

function renderLinkUploadTagEditor() {
  linkUploadTagChips.innerHTML = "";
  for (const tag of linkUploadTagState) {
    const chip = document.createElement("span");
    chip.className = "tag-token";

    const label = document.createElement("span");
    label.className = "tag-token-label";
    label.textContent = tag.value;

    const removeButton = document.createElement("button");
    removeButton.type = "button";
    removeButton.className = "tag-token-remove";
    removeButton.setAttribute("aria-label", `Remove ${tag.value}`);
    removeButton.textContent = "×";
    removeButton.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      removeLinkUploadTag(tag.id);
      linkUploadTagsInput.focus();
    });

    chip.append(label, removeButton);
    linkUploadTagChips.appendChild(chip);
  }
}

function renderLinkUploadTagSuggestions() {
  renderTagSuggestionList(linkUploadTagsInput, linkUploadTagSuggestions, linkUploadSuggestionState, activeLinkUploadSuggestionIndex,
    (index) => { activeLinkUploadSuggestionIndex = index; }, addLinkUploadTag);
}

async function fetchLinkUploadTagSuggestions() {
  if (!canAdd()) return;
  const needle = normalizeTagValue(linkUploadTagsInput.value);
  if (!needle) {
    if (linkUploadTagSuggestionAbortController) {
      linkUploadTagSuggestionAbortController.abort();
      linkUploadTagSuggestionAbortController = null;
    }
    linkUploadSuggestionState = [];
    renderLinkUploadTagSuggestions();
    return;
  }

  if (linkUploadTagSuggestionAbortController) {
    linkUploadTagSuggestionAbortController.abort();
  }

  linkUploadTagSuggestionAbortController = new AbortController();

  try {
    const response = await fetch(`/api/tags?q=${encodeURIComponent(needle)}`, {
      signal: linkUploadTagSuggestionAbortController.signal,
    });
    if (!response.ok) {
      return;
    }

    const payload = await response.json();
    if (normalizeTagValue(linkUploadTagsInput.value) !== needle) {
      return;
    }
    linkUploadSuggestionState = (payload.tags || []).filter((tag) => !linkUploadTagState.some((entry) => entry.value === tag));
    renderLinkUploadTagSuggestions();
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

  let updatedMeme = null;
  try {
    updatedMeme = await response.json();
  } catch (error) {
    updatedMeme = null;
  }

  if (updatedMeme?.id) {
    updateMemeInState(updatedMeme);
    queueRenderLoadedMemes({ force: true });
    syncMemeGridObserver();
  }

  loadInitialMemes().catch((error) => {
    console.error(error);
  });

  showToast("Saved meme updates.", "success", { title: "Meme" });

  return true;
}

async function copyShareText(value) {
  const shareText = String(value || "").trim();
  if (!shareText) throw new Error("share URL missing");
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(shareText);
      return;
    } catch (error) {
      console.warn("Clipboard API unavailable; trying browser copy fallback.", error);
    }
  }
  const input = document.createElement("textarea");
  input.value = shareText;
  input.setAttribute("readonly", "");
  input.style.position = "fixed";
  input.style.opacity = "0";
  document.body.appendChild(input);
  input.focus();
  input.select();
  input.setSelectionRange?.(0, input.value.length);
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } finally {
    input.remove();
  }
  if (!copied) throw new Error("clipboard unavailable");
}

function showShareCopiedFeedback(button) {
  if (!button) return;
  const label = button.querySelector(".modal-action-label, .random-reel-nav-label");
  const originalLabel = label?.textContent || "Share";
  button.classList.add("share-copied");
  button.setAttribute("aria-label", "Share link copied");
  button.setAttribute("data-tooltip", "Copied to Clipboard");
  if (label) label.textContent = "Copied!";

  const dialog = button.closest("dialog");
  if (dialog) {
    dialog.querySelector(".share-copy-confirmation")?.remove();
    const confirmation = document.createElement("div");
    confirmation.className = "share-copy-confirmation";
    confirmation.setAttribute("role", "status");
    confirmation.setAttribute("aria-live", "assertive");
    confirmation.innerHTML = `<span aria-hidden="true">&#10003;</span><strong>Share link copied</strong><small>Ready to paste anywhere</small>`;
    dialog.appendChild(confirmation);
    window.setTimeout(() => confirmation.remove(), 2600);
  }

  window.setTimeout(() => {
    button.classList.remove("share-copied");
    button.setAttribute("aria-label", "Share meme");
    button.setAttribute("data-tooltip", "Share Meme");
    if (label) label.textContent = originalLabel;
  }, 2400);
}

async function shareMeme(id, button) {
  if (!canView() || !id || button?.disabled) return false;
  if (button) button.disabled = true;
  try {
    const response = await fetch(`/api/memes/${encodeURIComponent(id)}/share`, { method: "POST" });
    if (!(await expectAuthorized(response, "Could not create a share link."))) return false;
    const payload = await response.json();
    const shareURL = String(payload.page_url || payload.url || "");
    if (!shareURL) throw new Error("share URL missing");
    await copyShareText(shareURL);
    showShareCopiedFeedback(button);
    showToast("30-day share link copied.", "success", { title: "Share", duration: 3200 });
    return true;
  } catch (error) {
    console.error(error);
    showToast("The share link could not be copied.", "error", { title: "Share" });
    return false;
  } finally {
    if (button) button.disabled = !canView();
  }
}

function deepLinkedMemeID() {
  const match = window.location.pathname.match(/^\/m\/([^/]+)$/);
  if (!match) return "";
  try {
    return decodeURIComponent(match[1]);
  } catch (error) {
    return "";
  }
}

function clearMemeDeepLinkURL() {
  if (!deepLinkedMemeID()) return;
  window.history.replaceState({}, "", "/");
}

async function openDeepLinkedMeme() {
  const id = deepLinkedMemeID();
  if (!id) return;
  const response = await fetch(`/api/memes/${encodeURIComponent(id)}`);
  if (!(await expectAuthorized(response, "Could not open the shared meme."))) {
    clearMemeDeepLinkURL();
    return;
  }
  const meme = await response.json();
  openModalWithMeme(upsertMemeInState(meme) || meme);
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
    showToast("Delete request sent for approval.", "info", { title: "Meme", duration: 3000 });
    return;
  }

  closeModal();
  await loadInitialMemes();
  showToast("Deleted meme.", "success", { title: "Meme" });
}

uploadForm.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!canAdd()) {
    uploadStatus.textContent = "You do not have permission to upload.";
    return;
  }
  const totalFiles = uploadFileInput.files?.length || 0;
  if (totalFiles === 0) {
    uploadStatus.textContent = "Choose at least one file first.";
    return;
  }

  const progressMessage = totalFiles > 1 ? `Uploading ${totalFiles} files...` : "Uploading...";
  uploadStatus.textContent = progressMessage;
  setModalBusyState(uploadModal, uploadBusyOverlay, uploadBusyMessage, true, progressMessage);
  syncUploadTagsField();

  try {
    const formData = new FormData(uploadForm);
    const response = await fetch("/api/memes", {
      method: "POST",
      body: formData,
    });

    if (response.status === 401 || response.status === 403) {
      if (!(await expectAuthorized(response, "Upload failed."))) {
        uploadStatus.textContent = "Upload failed.";
        return;
      }
    }

    if (!response.ok) {
      uploadStatus.textContent = await readAPIErrorMessage(response, "Upload failed.");
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
    showToast(uploadStatus.textContent || "Upload complete.", "success", { title: "Upload", duration: 3400 });
    await loadInitialMemes();
    uploadModal.close();
  } finally {
    setModalBusyState(uploadModal, uploadBusyOverlay, uploadBusyMessage, false);
  }
});

linkUploadForm?.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!canAdd()) {
    linkUploadStatus.textContent = "You do not have permission to upload.";
    return;
  }

  const sourceURL = linkUploadInput?.value?.trim() || "";
  if (!sourceURL) {
    linkUploadStatus.textContent = "Paste a supported link first.";
    return;
  }

  linkUploadStatus.textContent = "Processing link and saving it...";
  setModalBusyState(linkUploadModal, linkUploadBusyOverlay, linkUploadBusyMessage, true, "Processing link and saving it...");
  try {
    syncLinkUploadTagsField();
    const formData = new FormData(linkUploadForm);
    const response = await fetch("/api/memes", {
      method: "POST",
      body: formData,
    });

    if (response.status === 401 || response.status === 403) {
      if (!(await expectAuthorized(response, "Link import failed."))) {
        linkUploadStatus.textContent = "Link import failed.";
        return;
      }
    }

    if (!response.ok) {
      linkUploadStatus.textContent = await readAPIErrorMessage(response, "Link import failed.");
      return;
    }

    const payload = await response.json();
    if (response.status === 202 || payload.queued) {
      const nextAttempt = payload?.job?.next_attempt_at ? formatDateTime(payload.job.next_attempt_at) : "a few minutes";
      linkUploadStatus.textContent = `Link download failed for now. It was added to the retry queue and will try again around ${nextAttempt}.`;
      showToast("Link queued for retry.", "info", { title: "Import", duration: 3600 });
      await fetchAdminLinkRetryStatus().catch((error) => {
        console.error(error);
      });
      linkUploadForm?.reset();
      resetLinkUploadTags();
      linkUploadModal?.close();
      return;
    }
    const createdCount = Number(payload.created || 0);
    const skippedCount = Number(payload.skipped || 0);
    if (createdCount > 0 && skippedCount > 0) {
      linkUploadStatus.textContent = `${createdCount} imported, ${skippedCount} duplicate${skippedCount === 1 ? "" : "s"} skipped.`;
    } else if (createdCount > 0) {
      linkUploadStatus.textContent = "Link processed.";
    } else if (skippedCount > 0) {
      linkUploadStatus.textContent = `${skippedCount} duplicate${skippedCount === 1 ? "" : "s"} skipped.`;
    } else {
      linkUploadStatus.textContent = "Link processed.";
    }
    showToast(linkUploadStatus.textContent || "Link processed.", "success", { title: "Import", duration: 3400 });

    await loadInitialMemes();
    linkUploadForm?.reset();
    resetLinkUploadTags();
    linkUploadModal?.close();
  } finally {
    setModalBusyState(linkUploadModal, linkUploadBusyOverlay, linkUploadBusyMessage, false);
  }
});

function openUploadDialog() {
  if (!canAdd()) return;
  setModalBusyState(uploadModal, uploadBusyOverlay, uploadBusyMessage, false);
  uploadStatus.textContent = "";
  uploadForm.reset();
  resetUploadTags();
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.showModal();
}

function openVideoImportDialog() {
	if (!canAdd()) return;
	setModalBusyState(linkUploadModal, linkUploadBusyOverlay, linkUploadBusyMessage, false);
	linkUploadStatus.textContent = "";
	linkUploadForm?.reset();
	resetLinkUploadTags();
	linkUploadModal?.showModal();
}

function openAddVaultDialog() {
	if (!canAdd()) return;
	addVaultModal?.showModal();
}

openUploadModalButton.addEventListener("click", openAddVaultDialog);

document.querySelectorAll("#top-add-vault, #mobile-add-vault, [data-open-vault]").forEach((button) => button.addEventListener("click", openAddVaultDialog));
addVaultClose?.addEventListener("click", () => addVaultModal.close());

document.querySelectorAll("[data-ingest]").forEach((button) => {
	button.addEventListener("click", () => {
		const mode = button.dataset.ingest;
		if (mode === "upload") {
			addVaultModal.close();
			openUploadDialog();
		} else if (mode === "video") {
			addVaultModal.close();
			openVideoImportDialog();
		}
	});
});

openRandomReelButton?.addEventListener("click", () => {
  openRandomReel().catch((error) => {
    console.error(error);
  });
});

uploadModalClose.addEventListener("click", () => {
  if (isModalBusy(uploadModal)) return;
  setModalBusyState(uploadModal, uploadBusyOverlay, uploadBusyMessage, false);
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.close();
});

linkUploadModalClose?.addEventListener("click", () => {
  if (isModalBusy(linkUploadModal)) return;
  setModalBusyState(linkUploadModal, linkUploadBusyOverlay, linkUploadBusyMessage, false);
  resetLinkUploadTags();
  linkUploadModal?.close();
});

uploadModal.addEventListener("click", (event) => {
  if (isModalBusy(uploadModal)) return;
  if (event.target !== uploadModal) return;
  setModalBusyState(uploadModal, uploadBusyOverlay, uploadBusyMessage, false);
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.close();
});

linkUploadModal?.addEventListener("click", (event) => {
  if (isModalBusy(linkUploadModal)) return;
  if (event.target !== linkUploadModal) return;
  setModalBusyState(linkUploadModal, linkUploadBusyOverlay, linkUploadBusyMessage, false);
  resetLinkUploadTags();
  linkUploadModal.close();
});

uploadModal.addEventListener("cancel", (event) => {
  if (isModalBusy(uploadModal)) {
    event.preventDefault();
    return;
  }
  event.preventDefault();
  setModalBusyState(uploadModal, uploadBusyOverlay, uploadBusyMessage, false);
  clearUploadPreview();
  setUploadDragActive(false);
  uploadDragDepth = 0;
  uploadModal.close();
});

linkUploadModal?.addEventListener("cancel", (event) => {
  if (isModalBusy(linkUploadModal)) {
    event.preventDefault();
    return;
  }
  event.preventDefault();
  setModalBusyState(linkUploadModal, linkUploadBusyOverlay, linkUploadBusyMessage, false);
  linkUploadModal.close();
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
    setUsersStatus("Enter a Discord user ID first.");
    return;
  }

  setUsersStatus("Adding user...");
  if (await submitManagedUserAdd(userID)) {
    if (usersAddID) {
      usersAddID.value = "";
    }
  }
});

adminUsersAddForm?.addEventListener("submit", async (event) => {
  event.preventDefault();
  if (!canManageUsers()) return;

  const userID = adminUsersAddID?.value?.trim();
  if (!userID) {
    setUsersStatus("Enter a Discord user ID first.");
    return;
  }

  setUsersStatus("Adding user...");
  if (await submitManagedUserAdd(userID)) {
    if (adminUsersAddID) {
      adminUsersAddID.value = "";
    }
  }
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
  uploadSuggestionState = [];
  renderUploadTagSuggestions();
  fetchUploadTagSuggestions();
});

uploadTagsInput.addEventListener("keydown", (event) => {
  if (event.isComposing) return;
  if (navigateTagSuggestions(event, uploadTagsInput, uploadTagSuggestions, uploadSuggestionState, activeUploadSuggestionIndex,
    (index) => { activeUploadSuggestionIndex = index; })) return;

  if ((event.key === "Enter" || event.key === ",") && uploadTagsInput.value.trim()) {
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
    dismissTagSuggestions(uploadTagsInput, uploadTagSuggestions, (index) => { activeUploadSuggestionIndex = index; });
  }
});

linkUploadTagsInput?.addEventListener("input", () => {
  activeLinkUploadSuggestionIndex = -1;
  linkUploadSuggestionState = [];
  renderLinkUploadTagSuggestions();
  fetchLinkUploadTagSuggestions();
});

linkUploadTagsInput?.addEventListener("keydown", (event) => {
  if (event.isComposing) return;
  if (navigateTagSuggestions(event, linkUploadTagsInput, linkUploadTagSuggestions, linkUploadSuggestionState, activeLinkUploadSuggestionIndex,
    (index) => { activeLinkUploadSuggestionIndex = index; })) return;

  if ((event.key === "Enter" || event.key === ",") && linkUploadTagsInput.value.trim()) {
    event.preventDefault();
    if (activeLinkUploadSuggestionIndex >= 0 && linkUploadSuggestionState[activeLinkUploadSuggestionIndex]) {
      addLinkUploadTag(linkUploadSuggestionState[activeLinkUploadSuggestionIndex]);
    } else {
      addLinkUploadTag(linkUploadTagsInput.value);
    }
    return;
  }

  if (event.key === "Backspace" && !linkUploadTagsInput.value && linkUploadTagState.length > 0) {
    removeLinkUploadTag(linkUploadTagState[linkUploadTagState.length - 1].id);
  }

  if (event.key === "Escape") {
    dismissTagSuggestions(linkUploadTagsInput, linkUploadTagSuggestions, (index) => { activeLinkUploadSuggestionIndex = index; });
  }
});

tagSearchInput.addEventListener("input", async (event) => {
  activeTopTagSuggestionIndex = -1;
  topTagSuggestionState = [];
  renderTopTagSuggestions();
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
  if (event.isComposing) return;
  if (navigateTagSuggestions(event, tagSearchInput, tagSearchSuggestions, topTagSuggestionState, activeTopTagSuggestionIndex,
    (index) => { activeTopTagSuggestionIndex = index; })) return;

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
    state.filters.tag = "";
    state.filters.view = item.dataset.view || "library";
    loadInitialMemes().catch((error) => {
      console.error(error);
    });
    if (drawerMediaQuery.matches) {
      closeSidebarDrawer();
    }
  });
});

function navigateToView(view) {
	state.filters.tag = "";
	state.filters.view = view || "library";
	loadInitialMemes().catch((error) => console.error(error));
	if (drawerMediaQuery.matches) closeSidebarDrawer();
}

document.querySelectorAll("[data-go-library]").forEach((button) => button.addEventListener("click", () => navigateToView("library")));
document.querySelectorAll("[data-go-view]").forEach((button) => button.addEventListener("click", () => navigateToView(button.dataset.goView)));
dashboardRandomRefresh?.addEventListener("click", () => {
	refreshDashboardRandomItems().catch((error) => {
		console.error(error);
		showToast("Could not shuffle random picks.", "error", { title: "Dashboard" });
	});
});
document.querySelector("[data-admin-back-to-vault]")?.addEventListener("click", () => navigateToView("home"));
document.querySelectorAll("[data-mobile-view]").forEach((button) => button.addEventListener("click", () => navigateToView(button.dataset.mobileView)));
document.querySelectorAll("[data-mobile-drawer]").forEach((button) => button.addEventListener("click", openSidebarDrawer));

filterToggle?.addEventListener("click", () => {
	if (state.filters.view === "home" || state.filters.view === "admin") navigateToView("library");
	const willOpen = filterPanel.classList.contains("hidden");
	filterPanel.classList.toggle("hidden", !willOpen);
	filterToggle.setAttribute("aria-expanded", String(willOpen));
});

document.querySelectorAll("[data-filter-view]").forEach((button) => button.addEventListener("click", () => {
	state.filters.tag = "";
	state.filters.view = button.dataset.filterView || "library";
	loadInitialMemes().catch((error) => console.error(error));
}));

document.querySelector("#clear-search")?.addEventListener("click", () => {
	tagSearchInput.value = "";
	state.filters.query = "";
	state.filters.tag = "";
	loadInitialMemes().catch((error) => console.error(error));
});

librarySort?.addEventListener("change", () => {
	state.library.sort = librarySort.value;
	try { window.localStorage.setItem("memevault.sort", state.library.sort); } catch (error) { /* Optional preference. */ }
	fetchMemes({ page: 0 }).catch((error) => console.error(error));
});

function setLibraryViewMode(mode) {
	state.library.viewMode = mode === "list" ? "list" : "grid";
	try { window.localStorage.setItem("memevault.viewMode", state.library.viewMode); } catch (error) { /* Optional preference. */ }
	renderContentMode();
	queueRenderLoadedMemes({ force: true });
}
gridViewButton?.addEventListener("click", () => setLibraryViewMode("grid"));
listViewButton?.addEventListener("click", () => setLibraryViewMode("list"));

document.addEventListener("keydown", (event) => {
	if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "k") {
		event.preventDefault();
		tagSearchInput.focus();
		return;
	}
	if (event.key === "/" && !["INPUT", "TEXTAREA", "SELECT"].includes(document.activeElement?.tagName)) {
		event.preventDefault();
		tagSearchInput.focus();
	}
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

authAdmin?.addEventListener("click", (event) => {
  event.stopPropagation();
  closeAuthMenu();
  state.filters.view = "admin";
  state.admin.tab = "dashboard";
  showToast("Loading admin dashboard...", "info", { title: "Admin", duration: 1600 });
  loadInitialMemes().catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not load admin workspace.");
    showToast("Could not load admin workspace.", "error", { title: "Admin" });
  });
});

authInstall?.addEventListener("click", () => {
  installMemeIndex().catch((error) => {
    console.error(error);
    showToast("The browser could not start installation. Use Add to Home Screen from its menu.", "error", { title: "Install MemeIndex" });
  });
});

adminTabs.forEach((tab) => {
  tab.addEventListener("click", () => {
    if (!canManageUsers()) {
      return;
    }
    state.filters.view = "admin";
    state.admin.tab = tab.dataset.adminTab || "users";
    showToast(`Loading ${state.admin.tab.replaceAll("-", " ")}...`, "info", { title: "Admin", duration: 1600 });
    loadInitialMemes().catch((error) => {
      console.error(error);
      setAdminViewStatus("Could not load admin workspace.");
      showToast("Could not load admin workspace.", "error", { title: "Admin" });
    });
  });
});

document.querySelector("[data-admin-activity]")?.addEventListener("click", () => {
  if (!canManageUsers()) return;
  state.filters.view = "admin";
  state.admin.tab = "audit-logs";
  loadInitialMemes().catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not load audit logs.");
  });
});

adminPagePrev?.addEventListener("click", () => {
  const pageState = getActiveAdminPageState();
  if (!pageState || pageState.offset <= 0) return;
  pageState.offset = Math.max(0, pageState.offset - pageState.limit);
  showToast("Loading previous admin page...", "info", { title: "Admin", duration: 1500 });
  loadInitialMemes().catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not load admin page.");
    showToast("Could not load admin page.", "error", { title: "Admin" });
  });
});

adminPageNext?.addEventListener("click", () => {
  const pageState = getActiveAdminPageState();
  if (!pageState || !pageState.hasMore) return;
  pageState.offset += pageState.limit;
  showToast("Loading next admin page...", "info", { title: "Admin", duration: 1500 });
  loadInitialMemes().catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not load admin page.");
    showToast("Could not load admin page.", "error", { title: "Admin" });
  });
});

adminTagQueueReset?.addEventListener("click", () => {
  resetAdminTagSuggestions().catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not reset tag suggestions.");
    showToast("Could not reset tag suggestions.", "error", { title: "Tag Queue" });
    if (adminTagQueueReset) {
      adminTagQueueReset.disabled = false;
    }
  });
});

adminBackupExport?.addEventListener("click", () => {
  startAdminBackup().catch((error) => {
    console.error(error);
    adminBackupStatus.textContent = "Could not start the backup task.";
    showToast("Could not start the backup task.", "error", { title: "Backup & Restore" });
    fetchAdminBackupStatus().catch((statusError) => console.error(statusError));
  });
});

adminBackupImport?.addEventListener("click", () => {
  adminBackupFile?.click();
});

adminBackupFile?.addEventListener("change", () => {
  const file = adminBackupFile.files?.[0];
  if (!file) return;
  importPortableBackup(file).catch((error) => {
    console.error(error);
    adminBackupStatus.textContent = "Could not import the backup.";
    showToast("Could not import the backup.", "error", { title: "Backup & Restore" });
    adminBackupImportBusy = false;
    renderAdminBackupStatus();
    adminBackupFile.value = "";
  });
});

adminTagMergeForm?.addEventListener("submit", (event) => {
  event.preventDefault();
  mergeAdminTags(adminTagMergeSource?.value || "", adminTagMergeTarget?.value || "").catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not merge tags.");
    showToast("Could not merge tags.", "error", { title: "Tag Hygiene" });
    if (adminTagMergeSubmit) {
      adminTagMergeSubmit.disabled = false;
    }
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

modalMobileSummary?.addEventListener("click", () => {
  showMemeModalUI(false);
  toggleModalDetailsPanel(true);
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
  memeModal.classList.remove("has-video", "has-audio");
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
  const savedMemeID = activeMemeId;
  const continueAdminReview = adminTagReviewSessionActive && isAdminView() && activeAdminTab() === "tag-review";
  const saved = await persistCard(savedMemeID, {
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
  if (continueAdminReview && !(await completeAdminTagReview(savedMemeID))) {
    return;
  }
  closeModal({ continueAdminReview });
  if (continueAdminReview) {
    await openNextAdminTagReview();
  }
});

randomReelShare?.addEventListener("click", async () => {
  showRandomReelUI();
  if (!randomReelActiveMemeID) return;
  await shareMeme(randomReelActiveMemeID, randomReelShare);
});

modalSuggestTagsButton?.addEventListener("click", () => {
  fetchModalAITagSuggestions().catch((error) => {
    console.error(error);
  });
});

modalDismissAllSuggestedTagsButton?.addEventListener("click", () => {
  dismissAllStoredTagSuggestions().catch((error) => {
    console.error(error);
  });
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
  modalSuggestionState = [];
  renderTagSuggestions();
  fetchTagSuggestions();
});

modalTagsInput.addEventListener("keydown", (event) => {
  if (event.isComposing) return;
  if (navigateTagSuggestions(event, modalTagsInput, modalTagSuggestions, modalSuggestionState, activeSuggestionIndex,
    (index) => { activeSuggestionIndex = index; })) return;

  if ((event.key === "Enter" || event.key === ",") && modalTagsInput.value.trim()) {
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
    dismissTagSuggestions(modalTagsInput, modalTagSuggestions, (index) => { activeSuggestionIndex = index; });
  }
});

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

try {
	state.library.viewMode = window.localStorage.getItem("memevault.viewMode") === "list" ? "list" : "grid";
	state.library.sort = window.localStorage.getItem("memevault.sort") || "newest";
	if (librarySort) librarySort.value = state.library.sort;
} catch (error) {
	state.library.viewMode = "grid";
	state.library.sort = "newest";
}

syncResponsiveSidebar();
syncInstallAction();
syncMobileSearchHeader();

document.addEventListener("scroll", syncMobileSearchHeader, { passive: true });

function checkHomeDashboardForNewMemes() {
  refreshHomeDashboardForNewMemes().catch((error) => console.error(error));
}

window.setInterval(checkHomeDashboardForNewMemes, HOME_DASHBOARD_REFRESH_MS);
window.addEventListener("focus", checkHomeDashboardForNewMemes);
document.addEventListener("visibilitychange", () => {
  if (!document.hidden) checkHomeDashboardForNewMemes();
});

modalShare?.addEventListener("click", async () => {
  if (!activeMemeId) return;
  await shareMeme(activeMemeId, modalShare);
});

window.addEventListener("beforeinstallprompt", (event) => {
  event.preventDefault();
  deferredInstallPrompt = event;
  syncInstallAction();
});

window.addEventListener("appinstalled", () => {
  deferredInstallPrompt = null;
  syncInstallAction();
  showToast("MemeIndex was added to your home screen.", "success", { title: "App Installed", duration: 4200 });
});

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/service-worker.js", { scope: "/" }).catch((error) => {
      console.error("Service worker registration failed", error);
    });
  });
}

fetchAuthSession()
  .then(async () => {
    await loadInitialMemes();
    await openDeepLinkedMeme();
  })
  .catch((error) => {
    console.error(error);
    uploadStatus.textContent = "Could not load existing memes.";
  });

window.addEventListener("resize", () => {
  syncResponsiveSidebar();
  syncMobileSearchHeader();
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
