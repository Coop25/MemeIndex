// app.js holds the state the whole app shares, its page elements, and
// everything that runs when the page loads: event wiring and start-up. The
// functions live in static/js/, one file per area, with any state only that
// area uses. index.html loads those files first, and they only declare things
// (functions, and constants or variables whose starting value runs no code),
// so everything they define exists before this file runs.
//
// Put new functions, and state only one area uses, in that area's file. Put
// shared state, page-element lookups, and anything that must run at load here.
// scripts/app-scripts.test.cjs checks that the area files read nothing but a
// few built-ins while loading, and that this file declares no functions.

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
		appending: false,
		sort: "newest",
		viewMode: "grid",
  },
  admin: {
    tab: "dashboard",
    storage: { data: null, scan: null, selected: new Set(), busy: false, status: "" },
    audit: {
      offset: 0,
      limit: 100,
      total: 0,
      hasMore: false,
    },
    // {user_id, display_name} while Audit Logs is filtered to one user.
    auditActor: null,
    queue: {
      loaded: false,
      offset: 0,
      limit: 25,
      total: 0,
      hasMore: false,
    },
    tagReview: {
      loaded: false,
      offset: 0,
      limit: 50,
      total: 0,
      hasMore: false,
    },
    tagQueueStatus: null,
    linkRetryStatus: null,
    remuxStatus: null,
    backupStatus: null,
    backupIncoming: null,
    migration: null,
    dashboard: null,
    audiobookLibrary: { filter: "", sort: "created_at", dir: "desc", page: 1, data: null, busy: false, status: "" },
    tagHygiene: {
      loaded: false,
      ready: false,
      computedAt: null,
      pairs: [],
      pairTotal: 0,
      pairHasMore: false,
      tags: [],
      tagTotal: 0,
      tagHasMore: false,
      query: "",
      busy: false,
    },
    shares: [],
    sharesPage: { offset: 0, limit: 25, total: 0, hasMore: false },
  },
  auth: {
    enabled: false,
    authenticated: true,
    user: null,
    version: "dev",
    audiobookUploadConcurrency: 4,
    permissions: {
      canView: true,
      canAdd: true,
      canManage: true,
      canViewAudiobooks: true,
      canUploadAudiobooks: true,
      canDeleteAudiobooks: true,
      canManageAudiobookSeries: true,
    },
    logoutURL: "/auth/logout",
  },
  filters: {
    tag: "",
		query: "",
    view: "home",
  },
  audiobooks: {
    items: [],
    loading: false,
    appending: false,
    hasMore: false,
    upload: null,
    player: null,
    filter: "",
    seriesId: "",
    author: "",
    tag: "",
    series: [],
    view: "books",
    manageSeriesId: "",
    stats: null,
    statsHistoryOpenID: "",
    statsHistoryByID: {},
    statsHistoryFailedIDs: new Set(),
  },
};

// ADMIN_TABS is the single source of truth for the admin workspace: which tabs
// exist, how they are grouped in the rail, the header copy shown for each, and
// (via load) how the tab's data is fetched. loadInitialMemes and the tab rail
// renderer both read from here instead of duplicating an entry per tab.
//   superOnly  - tab requires manage-users permission (all currently do, but the
//                flag keeps that explicit and future-proof)
//   panel      - id of the element that becomes the visible tabpanel content
//   sibling    - a closely-related tab surfaced as a quick cross-link
//   badge      - key into adminTabBadgeCounts() for an attention count
const ADMIN_TABS = Object.freeze({
  dashboard: {
    group: "Overview", label: "Dashboard", superOnly: true, panel: "admin-dashboard-panel",
    title: "Archive Dashboard", kicker: "Admin",
    copy: "A quick pulse-check on backlog size, tag coverage, recent uploads, and the tags that define your archive.",
  },
  users: {
    group: "Overview", label: "Users", superOnly: true, panel: "admin-users-panel", sibling: "user-activity",
    title: "User Access", kicker: "Admin",
    copy: "Manage users and permissions from one place.",
  },
  "user-activity": {
    group: "Overview", label: "User Activity", superOnly: true, panel: "admin-user-activity-panel", sibling: "users",
    title: "User Activity", kicker: "Admin",
    copy: "Who uploads, favorites, and listens - and when each account was last active.",
  },
  shares: {
    group: "Overview", label: "Shared Memes", superOnly: true, panel: "admin-view-table",
    title: "Shared Memes", kicker: "Admin",
    copy: "See every meme with active public access, copy its current link, or revoke it immediately.",
  },
  "tag-hygiene": {
    group: "Tags", label: "Tag Hygiene", superOnly: true, panel: "admin-tag-hygiene-panel",
    title: "Tag Hygiene", kicker: "Admin",
    copy: "Review likely misspellings, separator variants, and close tag duplicates, then merge them into cleaner canonical tags.",
  },
  "tag-queue": {
    group: "Tags", label: "Tag Queue", superOnly: true, panel: "admin-tag-queue-panel", sibling: "tag-review",
    title: "Tag Queue", kicker: "Admin",
    copy: "Monitor the background tag suggestion worker and the memes waiting to be processed.",
  },
  "tag-review": {
    group: "Tags", label: "Suggested Tags", superOnly: true, panel: "admin-tag-queue-panel", sibling: "tag-queue", badge: "tagReview",
    title: "Suggested Tags", kicker: "Admin",
    copy: "Review memes with pending AI tag suggestions and open them directly in the edit modal.",
  },
  "link-retries": {
    group: "Imports", label: "Link Retries", superOnly: true, panel: "admin-link-queue-panel", sibling: "rejected-links", badge: "linkRetries",
    title: "Link Retries", kicker: "Admin",
    copy: "Failed link imports waiting for another download attempt.",
  },
  "rejected-links": {
    group: "Imports", label: "Rejected Links", superOnly: true, panel: "admin-link-queue-panel", sibling: "link-retries", badge: "rejectedLinks",
    title: "Rejected Links", kicker: "Admin",
    copy: "Links that exhausted their retry budget and need a manual requeue if you want to try again later.",
  },
  "delete-queue": {
    group: "Operations", label: "Delete Queue", superOnly: true, panel: "admin-view-table", badge: "deleteQueue",
    title: "Delete Requests", kicker: "Admin",
    copy: "Review pending meme deletions, inspect the media, and either keep the meme or approve the delete.",
  },
  "audit-logs": {
    group: "Operations", label: "Audit Logs", superOnly: true, panel: "admin-view-table",
    title: "Activity Log", kicker: "Admin",
    copy: "A full activity log with actor, action, target meme, and quick-open access for review.",
  },
  backup: {
    group: "Operations", label: "Backup & Restore", superOnly: true, panel: "admin-backup-panel",
    title: "Backup & Restore", kicker: "Admin",
    copy: "Move the complete meme library and its database to another MemeIndex Docker instance.",
  },
  storage: {
    group: "Operations", label: "Storage", superOnly: true, panel: "admin-storage-panel",
    title: "Storage", kicker: "Admin",
    copy: "See what is using disk space, find files the database no longer references, and clear abandoned uploads.",
  },
  "audiobook-library": {
    group: "Audiobooks", label: "Library", superOnly: true, panel: "admin-audiobook-library-panel",
    title: "Audiobook Library", kicker: "Admin",
    copy: "Every audiobook on the server with its size, format, metadata gaps, and how much it is listened to.",
  },
  "audiobook-series": {
    group: "Audiobooks", label: "Series & Tags", superOnly: true, panel: "admin-audiobook-series-panel",
    title: "Series & Tags", kicker: "Admin",
    copy: "Rename, merge, and reorder audiobook series, and see which tags audiobooks and restricted accounts depend on.",
  },
  "audiobook-conversion": {
    group: "Audiobooks", label: "MP3 Conversion", superOnly: true, panel: "admin-remux-panel",
    title: "Audiobook Conversion", kicker: "Admin",
    copy: "MP3 audiobooks are copied, without re-encoding, into .m4b files so chapter jumps and seeking land exactly. Books stay playable throughout.",
  },
});
const ADMIN_TAB_IDS = Object.freeze(Object.keys(ADMIN_TABS));
const ADMIN_DEFAULT_TAB = "dashboard";

const uploadForm = document.querySelector("#upload-form");
const uploadStatus = document.querySelector("#upload-status");
const uploadModal = document.querySelector("#upload-modal");
const uploadBusyOverlay = document.querySelector("#upload-busy-overlay");
const uploadBusyMessage = document.querySelector("#upload-busy-message");
const openUploadModalButton = /** @type {HTMLButtonElement} */ (document.querySelector("#open-upload-modal"));
const addVaultModal = document.querySelector("#add-vault-modal");
const addVaultClose = document.querySelector("#add-vault-close");
const homeDashboard = document.querySelector("#home-dashboard");
const dashboardStats = document.querySelector("#dashboard-stats");
const dashboardRecent = document.querySelector("#dashboard-recent");
const dashboardFavorites = document.querySelector("#dashboard-favorites");
const dashboardRandomRefresh = document.querySelector("#dashboard-random-refresh");
const dashboardRecentAll = document.querySelector("#dashboard-recent-all");
const dashboardFavoritesAll = document.querySelector("#dashboard-favorites-all");
const homeSpotlight = document.querySelector("#home-spotlight");
const homeSpotlightBackdrop = document.querySelector("#home-spotlight-backdrop");
const homeSpotlightStage = document.querySelector("#home-spotlight-stage");
const homeSpotlightTitle = document.querySelector("#home-spotlight-title");
const homeSpotlightMeta = document.querySelector("#home-spotlight-meta");
const homeSpotlightTags = document.querySelector("#home-spotlight-tags");
const homeSpotlightOpen = document.querySelector("#home-spotlight-open");
const homeSpotlightFavorite = document.querySelector("#home-spotlight-favorite");
const homeSpotlightShare = document.querySelector("#home-spotlight-share");
const homeSpotlightNext = document.querySelector("#home-spotlight-next");
const homeSpotlightQueue = document.querySelector("#home-spotlight-queue");
const homeSpotlightPrev = document.querySelector("#home-spotlight-prev");
const homeSpotlightNextButton = document.querySelector("#home-spotlight-next-button");
const homeSpotlightPosition = document.querySelector("#home-spotlight-position");
const homeSpotlightAutoplay = document.querySelector("#home-spotlight-autoplay");
const homeSpotlightLoop = document.querySelector("#home-spotlight-loop");
// The single-row sections of the home screen, each fitted edge to edge.
const dashboardRows = [dashboardRecent, dashboardFavorites].filter(Boolean);
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
const authTrigger = /** @type {HTMLButtonElement} */ (document.querySelector("#auth-trigger"));
const authMenu = document.querySelector("#auth-menu");
const authAvatar = /** @type {HTMLImageElement} */ (document.querySelector("#auth-avatar"));
const authAvatarFallback = document.querySelector("#auth-avatar-fallback");
const authName = document.querySelector("#auth-name");
const authRole = document.querySelector("#auth-role");
const authVersion = document.querySelector("#auth-version");
const authVersionValue = document.querySelector("#auth-version-value");
const authAdmin = document.querySelector("#auth-admin");
const authInstall = document.querySelector("#auth-install");
const authLogout = /** @type {HTMLAnchorElement} */ (document.querySelector("#auth-logout"));
// Logout is POST-only on the server (a GET could be triggered by any link a
// user clicks on another site), so the menu link submits it instead of
// navigating to it.
authLogout?.addEventListener("click", async (event) => {
  event.preventDefault();
  try {
    await fetch(authLogout.getAttribute("href") || "/auth/logout", { method: "POST", credentials: "same-origin" });
  } catch (error) {
    console.error(error);
  }
  window.location.href = "/";
});
const navAudiobooksItem = document.querySelector("#nav-audiobooks-item");
const audiobooksView = document.querySelector("#audiobooks-view");
const audiobooksStatus = document.querySelector("#audiobooks-status");
const audiobooksGrid = document.querySelector("#audiobooks-grid");
const audiobooksGridSentinel = document.querySelector("#audiobooks-grid-sentinel");
const audiobooksEmpty = document.querySelector("#audiobooks-empty");
const openAudiobookUploadButton = document.querySelector("#open-audiobook-upload");
const audiobooksSubnav = document.querySelector("#audiobooks-subnav");
const audiobooksSubnavFilterItems = document.querySelectorAll("#audiobooks-subnav [data-audiobook-filter]");
const audiobooksSubnavSeriesButton = document.querySelector("#audiobooks-subnav-series-button");
const audiobooksSubnavStatsButton = document.querySelector("#audiobooks-subnav-stats-button");
const audiobookStatsView = document.querySelector("#audiobook-stats-view");
const audiobookStatsBackButton = document.querySelector("#audiobook-stats-back");
const audiobookStatsTotalTime = document.querySelector("#audiobook-stats-total-time");
const audiobookStatsStreak = document.querySelector("#audiobook-stats-streak");
const audiobookStatsChart = document.querySelector("#audiobook-stats-chart");
const audiobookStatsFinishedList = document.querySelector("#audiobook-stats-finished-list");
const audiobookStatsFinishedSentinel = document.querySelector("#audiobook-stats-finished-sentinel");
const audiobookStatsFinishedEmpty = document.querySelector("#audiobook-stats-finished-empty");
const audiobooksActiveFilterLabel = document.querySelector("#audiobooks-active-filter-label");
const openAudiobookSeriesManagerButton = document.querySelector("#open-audiobook-series-manager");
const audiobookSeriesManageView = document.querySelector("#audiobook-series-manage-view");
const audiobookSeriesManageBackButton = document.querySelector("#audiobook-series-manage-back");
const audiobookSeriesManagerCreateForm = document.querySelector("#audiobook-series-manager-create-form");
const audiobookSeriesManagerNameInput = document.querySelector("#audiobook-series-manager-name-input");
const audiobookSeriesManagerStatus = document.querySelector("#audiobook-series-manager-status");
const audiobookSeriesManagerRailList = document.querySelector("#audiobook-series-manager-rail-list");
const audiobookSeriesManagerEmpty = document.querySelector("#audiobook-series-manager-empty");
const audiobookSeriesManagerSelected = document.querySelector("#audiobook-series-manager-selected");
const audiobookSeriesManagerSelectedName = document.querySelector("#audiobook-series-manager-selected-name");
const audiobookSeriesManagerSelectedCount = document.querySelector("#audiobook-series-manager-selected-count");
const audiobookSeriesManagerShareButton = document.querySelector("#audiobook-series-manager-share");
const audiobookSeriesAddInput = document.querySelector("#audiobook-series-add-input");
const audiobookSeriesAddList = document.querySelector("#audiobook-series-add-list");
const audiobookSeriesSuggestOpenButton = document.querySelector("#audiobook-series-suggest-open");
const audiobookSeriesSuggestions = document.querySelector("#audiobook-series-suggestions");
const audiobookSeriesSuggestCloseButton = document.querySelector("#audiobook-series-suggest-close");
const audiobookSeriesSuggestAllInput = document.querySelector("#audiobook-series-suggest-all");
const audiobookSeriesSuggestRunButton = document.querySelector("#audiobook-series-suggest-run");
const audiobookSeriesSuggestStopButton = document.querySelector("#audiobook-series-suggest-stop");
const audiobookSeriesSuggestStatus = document.querySelector("#audiobook-series-suggest-status");
const audiobookSeriesSuggestList = document.querySelector("#audiobook-series-suggest-list");
const audiobookSeriesManagerBooksList = document.querySelector("#audiobook-series-manager-books");
const audiobookEditModal = document.querySelector("#audiobook-edit-modal");
const audiobookEditForm = document.querySelector("#audiobook-edit-form");
const audiobookEditCoverPreviewImg = document.querySelector("#audiobook-edit-cover-preview-img");
const audiobookEditCoverPreviewFallback = document.querySelector("#audiobook-edit-cover-preview-fallback");
const audiobookEditCoverInput = document.querySelector("#audiobook-edit-cover-input");
const audiobookEditCoverRemoveButton = document.querySelector("#audiobook-edit-cover-remove");
const audiobookEditTitleInput = document.querySelector("#audiobook-edit-title");
const audiobookEditAuthorInput = document.querySelector("#audiobook-edit-author");
const audiobookEditNarratorInput = document.querySelector("#audiobook-edit-narrator");
const audiobookEditTagsInput = document.querySelector("#audiobook-edit-tags-input");
const audiobookEditTagChips = document.querySelector("#audiobook-edit-tag-chips");
const audiobookEditTagSuggestions = document.querySelector("#audiobook-edit-tag-suggestions");
const audiobookEditStatus = document.querySelector("#audiobook-edit-status");
const audiobookEditCancelButton = document.querySelector("#audiobook-edit-cancel");
const audiobookEditSaveButton = document.querySelector("#audiobook-edit-save");
const audiobookUploadModal = document.querySelector("#audiobook-upload-modal");
const audiobookUploadModalClose = document.querySelector("#audiobook-upload-modal-close");
const audiobookUploadForm = document.querySelector("#audiobook-upload-form");
const audiobookUploadFileInput = document.querySelector("#audiobook-upload-file-input");
const audiobookUploadQueueList = document.querySelector("#audiobook-upload-queue");
const audiobookUploadMetadataFields = document.querySelector("#audiobook-upload-metadata-fields");
const audiobookUploadMetadataNote = document.querySelector("#audiobook-upload-metadata-note");
const audiobookUploadTitleInput = document.querySelector("#audiobook-upload-title");
const audiobookUploadAuthorInput = document.querySelector("#audiobook-upload-author");
const audiobookUploadSubmit = document.querySelector("#audiobook-upload-submit");
const audiobookUploadCancel = document.querySelector("#audiobook-upload-cancel");
const audiobookUploadStatus = document.querySelector("#audiobook-upload-status");
const audiobookUploadProgressWrap = document.querySelector("#audiobook-upload-progress-wrap");
const audiobookUploadProgressBar = document.querySelector("#audiobook-upload-progress-bar");
const audiobookUploadProgressLabel = document.querySelector("#audiobook-upload-progress-label");
const audiobookUploadProgressSize = document.querySelector("#audiobook-upload-progress-size");
const audiobookUploadPartsWrap = document.querySelector("#audiobook-upload-parts-wrap");
const audiobookUploadParts = document.querySelector("#audiobook-upload-parts");
const audiobookUploadPartsCaption = document.querySelector("#audiobook-upload-parts-caption");
const audiobookUploadBusyOverlay = document.querySelector("#audiobook-upload-busy-overlay");
const audiobookPlayerModal = document.querySelector("#audiobook-player-modal");
const audiobookPlayerClose = document.querySelector("#audiobook-player-close");
const audiobookPlayerElsewhereBanner = document.querySelector("#audiobook-player-elsewhere-banner");
const audiobookPlayerElsewhereText = document.querySelector("#audiobook-player-elsewhere-text");
const audiobookPlayerElsewhereAction = document.querySelector("#audiobook-player-elsewhere-action");
const audiobookPlayerBottombar = document.querySelector("#audiobook-player-bottombar");
const audiobookPlayerBottombarRow = document.querySelector("#audiobook-player-bottombar-row");
const audiobookPlayerProgressWrap = document.querySelector("#audiobook-player-progress-wrap");
const audiobookPlayerControls = document.querySelector("#audiobook-player-controls");
const audiobookMiniPlayer = document.querySelector("#audiobook-mini-player");
const audiobookMiniPlayerProgressSlot = document.querySelector("#audiobook-mini-player-progress-slot");
const audiobookMiniPlayerControlsSlot = document.querySelector("#audiobook-mini-player-controls-slot");
const audiobookMiniPlayerClose = document.querySelector("#audiobook-mini-player-close");
const audiobookMiniPlayerHandle = document.querySelector("#audiobook-mini-player-handle");
const audiobookMiniPlayerTitle = document.querySelector("#audiobook-mini-player-title");
const audiobookMiniPlayerSubtitle = document.querySelector("#audiobook-mini-player-subtitle");
const audiobookMiniPlayerCover = document.querySelector("#audiobook-mini-player-cover");
const audiobookMiniPlayerCoverFallback = document.querySelector("#audiobook-mini-player-cover-fallback");
const audiobookPlayerFlip = document.querySelector("#audiobook-player-flip");
const audiobookPlayerCover = document.querySelector("#audiobook-player-cover");
const audiobookPlayerCoverFallback = document.querySelector("#audiobook-player-cover-fallback");
const audiobookPlayerMeta = document.querySelector("#audiobook-player-meta");
const audiobookPlayerChaptersToggle = document.querySelector("#audiobook-player-chapters-toggle");
const audiobookPlayerEditButton = document.querySelector("#audiobook-player-edit");
const audiobookPlayerShareButton = document.querySelector("#audiobook-player-share");
const audiobookPlayerChaptersPanel = document.querySelector("#audiobook-player-chapters-panel");
const audiobookPlayerChapters = document.querySelector("#audiobook-player-chapters");
const audiobookPlayerTitle = document.querySelector("#audiobook-player-title");
const audiobookPlayerAuthor = document.querySelector("#audiobook-player-author");
const audiobookPlayerChapterLabel = document.querySelector("#audiobook-player-chapter-label");
const audiobookPlayerProgress = document.querySelector("#audiobook-player-progress");
const audiobookPlayerCurrentTime = document.querySelector("#audiobook-player-current-time");
const audiobookPlayerDuration = document.querySelector("#audiobook-player-duration");
const audiobookPlayerPlayButton = document.querySelector("#audiobook-player-play");
const audiobookPlayerPlayIcon = document.querySelector("#audiobook-player-play-icon");
const audiobookPlayerSkipBack = document.querySelector("#audiobook-player-skip-back");
const audiobookPlayerSkipForward = document.querySelector("#audiobook-player-skip-forward");
const audiobookPlayerSpeed = document.querySelector("#audiobook-player-speed");
const audiobookPlayerVolume = document.querySelector("#audiobook-player-volume");
const audiobookPlayerVolumeWrap = document.querySelector("#audiobook-player-volume-wrap");
const audiobookPlayerVolumeToggle = document.querySelector("#audiobook-player-volume-toggle");
const audiobookPlayerStatus = document.querySelector("#audiobook-player-status");
const audiobookPlayerMarkUnplayed = document.querySelector("#audiobook-player-mark-unplayed");
const audiobookPlayerMarkFinished = document.querySelector("#audiobook-player-mark-finished");
const audiobookPlayerRemaining = document.querySelector("#audiobook-player-remaining");
const audiobookPlayerPrevChapter = document.querySelector("#audiobook-player-prev-chapter");
const audiobookPlayerNextChapter = document.querySelector("#audiobook-player-next-chapter");
const audiobookPlayerBookmarksToggle = document.querySelector("#audiobook-player-bookmarks-toggle");
const audiobookPlayerBookmarksPanel = document.querySelector("#audiobook-player-bookmarks-panel");
const audiobookPlayerBookmarks = document.querySelector("#audiobook-player-bookmarks");
const audiobookPlayerBookmarksEmpty = document.querySelector("#audiobook-player-bookmarks-empty");
const audiobookPlayerAddBookmarkButton = document.querySelector("#audiobook-player-add-bookmark");
const audiobookPlayerSleepTimerButton = document.querySelector("#audiobook-player-sleep-timer");
const audiobookPlayerSleepTimerLabel = document.querySelector("#audiobook-player-sleep-timer-label");
const sleepTimerModal = document.querySelector("#sleep-timer-modal");
const sleepTimerCancelButton = document.querySelector("#sleep-timer-cancel");
const sleepTimerOffButton = document.querySelector("#sleep-timer-off");
const sleepTimerStartButton = document.querySelector("#sleep-timer-start");
const sleepTimerStatus = document.querySelector("#sleep-timer-status");
const sleepTimerValue = document.querySelector("#sleep-timer-value");
const sleepTimerMinusButton = document.querySelector("#sleep-timer-minus");
const sleepTimerPlusButton = document.querySelector("#sleep-timer-plus");
const sleepTimerPresets = document.querySelector("#sleep-timer-presets");
const sleepTimerChapterPreset = document.querySelector("#sleep-timer-preset-chapter");
const audiobookPlayerSleepOverlay = document.querySelector("#audiobook-player-sleep-overlay");
const audiobookPlayerSleepOverlayTime = document.querySelector("#audiobook-player-sleep-overlay-time");
const confirmModal = /** @type {HTMLDialogElement} */ (document.querySelector("#confirm-modal"));
const confirmModalTitle = document.querySelector("#confirm-modal-title");
const confirmModalMessage = document.querySelector("#confirm-modal-message");
const confirmModalConfirmButton = document.querySelector("#confirm-modal-confirm");
const confirmModalCancelButton = document.querySelector("#confirm-modal-cancel");
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
const adminTabList = document.querySelector("#admin-tab-list");
const adminPanelRegion = document.querySelector("#admin-panel-region");
const adminViewStatus = document.querySelector("#admin-view-status");
const adminTabCrosslink = document.querySelector("#admin-tab-crosslink");
const adminTagQueuePanel = document.querySelector("#admin-tag-queue-panel");
const adminTagQueueKicker = document.querySelector("#admin-tag-queue-kicker");
const adminTagQueueTitle = document.querySelector("#admin-tag-queue-title");
const adminTagQueueSummary = document.querySelector("#admin-tag-queue-summary");
const adminTagQueueReset = document.querySelector("#admin-tag-queue-reset");
const adminTagQueueRetryFailed = document.querySelector("#admin-tag-queue-retry-failed");
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
const adminTagHygieneFreshness = document.querySelector("#admin-tag-hygiene-freshness");
const adminTagHygieneSearch = document.querySelector("#admin-tag-hygiene-search");
const adminTagHygienePairsCount = document.querySelector("#admin-tag-hygiene-pairs-count");
const adminTagHygienePairsMore = document.querySelector("#admin-tag-hygiene-pairs-more");
const adminTagHygieneTagsCount = document.querySelector("#admin-tag-hygiene-tags-count");
const adminTagHygieneTagsMore = document.querySelector("#admin-tag-hygiene-tags-more");
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
const adminBackupDownloads = document.querySelector("#admin-backup-downloads");
const adminBackupImport = document.querySelector("#admin-backup-import");
const adminBackupFile = document.querySelector("#admin-backup-file");
const adminBackupDiscard = document.querySelector("#admin-backup-discard");
const adminBackupStatus = document.querySelector("#admin-backup-status");
const adminMigrationSourceCard = document.querySelector("#admin-migration-source-card");
const adminMigrationTokenBox = document.querySelector("#admin-migration-token-box");
const adminMigrationTokenValue = document.querySelector("#admin-migration-token-value");
const adminMigrationTokenCopy = document.querySelector("#admin-migration-token-copy");
const adminMigrationTokenCreate = document.querySelector("#admin-migration-token-create");
const adminMigrationTokenRevoke = document.querySelector("#admin-migration-token-revoke");
const adminMigrationTokenStatus = document.querySelector("#admin-migration-token-status");
const adminMigrationImportCard = document.querySelector("#admin-migration-import-card");
const adminMigrationForm = document.querySelector("#admin-migration-form");
const adminMigrationSource = document.querySelector("#admin-migration-source");
const adminMigrationToken = document.querySelector("#admin-migration-token");
const adminMigrationStart = document.querySelector("#admin-migration-start");
const adminMigrationCancel = document.querySelector("#admin-migration-cancel");
const adminMigrationProgress = document.querySelector("#admin-migration-progress");
const adminMigrationBar = adminMigrationProgress?.querySelector(".admin-migration-bar");
const adminMigrationBarFill = document.querySelector("#admin-migration-bar-fill");
const adminMigrationProgressText = document.querySelector("#admin-migration-progress-text");
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
const libraryToolbar = document.querySelector(".library-toolbar");
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
const modalVolumeValue = document.querySelector("#modal-volume-value");
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
const randomReelProgressWrap = document.querySelector("#random-reel-progress-wrap");
const randomReelCurrentTime = document.querySelector("#random-reel-current-time");
const randomReelProgress = document.querySelector("#random-reel-progress");
const randomReelDuration = document.querySelector("#random-reel-duration");
const randomReelShare = document.querySelector("#random-reel-share");
const randomReelHint = document.querySelector("#random-reel-hint");
const randomReelFavorite = document.querySelector("#random-reel-favorite");
const randomReelPlay = document.querySelector("#random-reel-play");
const randomReelPlayIcon = document.querySelector("#random-reel-play-icon");
const randomReelVolumeWrap = document.querySelector("#random-reel-volume-wrap");
const randomReelVolumeToggle = document.querySelector("#random-reel-volume-toggle");
const randomReelVolumeIcon = document.querySelector("#random-reel-volume-icon");
const randomReelVolume = document.querySelector("#random-reel-volume");
const randomReelVolumeValue = document.querySelector("#random-reel-volume-value");
const randomReelPrev = document.querySelector("#random-reel-prev");
const randomReelNext = document.querySelector("#random-reel-next");
const randomReelClose = document.querySelector("#random-reel-close");

let activeMemeId = null;
let modalSnapshot = null;
let adminTagReviewSessionActive = false;
let modalTagState = [];
let modalSuggestionState = [];
let activeSuggestionIndex = -1;
let uploadTagState = [];
let uploadSuggestionState = [];
let activeUploadSuggestionIndex = -1;
let linkUploadTagState = [];
let linkUploadSuggestionState = [];
let activeLinkUploadSuggestionIndex = -1;
let uploadDragDepth = 0;
let topTagSuggestionState = [];
let activeTopTagSuggestionIndex = -1;
let topTagSearchDebounce = null;
let randomReelActiveMemeID = null;
let randomReelWheelLock = false;
let randomReelWheelTimeout = null;
let randomReelTouchStartY = null;
let randomReelTouchDeltaY = 0;
let randomReelTouchActive = false;
let randomReelTouchBlocked = false;
let adminBackupImportBusy = false;
let deferredInstallPrompt = null;
let adminTagHygieneSearchTimer = null;

const adminTagQueuePoller = createAdminPoller({
  baseInterval: 8000,
  shouldRun: () => isAdminView() && canManageUsers() && ["tag-queue", "tag-review"].includes(activeAdminTab()),
  tick: () => fetchAdminTagQueueStatus(),
});
const adminLinkQueuePoller = createAdminPoller({
  baseInterval: 8000,
  shouldRun: () => isAdminView() && canManageUsers() && ["link-retries", "rejected-links"].includes(activeAdminTab()),
  tick: () => fetchAdminLinkRetryStatus(),
});
const adminBackupPoller = createAdminPoller({
  baseInterval: 3000,
  shouldRun: () => isAdminView() && canManageUsers() && activeAdminTab() === "backup",
  tick: () => fetchAdminBackupStatus(),
});

const adminRemuxPoller = createAdminPoller({
  baseInterval: 3000,
  shouldRun: () => isAdminView() && canManageUsers() && activeAdminTab() === "audiobook-conversion",
  tick: () => fetchAdminRemuxStatus(),
});

const adminPollers = [adminTagQueuePoller, adminLinkQueuePoller, adminBackupPoller, adminRemuxPoller];

document.addEventListener("visibilitychange", () => {
  adminPollers.forEach((poller) => poller.sync());
});
const drawerMediaQuery = window.matchMedia("(max-width: 1100px)");
const modalDetailsDrawerMediaQuery = window.matchMedia("(max-width: 1100px)");
const mobileSearchHeaderMediaQuery = window.matchMedia("(max-width: 760px)");
const HOME_DASHBOARD_REFRESH_MS = 10000;
const MODAL_PROGRESS_SCALE_MAX = 1000;

// The meme modal's and the random reel's control bars (see createMediaControls).
// On a phone their speaker opens a volume popover instead of muting.
const modalMediaControlBar = createMediaControls({
  play: modalPlay,
  playIcon: modalPlayIcon,
  progressWrap: modalProgressWrap,
  progress: modalProgress,
  current: modalCurrentTime,
  duration: modalDuration,
  volumeWrap: modalVolumeWrap,
  volumeToggle: modalVolumeToggle,
  volumeIcon: modalVolumeIcon,
  volume: modalVolume,
  volumeValue: modalVolumeValue,
}, {
  getMedia: () => getModalMediaControlTarget(),
  isCompact: () => mobileSearchHeaderMediaQuery.matches,
  onInteract: () => showMemeModalUI(),
  onSync(media) {
    memeModal?.classList.toggle("has-video", media instanceof HTMLVideoElement);
    memeModal?.classList.toggle("has-audio", media instanceof HTMLAudioElement);
    modalMediaControls?.classList.toggle("hidden", !media);
    // With nothing to play there is no reason to auto-hide the modal's UI.
    if (!media) showMemeModalUI();
  },
});
const randomReelMediaControlBar = createMediaControls({
  play: randomReelPlay,
  playIcon: randomReelPlayIcon,
  playLabel: randomReelPlay?.querySelector(".random-reel-nav-label"),
  progressWrap: randomReelProgressWrap,
  progress: randomReelProgress,
  current: randomReelCurrentTime,
  duration: randomReelDuration,
  volumeWrap: randomReelVolumeWrap,
  volumeToggle: randomReelVolumeToggle,
  volumeIcon: randomReelVolumeIcon,
  volume: randomReelVolume,
  volumeValue: randomReelVolumeValue,
}, {
  getMedia: () => getRandomReelMediaControlTarget(),
  isCompact: () => mobileSearchHeaderMediaQuery.matches,
  onInteract: () => showRandomReelUI(),
});
// Used before the auth session response has loaded, or if the server ever
// omits audiobook_upload_concurrency; the server's own value (admin-tunable
// via the MEMEINDEX_AUDIOBOOK_UPLOAD_CONCURRENCY env var) takes over once
// fetchAuthSession() resolves.
const AUDIOBOOK_UPLOAD_CONCURRENCY_DEFAULT = 4;

class AudiobookUploadCancelled extends Error {}

// Audiobook lists load a chunk at a time as you scroll, the way the meme grid
// pages in: a sentinel just after each list asks for more whenever it nears
// the viewport. The books grid fetches its next page from the server; the
// series gallery and the Stats page's finished list arrive whole, so they
// just draw the next chunk of what's already loaded.
const AUDIOBOOK_RENDER_CHUNK = 48;
const audiobookIncrementalRender = "IntersectionObserver" in window;

let audiobookStatsFinishedLimit = AUDIOBOOK_RENDER_CHUNK;

// ---------------------------------------------------------------------------
// Add-to-series search: filters the already-fetched full library client side
// (no extra network round trip per keystroke) and lets a click add a book to
// the currently selected series straight away - the fast replacement for
// scrolling a whole separate "library" pane.
// ---------------------------------------------------------------------------

let audiobookSeriesAddQuery = "";

// The add list can be the whole library, so it's drawn a chunk at a time and
// the next chunk is appended as the list itself (its own scroll box) nears its
// end. Re-rendering the same list - after adding a book, say - keeps as many
// rows as were already scrolled in.
let audiobookSeriesAddRows = [];
let audiobookSeriesAddShown = 0;

audiobookSeriesAddList?.addEventListener("scroll", () => {
  const list = audiobookSeriesAddList;
  if (audiobookSeriesAddShown >= audiobookSeriesAddRows.length) return;
  if (list.scrollTop + list.clientHeight >= list.scrollHeight - 300) {
    appendAudiobookSeriesAddRows(AUDIOBOOK_RENDER_CHUNK);
  }
}, { passive: true });

// The main grid shows either books or (on the Series page) series tiles.
// Books are paged from the server (see fetchAudiobooks) and every loaded one
// is drawn; the series list arrives whole, so the gallery draws it a chunk
// at a time instead. audiobooksGridLimit is that gallery chunk - it survives
// re-renders while you stay on the Series page and resets once you leave it.
let audiobooksGridLimit = AUDIOBOOK_RENDER_CHUNK;
let audiobookEditPendingCoverFile = null;
let audiobookEditRemoveCoverRequested = false;

// The audiobook edit modal's tag editor - token chips with autocomplete
// against the shared `tags` table, mirroring the meme modal's tag editor
// (modalTagState / renderTagEditor / fetchTagSuggestions) rather than a
// plain comma-separated input.
let audiobookEditTagState = [];
let audiobookEditTagSuggestionState = [];
let audiobookEditActiveTagSuggestionIndex = -1;

class AudiobookUploadForbidden extends Error {}

class AudiobookUploadDuplicate extends Error {}

// --- Player --------------------------------------------------------------

let audiobookAudioEl = null;
let audiobookPlayerProgressDragging = false;
// The item backing the currently open player, kept around so the player's
// own Edit button can open the edit modal without a round trip to the list.
let audiobookPlayerOpenItem = null;
// Set by applyAudiobooksStateFromURL from a shared ?book=<id> link, then
// consumed once by openDeepLinkedAudiobook after the initial audiobooks
// fetch resolves (state.audiobooks.items isn't populated yet at parse time).
let audiobookPendingDeepLinkBookId = "";
// The other device's last-known position, while the banner is showing it -
// null whenever nothing is playing elsewhere for the open audiobook.
let audiobookPlaybackElsewhereState = null;
let audiobookStreamRecovery = null;

// audiobookPlayPending covers the gap before the element itself says it is
// trying to play: "open" while opening a book waits on metadata and the
// saved position before autoplaying, "resume" while a press of play waits on
// the saved-position fetch. The element is still paused through both, so
// without this a press deep into a long book - where the seek and the fetch
// both take a while on a phone - showed nothing at all.
let audiobookPlayPending = null;
// What the picker currently shows; only becomes a timer on "Start timer".
let sleepTimerSelection = { mode: "minutes", minutes: 30 };

// Autoplay and Loop are remembered per browser. Both start on.
const SPOTLIGHT_AUTOPLAY_KEY = "memeindex.spotlightAutoplay";
const SPOTLIGHT_LOOP_KEY = "memeindex.spotlightLoop";

if (homeSpotlightAutoplay) homeSpotlightAutoplay.checked = loadSpotlightToggle(SPOTLIGHT_AUTOPLAY_KEY);
if (homeSpotlightLoop) homeSpotlightLoop.checked = loadSpotlightToggle(SPOTLIGHT_LOOP_KEY);

homeSpotlightStage?.addEventListener("click", (event) => {
	if (homeSpotlightStage.classList.contains("is-playable")) {
		// Tapping the picture itself plays or pauses, like most players; the
		// control bar handles its own clicks.
		if (event.target.closest(".spot-controls")) return;
		const player = spotlightMediaElements()[0];
		if (!player) return;
		if (player.paused) player.play().catch(() => {});
		else player.pause();
		return;
	}
	openSpotlightMeme();
});
homeSpotlightStage?.addEventListener("keydown", (event) => {
	if (homeSpotlightStage.classList.contains("is-playable")) return;
	if (event.key === "Enter" || event.key === " ") {
		event.preventDefault();
		openSpotlightMeme();
	}
});
homeSpotlightOpen?.addEventListener("click", openSpotlightMeme);
homeSpotlightFavorite?.addEventListener("click", async () => {
	const meme = currentSpotlightMeme();
	if (meme) await persistFavorite(meme.id, !meme.favorite);
});
homeSpotlightShare?.addEventListener("click", () => {
	const meme = currentSpotlightMeme();
	if (meme) shareMeme(meme.id, homeSpotlightShare);
});
homeSpotlightPrev?.addEventListener("click", showPreviousSpotlight);
homeSpotlightAutoplay?.addEventListener("change", () => {
	saveSpotlightToggle(SPOTLIGHT_AUTOPLAY_KEY, homeSpotlightAutoplay.checked);
});
homeSpotlightLoop?.addEventListener("change", () => {
	saveSpotlightToggle(SPOTLIGHT_LOOP_KEY, homeSpotlightLoop.checked);
	spotlightMediaElements().forEach((media) => {
		media.loop = homeSpotlightLoop.checked;
	});
});
homeSpotlightNextButton?.addEventListener("click", () => {
	showNextSpotlight().catch((error) => console.warn("Could not show the next random pick", error));
});
// Swiping the stage sideways on a touch screen steps through the picks.
(() => {
	if (!homeSpotlightStage) return;
	let startX = 0;
	let startY = 0;
	let tracking = false;
	homeSpotlightStage.addEventListener("touchstart", (event) => {
		if (event.touches.length !== 1) return;
		// The bottom of a player is its control bar; dragging the seek bar
		// there is not a swipe.
		const bounds = homeSpotlightStage.getBoundingClientRect();
		if (homeSpotlightStage.classList.contains("is-playable") && event.touches[0].clientY > bounds.bottom - 64) {
			tracking = false;
			return;
		}
		startX = event.touches[0].clientX;
		startY = event.touches[0].clientY;
		tracking = true;
	}, { passive: true });
	homeSpotlightStage.addEventListener("touchend", (event) => {
		if (!tracking) return;
		tracking = false;
		const touch = event.changedTouches[0];
		const dx = touch.clientX - startX;
		const dy = touch.clientY - startY;
		if (Math.abs(dx) < 60 || Math.abs(dx) < Math.abs(dy) * 1.5) return;
		if (dx < 0) homeSpotlightNextButton?.click();
		else showPreviousSpotlight();
	}, { passive: true });
})();
// Any other video or sound starting (the editor, a card, an audiobook) stops
// a playing spotlight item, so two never play over each other.
document.addEventListener("play", (event) => {
	if (homeSpotlightStage && event.target instanceof HTMLMediaElement && !homeSpotlightStage.contains(event.target)) {
		pauseSpotlightMedia();
	}
}, true);
// Leaving the home view, or opening the reel over it, stops a playing
// spotlight item.
new MutationObserver(() => {
	const classes = document.body.classList;
	if (!classes.contains("home-mode") || classes.contains("random-reel-open")) pauseSpotlightMedia();
}).observe(document.body, { attributes: true, attributeFilter: ["class"] });

if (dashboardRows.length && typeof ResizeObserver === "function") {
	const dashboardRowObserver = new ResizeObserver(() => scheduleDashboardRecentLayout());
	dashboardRows.forEach((row) => dashboardRowObserver.observe(row));
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
    // The server saves what it can and lists the rest, so name the files that
    // did not make it instead of reporting the whole batch as a success.
    const failedNames = (Array.isArray(payload.failed) ? payload.failed : []).map((entry) => entry?.filename).filter(Boolean);
    if (failedNames.length > 0) {
      const shown = failedNames.slice(0, 3).join(", ") + (failedNames.length > 3 ? `, and ${failedNames.length - 3} more` : "");
      uploadStatus.textContent = `${uploadStatus.textContent} ${failedNames.length} could not be saved: ${shown}.`;
      showToast(uploadStatus.textContent, "error", { title: "Upload", duration: 8000 });
      await loadInitialMemes();
      return;
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
    if (response.status === 202 && payload.pending) {
      linkUploadStatus.textContent = "This one is taking a while. It keeps downloading in the background and will show up in your archive when it finishes.";
      showToast("Still downloading in the background.", "info", { title: "Import", duration: 4200 });
      linkUploadForm?.reset();
      resetLinkUploadTags();
      linkUploadModal?.close();
      return;
    }
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

openAudiobookUploadButton?.addEventListener("click", openAudiobookUploadModal);
audiobookUploadModalClose?.addEventListener("click", () => audiobookUploadModal?.close());
audiobookUploadFileInput?.addEventListener("change", () => {
  setAudiobookUploadQueue(audiobookUploadFileInput.files || []);
});
audiobookUploadForm?.addEventListener("submit", (event) => {
  submitAudiobookUpload(event).catch((error) => console.error(error));
});
audiobookUploadCancel?.addEventListener("click", () => {
  cancelAudiobookUpload().catch((error) => console.error(error));
});

audiobooksSubnavFilterItems.forEach((item) => {
  item.addEventListener("click", () => {
    state.audiobooks.filter = item.dataset.audiobookFilter || "";
    state.audiobooks.seriesId = "";
    state.audiobooks.author = "";
    state.audiobooks.tag = "";
    state.audiobooks.view = "books";
    syncAudiobooksURL({ push: true });
    navigateToView("audiobooks");
  });
});
audiobooksSubnavSeriesButton?.addEventListener("click", () => {
  state.audiobooks.filter = "";
  state.audiobooks.seriesId = "";
  state.audiobooks.author = "";
  state.audiobooks.tag = "";
  state.audiobooks.view = "series";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
});
audiobooksSubnavStatsButton?.addEventListener("click", () => {
  state.audiobooks.filter = "";
  state.audiobooks.seriesId = "";
  state.audiobooks.author = "";
  state.audiobooks.tag = "";
  state.audiobooks.view = "stats";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
});
audiobookStatsBackButton?.addEventListener("click", () => {
  state.audiobooks.view = "books";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
});

openAudiobookSeriesManagerButton?.addEventListener("click", openAudiobookSeriesManager);
audiobookSeriesManagerCreateForm?.addEventListener("submit", (event) => {
  event.preventDefault();
  const name = audiobookSeriesManagerNameInput?.value.trim();
  if (!name) return;
  createAudiobookSeriesEntry(name).then(() => {
    if (audiobookSeriesManagerNameInput) audiobookSeriesManagerNameInput.value = "";
  }).catch((error) => console.error(error));
});
audiobookSeriesManageBackButton?.addEventListener("click", closeAudiobookSeriesManagePage);
audiobookSeriesManagerShareButton?.addEventListener("click", () => {
  const current = state.audiobooks.series.find((s) => s.id === state.audiobooks.manageSeriesId);
  if (current) shareAudiobookSeries(current).catch((error) => console.error(error));
});
audiobookSeriesAddInput?.addEventListener("input", () => {
  audiobookSeriesAddQuery = audiobookSeriesAddInput.value;
  renderAudiobookSeriesAddList();
});
audiobookSeriesSuggestOpenButton?.addEventListener("click", openAudiobookSeriesSuggestions);
audiobookSeriesSuggestCloseButton?.addEventListener("click", closeAudiobookSeriesSuggestions);
audiobookSeriesSuggestRunButton?.addEventListener("click", () => {
  scanAudiobookSeriesSuggestions().catch((error) => console.error(error));
});
audiobookSeriesSuggestStopButton?.addEventListener("click", () => {
  stopAudiobookSeriesSuggestScan();
  renderAudiobookSeriesSuggestions();
});
// A new scan supersedes one already running (its token moves on), so the
// results always match the box as it is now.
audiobookSeriesSuggestAllInput?.addEventListener("change", () => {
  scanAudiobookSeriesSuggestions().catch((error) => console.error(error));
});

audiobookEditForm?.addEventListener("submit", (event) => {
  submitAudiobookEdit(event).catch((error) => console.error(error));
});
audiobookEditCancelButton?.addEventListener("click", () => audiobookEditModal?.close());
audiobookEditCoverInput?.addEventListener("change", () => {
  const file = audiobookEditCoverInput.files?.[0];
  if (!file) return;
  audiobookEditPendingCoverFile = file;
  audiobookEditRemoveCoverRequested = false;
  renderAudiobookEditCoverPreview(URL.createObjectURL(file));
});
audiobookEditCoverRemoveButton?.addEventListener("click", () => {
  audiobookEditPendingCoverFile = null;
  audiobookEditRemoveCoverRequested = true;
  if (audiobookEditCoverInput) audiobookEditCoverInput.value = "";
  renderAudiobookEditCoverPreview("");
});

audiobookEditTagsInput?.addEventListener("input", () => {
  audiobookEditActiveTagSuggestionIndex = -1;
  audiobookEditTagSuggestionState = [];
  renderAudiobookEditTagSuggestions();
  fetchAudiobookEditTagSuggestions();
});
audiobookEditTagsInput?.addEventListener("keydown", (event) => {
  if (event.isComposing) return;
  if (audiobookEditTagsInput && navigateTagSuggestions(event, audiobookEditTagsInput, audiobookEditTagSuggestions, audiobookEditTagSuggestionState, audiobookEditActiveTagSuggestionIndex,
    (index) => { audiobookEditActiveTagSuggestionIndex = index; })) return;

  if ((event.key === "Enter" || event.key === ",") && audiobookEditTagsInput.value.trim()) {
    event.preventDefault();
    if (audiobookEditActiveTagSuggestionIndex >= 0 && audiobookEditTagSuggestionState[audiobookEditActiveTagSuggestionIndex]) {
      addAudiobookEditTag(audiobookEditTagSuggestionState[audiobookEditActiveTagSuggestionIndex]);
    } else {
      addAudiobookEditTag(audiobookEditTagsInput.value);
    }
    return;
  }

  if (event.key === "Backspace" && !audiobookEditTagsInput.value && audiobookEditTagState.length > 0) {
    removeAudiobookEditTag(audiobookEditTagState[audiobookEditTagState.length - 1].id);
    return;
  }

  if (event.key === "Escape" && audiobookEditTagSuggestions) {
    dismissTagSuggestions(audiobookEditTagsInput, audiobookEditTagSuggestions, (index) => { audiobookEditActiveTagSuggestionIndex = index; });
  }
});

audiobookPlayerClose?.addEventListener("click", minimizeAudiobookPlayer);
audiobookPlayerEditButton?.addEventListener("click", () => {
  if (audiobookPlayerOpenItem) openAudiobookEditModal(audiobookPlayerOpenItem);
});
audiobookPlayerShareButton?.addEventListener("click", () => {
  if (audiobookPlayerOpenItem) shareAudiobookItem(audiobookPlayerOpenItem).catch((error) => console.error(error));
});
audiobookPlayerModal?.addEventListener("cancel", (event) => {
  event.preventDefault();
  minimizeAudiobookPlayer();
});
audiobookMiniPlayerClose?.addEventListener("click", (event) => {
  event.stopPropagation();
  closeAudiobookPlayer();
});
audiobookMiniPlayerHandle?.addEventListener("click", (event) => {
  event.stopPropagation();
  const collapsed = audiobookMiniPlayer?.classList.contains("is-collapsed") || false;
  setAudiobookMiniPlayerCollapsed(!collapsed);
});
audiobookMiniPlayer?.addEventListener("click", (event) => {
  if (event.target.closest("input, button")) return;
  expandAudiobookPlayer();
});
audiobookPlayerModal?.addEventListener("click", (event) => {
  if (audiobookPlayerVolumeWrap?.classList.contains("is-expanded") && !audiobookPlayerVolumeWrap.contains(event.target)) {
    setAudiobookVolumeExpanded(false);
  }
});
audiobookPlayerFlip?.addEventListener("click", () => {
  audiobookPlayerFlip.classList.toggle("is-flipped");
});
audiobookPlayerChaptersToggle?.addEventListener("click", () => {
  toggleAudiobookSidePanel(audiobookPlayerChaptersPanel, audiobookPlayerChaptersToggle, audiobookPlayerBookmarksPanel, audiobookPlayerBookmarksToggle);
});
audiobookPlayerBookmarksToggle?.addEventListener("click", () => {
  toggleAudiobookSidePanel(audiobookPlayerBookmarksPanel, audiobookPlayerBookmarksToggle, audiobookPlayerChaptersPanel, audiobookPlayerChaptersToggle);
});
audiobookPlayerPlayButton?.addEventListener("click", () => {
  const audio = getAudiobookAudioElement();
  // Pressing the spinning button again while a press is still fetching the
  // saved position calls that press off, like pausing would.
  if (audio.paused && (audiobookPlayPending === "resume" || audiobookPlayPending === "takeover")) {
    cancelAudiobookPlayRequest();
    return;
  }
  if (audio.paused) {
    // Another device playing this book takes priority over a plain play():
    // pressing play here means "play here instead", not "play here too", so
    // it asks that device to stop rather than starting a second simultaneous
    // playback.
    if (audiobookPlaybackElsewhereState) {
      pauseAudiobookElsewhereAndPlayHere().catch((error) => console.error(error));
    } else {
      resumeAudiobookPlaybackFromSavedPosition().catch((error) => console.error(error));
    }
  } else {
    cancelAudiobookPlayRequest();
    audio.pause();
  }
});
audiobookPlayerSkipBack?.addEventListener("click", () => {
  const audio = getAudiobookAudioElement();
  audio.currentTime = Math.max(0, audio.currentTime - 30);
});
audiobookPlayerSkipForward?.addEventListener("click", () => {
  const audio = getAudiobookAudioElement();
  audio.currentTime = Math.min(audio.duration || audio.currentTime + 30, audio.currentTime + 30);
});
audiobookPlayerPrevChapter?.addEventListener("click", () => jumpAudiobookChapter(-1));
audiobookPlayerNextChapter?.addEventListener("click", () => jumpAudiobookChapter(1));
audiobookPlayerAddBookmarkButton?.addEventListener("click", () => {
  createAudiobookBookmark("").then((bookmark) => {
    if (!bookmark) return;
    showToast("Bookmark added.", "success", { title: "Audiobooks" });
    flashAudiobookActionButton(audiobookPlayerAddBookmarkButton, audiobookPlayerAddBookmarkButton.querySelector("span"), "Saved!");
  }).catch((error) => console.error(error));
});
audiobookPlayerElsewhereAction?.addEventListener("click", () => {
  pauseAudiobookElsewhereAndPlayHere().catch((error) => console.error(error));
});
audiobookPlayerSleepTimerButton?.addEventListener("click", openSleepTimerPicker);
[[sleepTimerMinusButton, -1], [sleepTimerPlusButton, 1]].forEach(([button, direction]) => {
  if (!button) return;
  button.addEventListener("pointerdown", (event) => {
    if (event.button !== 0 || button.disabled) return;
    button.setPointerCapture?.(event.pointerId);
    startSleepTimerRepeat(direction);
  });
  ["pointerup", "pointercancel", "lostpointercapture"].forEach((type) => button.addEventListener(type, stopSleepTimerRepeat));
  // Pointer presses already stepped on pointerdown; detail === 0 means the
  // click came from the keyboard (Enter/Space), which needs its own step.
  button.addEventListener("click", (event) => {
    if (event.detail === 0) stepSleepTimerSelection(direction);
  });
});
sleepTimerPresets?.querySelectorAll(".sleep-timer-preset").forEach((button) => {
  button.addEventListener("click", () => {
    sleepTimerSelection = button.dataset.mode === "chapter"
      ? { mode: "chapter", minutes: sleepTimerSelection.minutes }
      : { mode: "minutes", minutes: Number(button.dataset.minutes) || 30 };
    renderSleepTimerPicker();
  });
});
sleepTimerStartButton?.addEventListener("click", () => {
  startSleepTimer({ ...sleepTimerSelection }).catch((error) => console.error(error));
  sleepTimerModal?.close();
});
sleepTimerModal?.addEventListener("close", stopSleepTimerRepeat);
sleepTimerCancelButton?.addEventListener("click", () => sleepTimerModal?.close());
sleepTimerOffButton?.addEventListener("click", () => {
  cancelSleepTimer();
  sleepTimerModal?.close();
});
audiobookPlayerSpeed?.addEventListener("change", () => {
  const audio = getAudiobookAudioElement();
  audio.playbackRate = Number(audiobookPlayerSpeed.value) || 1;
});
audiobookPlayerMarkUnplayed?.addEventListener("click", async () => {
  const confirmed = await confirmViaModal({
    title: "Mark as not started?",
    message: "Your saved progress will be reset to the beginning. This cannot be undone.",
    confirmLabel: "Reset progress",
  });
  if (!confirmed) return;
  const audio = getAudiobookAudioElement();
  audio.pause();
  setAudiobookPlayerPosition(0).catch((error) => console.error(error));
});
audiobookPlayerMarkFinished?.addEventListener("click", async () => {
  const audio = getAudiobookAudioElement();
  const duration = Number.isFinite(audio.duration) && audio.duration > 0 ? audio.duration : 0;
  if (duration <= 0) {
    showToast("Duration isn't known yet for this audiobook.", "error", { title: "Audiobooks" });
    return;
  }
  const confirmed = await confirmViaModal({
    title: "Mark as finished?",
    message: "Your saved progress will jump to the end of the book. This cannot be undone.",
    confirmLabel: "Mark finished",
  });
  if (!confirmed) return;
  audio.pause();
  setAudiobookPlayerPosition(duration).catch((error) => console.error(error));
});
audiobookPlayerVolume?.addEventListener("input", () => {
  scheduleAudiobookVolumeAutoHide();
  const audio = getAudiobookAudioElement();
  const volume = Number(audiobookPlayerVolume.value) / 100;
  audio.volume = volume;
  audio.muted = volume === 0;
  syncStoredMediaVolumeFromElement(audio);
});
audiobookPlayerVolume?.addEventListener("pointerdown", scheduleAudiobookVolumeAutoHide);
audiobookPlayerVolumeToggle?.addEventListener("click", () => {
  const expanded = audiobookPlayerVolumeWrap?.classList.contains("is-expanded") || false;
  setAudiobookVolumeExpanded(!expanded);
});
// Which chapter a scrub applies to is frozen on the first "input" of a drag
// gesture and reused through to "change", rather than re-derived from
// audio.currentTime each time - that live position keeps moving if playback
// is still going during the drag, so recomputing it could silently retarget
// the whole gesture at a different, wrong chapter by the time the user lets
// go (especially right near a chapter boundary on a slow drag).
let audiobookPlayerDragChapterBounds = null;

audiobookPlayerProgress?.addEventListener("input", () => {
  const audio = audiobookAudioEl;
  if (!audio) return;
  if (!audiobookPlayerProgressDragging) {
    audiobookPlayerDragChapterBounds = getCurrentAudiobookChapterBounds(audio.currentTime);
  }
  audiobookPlayerProgressDragging = true;
  const fraction = Number(audiobookPlayerProgress.value) / 1000;
  const chapterBounds = audiobookPlayerDragChapterBounds;
  if (chapterBounds) {
    const chapterLength = chapterBounds.end - chapterBounds.start;
    const elapsedInChapter = fraction * chapterLength;
    if (audiobookPlayerCurrentTime) audiobookPlayerCurrentTime.textContent = formatMediaTime(elapsedInChapter);
    if (audiobookPlayerDuration) audiobookPlayerDuration.textContent = `-${formatMediaTime(chapterLength - elapsedInChapter)}`;
  } else {
    const duration = Number.isFinite(audio.duration) ? audio.duration : 0;
    if (audiobookPlayerCurrentTime && duration > 0) {
      audiobookPlayerCurrentTime.textContent = formatMediaTime(fraction * duration);
    }
  }
  audiobookPlayerProgress.style.setProperty("--played", `${fraction * 100}%`);
});
audiobookPlayerProgress?.addEventListener("change", () => {
  const audio = getAudiobookAudioElement();
  const fraction = Number(audiobookPlayerProgress.value) / 1000;
  // A "change" with no "input" before it (some keyboard/assistive-tech
  // adjustments) never froze bounds; the slider is still on the chapter
  // scale, so look them up now rather than reading it as a whole-book fraction.
  const chapterBounds = audiobookPlayerProgressDragging ? audiobookPlayerDragChapterBounds : getCurrentAudiobookChapterBounds(audio.currentTime);
  if (chapterBounds) {
    audio.currentTime = chapterBounds.start + fraction * (chapterBounds.end - chapterBounds.start);
  } else {
    const duration = Number.isFinite(audio.duration) ? audio.duration : 0;
    if (duration > 0) audio.currentTime = fraction * duration;
  }
  audiobookPlayerProgressDragging = false;
  audiobookPlayerDragChapterBounds = null;
  updateAudiobookPlayerProgressUI();
});

window.addEventListener("beforeunload", () => {
  if (state.audiobooks.player) {
    saveAudiobookProgressNow();
    sendAudiobookPlaybackStoppedSignal();
  }
});
// beforeunload is not reliable on mobile - iOS Safari in particular often
// skips it entirely when a tab is backgrounded or the app is swiped away, so
// a listener there alone can lose whatever position wasn't already caught by
// the 15s save timer. pagehide fires in those cases too (including bfcache
// navigation) and visibilitychange catches the more common "locked the
// screen/switched apps while still playing" case even earlier, before the
// page is at any risk of being torn down.
window.addEventListener("pagehide", () => {
  if (state.audiobooks.player) {
    saveAudiobookProgressNow();
    sendAudiobookPlaybackStoppedSignal();
  }
});
document.addEventListener("visibilitychange", () => {
  if (document.hidden && state.audiobooks.player) {
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat();
  }
  if (!document.hidden && audiobookStreamRecovery) retryAudiobookStreamNow(audiobookAudioEl);
});
window.addEventListener("online", () => {
  if (audiobookStreamRecovery) retryAudiobookStreamNow(audiobookAudioEl);
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
    if (item.dataset.view === "audiobooks") {
      state.audiobooks.filter = "";
      state.audiobooks.seriesId = "";
      state.audiobooks.author = "";
      state.audiobooks.tag = "";
      state.audiobooks.view = "books";
    }
    loadInitialMemes().catch((error) => {
      console.error(error);
    });
    if (drawerMediaQuery.matches) {
      closeSidebarDrawer();
    }
  });
});

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
  navigateAdminTab(ADMIN_DEFAULT_TAB);
});

authInstall?.addEventListener("click", () => {
  installMemeIndex().catch((error) => {
    console.error(error);
    showToast("The browser could not start installation. Use Add to Home Screen from its menu.", "error", { title: "Install MemeIndex" });
  });
});

// The admin tab rail is rendered by renderAdminTabRail() and wired once through
// setupAdminTabRail() (click + keyboard delegation on #admin-tab-list).

document.querySelector("[data-admin-activity]")?.addEventListener("click", () => {
  navigateAdminTab("audit-logs");
});

adminPagePrev?.addEventListener("click", () => {
  const pageState = getActiveAdminPageState();
  if (!pageState || pageState.offset <= 0) return;
  pageState.offset = Math.max(0, pageState.offset - pageState.limit);
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
  loadInitialMemes().catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not load admin page.");
    showToast("Could not load admin page.", "error", { title: "Admin" });
  });
});

adminTagQueueRetryFailed?.addEventListener("click", async () => {
  adminTagQueueRetryFailed.disabled = true;
  try {
    const response = await fetch("/api/admin/tag-suggestions/retry-failed", { method: "POST" });
    if (!(await expectAuthorized(response, "Failed to retry tag suggestions."))) {
      showToast("Could not retry failed tag suggestions.", "error", { title: "Tag Queue" });
      return;
    }
    const payload = await response.json();
    const reenabled = Number(payload?.reenabled_exhausted || 0);
    const queued = Number(payload?.queued || 0);
    showToast(`Re-enabled ${reenabled} failed meme${reenabled === 1 ? "" : "s"} and queued ${queued}.`, "success", { title: "Tag Queue", duration: 3400 });
    await fetchAdminTagQueueStatus();
  } catch (error) {
    console.error(error);
    showToast("Could not retry failed tag suggestions.", "error", { title: "Tag Queue" });
  } finally {
    adminTagQueueRetryFailed.disabled = false;
  }
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

adminBackupDiscard?.addEventListener("click", async () => {
  const incoming = state.admin.backupIncoming;
  if (!incoming?.staged) return;
  if (!window.confirm(`Discard the ${incoming.received_count} uploaded part(s) of ${incoming.backup_id}? ` +
    "You would need to upload them again to restore this backup.")) {
    return;
  }
  try {
    const response = await fetch("/api/admin/backup/import/parts", { method: "DELETE" });
    if (!(await expectAuthorized(response, "Could not discard the uploaded parts."))) {
      adminBackupStatus.textContent = await readAPIErrorMessage(response, "Could not discard the uploaded parts.");
      return;
    }
    showToast("Uploaded backup parts discarded.", "success", { title: "Backup & Restore" });
    await fetchAdminBackupStatus();
  } catch (error) {
    console.error(error);
    showToast("Could not discard the uploaded parts.", "error", { title: "Backup & Restore" });
  }
});

adminBackupImport?.addEventListener("click", () => {
  adminBackupFile?.click();
});

adminMigrationTokenCreate?.addEventListener("click", () => {
  createAdminMigrationToken().catch((error) => {
    console.error(error);
    showToast("Could not generate a migration token.", "error", { title: "Backup & Restore" });
  });
});

adminMigrationTokenRevoke?.addEventListener("click", async () => {
  if (!window.confirm("Revoke the migration token? A destination using it right now keeps its current transfer, but cannot start a new one.")) return;
  try {
    const response = await fetch("/api/admin/migration/token", { method: "DELETE" });
    if (!(await expectAuthorized(response, "Could not revoke the token."))) return;
    await fetchAdminMigration();
    showToast("Migration token revoked.", "success", { title: "Backup & Restore" });
  } catch (error) {
    console.error(error);
    showToast("Could not revoke the token.", "error", { title: "Backup & Restore" });
  }
});

adminMigrationTokenCopy?.addEventListener("click", async () => {
  const token = adminMigrationTokenValue?.textContent || "";
  if (!token) return;
  try {
    await navigator.clipboard.writeText(token);
    showToast("Token copied.", "success", { title: "Backup & Restore" });
  } catch {
    window.getSelection()?.selectAllChildren(adminMigrationTokenValue);
    showToast("Select the token and copy it manually.", "info", { title: "Backup & Restore" });
  }
});

adminMigrationForm?.addEventListener("submit", (event) => {
  event.preventDefault();
  startAdminMigration().catch((error) => {
    console.error(error);
    adminMigrationProgressText.textContent = "Could not start the migration.";
    showToast("Could not start the migration.", "error", { title: "Backup & Restore" });
  });
});

adminMigrationCancel?.addEventListener("click", async () => {
  if (!window.confirm("Cancel the migration? This server stays as it was before the migration started.")) return;
  try {
    const response = await fetch("/api/admin/migration/import", { method: "DELETE" });
    if (!(await expectAuthorized(response, "Could not cancel the migration."))) {
      showToast(await readAPIErrorMessage(response, "Could not cancel the migration."), "error", { title: "Backup & Restore" });
    }
    await fetchAdminMigration();
  } catch (error) {
    console.error(error);
  }
});

// Check the session with a HEAD request first so an expired login surfaces as
// a handled error, then hand the URL to the browser's own downloader: a part
// can be tens of gigabytes, far too much to buffer through fetch().
adminBackupDownloads?.addEventListener("click", async (event) => {
  const link = event.target.closest("a.admin-backup-download");
  if (!link) return;
  event.preventDefault();
  if (link.dataset.busy === "1") return;
  link.dataset.busy = "1";
  try {
    const response = await fetch(link.href, { method: "HEAD", cache: "no-store" });
    if (!(await expectAuthorized(response, "Could not download the backup."))) {
      return;
    }
    if (!response.ok) {
      adminBackupStatus.textContent = "That backup file is no longer available. Refresh to see the latest backup.";
      showToast("Could not download the backup.", "error", { title: "Backup & Restore" });
      return;
    }
    const temp = document.createElement("a");
    temp.href = link.href;
    temp.download = link.getAttribute("download") || "";
    document.body.appendChild(temp);
    temp.click();
    temp.remove();
  } catch (error) {
    console.error(error);
    adminBackupStatus.textContent = "Could not download the backup.";
    showToast("Could not download the backup.", "error", { title: "Backup & Restore" });
  } finally {
    delete link.dataset.busy;
  }
});

adminBackupFile?.addEventListener("change", () => {
  const files = Array.from(adminBackupFile.files || []);
  if (!files.length) return;
  importPortableBackup(files).catch((error) => {
    console.error(error);
    adminBackupStatus.textContent = "Could not import the backup.";
    showToast("Could not import the backup.", "error", { title: "Backup & Restore" });
    adminBackupImportBusy = false;
    renderAdminBackupStatus();
  }).finally(() => {
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

adminTagHygienePairsMore?.addEventListener("click", () => {
  loadMoreTagHygiene("pairs").catch((error) => console.error(error));
});

adminTagHygieneTagsMore?.addEventListener("click", () => {
  loadMoreTagHygiene("tags").catch((error) => console.error(error));
});

adminTagHygieneSearch?.addEventListener("input", (event) => {
  const value = event.target.value || "";
  window.clearTimeout(adminTagHygieneSearchTimer);
  adminTagHygieneSearchTimer = window.setTimeout(() => setTagHygieneSearch(value), 250);
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
  modalVolumeWrap?.classList.remove("is-expanded");
  syncModalPanelToggle();
  overlayClose.classList.add("hidden");
});

memeModal.addEventListener("click", (event) => {
  if (modalVolumeWrap?.classList.contains("is-expanded") && !modalVolumeWrap.contains(event.target)) {
    modalVolumeWrap.classList.remove("is-expanded");
    syncModalMediaControls();
  }

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

modalPreview?.addEventListener("click", (event) => {
  const media = modalPreview.querySelector("video");
  if (!media || event.target !== media) {
    return;
  }

  showMemeModalUI();
  modalMediaControlBar.togglePlay();
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

randomReelMedia?.addEventListener("click", (event) => {
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
  randomReelMediaControlBar.togglePlay();
});

randomReelStage?.addEventListener("click", (event) => {
  if (randomReelControlsContains(event.target) || randomReelMedia.contains(event.target)) {
    return;
  }
  showRandomReelUI();
  syncRandomReelMediaControls();
});

randomReelModal?.addEventListener("click", (event) => {
  if (randomReelVolumeWrap?.classList.contains("is-expanded") && !randomReelVolumeWrap.contains(event.target)) {
    randomReelVolumeWrap.classList.remove("is-expanded");
    syncRandomReelMediaControls();
  }

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
  modalSave.disabled = true;
  try {
    const saved = await persistCard(savedMemeID, buildMemePatch(modalSnapshot, {
      favorite: modalFavorite.classList.contains("is-active"),
      notes: modalNotesInput.value,
      tags: getModalTagValues(),
    }));
    if (!saved) return;
    // The server merged our tag delta into the meme's current tags, which may
    // include tags someone else added meanwhile - show what was actually saved
    // (unless the user already moved on to another meme).
    if (saved?.id === savedMemeID && activeMemeId === savedMemeID && Array.isArray(saved.tags)) {
      setModalTags(saved.tags);
      renderTagEditor();
    }
    modalSnapshot = {
      favorite: modalFavorite.classList.contains("is-active"),
      notes: modalNotesInput.value,
      tags: [...getModalTagValues()].sort().join(", "),
    };
    if (continueAdminReview && !(await completeAdminTagReview(savedMemeID))) {
      return;
    }
    if (continueAdminReview) {
      closeModal({ continueAdminReview });
      await openNextAdminTagReview();
    } else {
      loadModalAudit(savedMemeID).catch((error) => console.error(error));
    }
  } finally {
    modalSave.disabled = !canEditMetadata();
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

window.addEventListener("popstate", handleModalHistoryPop);
window.addEventListener("popstate", handleSectionHistoryPop);

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("/service-worker.js", { scope: "/" }).catch((error) => {
      console.error("Service worker registration failed", error);
    });
  });
}

fetchAuthSession()
  .then(async () => {
    applyAdminStateFromURL();
    applyAudiobooksStateFromURL();
    if (state.filters.view === "home" && !canView() && canViewAudiobooks()) {
      state.filters.view = "audiobooks";
    }
    if (canViewAudiobooks() && !isAudiobooksView()) {
      fetchAudiobookSeries().catch((error) => console.error(error));
    }
    await loadInitialMemes();
    await openDeepLinkedMeme();
    await openDeepLinkedAudiobook();
    consumeShareTargetResult();
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

contentPanel?.addEventListener("scroll", noteMemeGridUserScroll, { passive: true });
window.addEventListener("scroll", noteMemeGridUserScroll, { passive: true });

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
