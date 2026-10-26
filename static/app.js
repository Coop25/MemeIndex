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
    remuxStatus: null,
    backupStatus: null,
    dashboard: null,
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
    upload: null,
    player: null,
    filter: "",
    seriesId: "",
    author: "",
    tag: "",
    series: [],
    view: "books",
    manageSeriesId: "",
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
    group: "Overview", label: "Users", superOnly: true, panel: "admin-users-panel",
    title: "User Access", kicker: "Admin",
    copy: "Manage users and permissions from one place.",
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
  "audiobook-conversion": {
    group: "Operations", label: "MP3 Conversion", superOnly: true, panel: "admin-remux-panel",
    title: "Audiobook Conversion", kicker: "Admin",
    copy: "MP3 audiobooks are copied, without re-encoding, into .m4b files so chapter jumps and seeking land exactly. Books stay playable throughout.",
  },
});
const ADMIN_TAB_IDS = Object.freeze(Object.keys(ADMIN_TABS));
const ADMIN_TAB_GROUPS = Object.freeze(["Overview", "Tags", "Imports", "Operations"]);
const ADMIN_DEFAULT_TAB = "dashboard";

function isValidAdminTab(tab) {
  return Object.prototype.hasOwnProperty.call(ADMIN_TABS, tab);
}

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
const navAudiobooksItem = document.querySelector("#nav-audiobooks-item");
const audiobooksView = document.querySelector("#audiobooks-view");
const audiobooksStatus = document.querySelector("#audiobooks-status");
const audiobooksGrid = document.querySelector("#audiobooks-grid");
const audiobooksEmpty = document.querySelector("#audiobooks-empty");
const openAudiobookUploadButton = document.querySelector("#open-audiobook-upload");
const audiobooksSubnav = document.querySelector("#audiobooks-subnav");
const audiobooksSubnavFilterItems = document.querySelectorAll("#audiobooks-subnav [data-audiobook-filter]");
const audiobooksSubnavSeriesButton = document.querySelector("#audiobooks-subnav-series-button");
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
const confirmModal = document.querySelector("#confirm-modal");
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
// Tracks whether opening the meme modal paused an in-progress audiobook, so
// closing it only resumes playback it actually interrupted - not an
// audiobook the listener had already paused themselves before or during.
let audiobookPausedForMemeModal = false;
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
const MODAL_HISTORY_STATE_KEY = "memeIndexModal";
let suppressNextModalHistoryPop = false;

function currentModalHistoryState() {
  return window.history.state?.[MODAL_HISTORY_STATE_KEY] || "";
}

function pushModalHistoryState(modalName, url = window.location.href) {
  if (!modalName || currentModalHistoryState() === modalName) return;
  const currentState = window.history.state && typeof window.history.state === "object"
    ? window.history.state
    : {};
  window.history.pushState({ ...currentState, [MODAL_HISTORY_STATE_KEY]: modalName }, "", url);
}

function unwindModalHistoryState(modalName) {
  if (currentModalHistoryState() !== modalName) return;
  suppressNextModalHistoryPop = true;
  window.history.back();
}

function handleModalHistoryPop() {
  if (suppressNextModalHistoryPop) {
    suppressNextModalHistoryPop = false;
    return;
  }

  if (randomReelModal?.open) {
    closeRandomReel({ syncHistory: false });
    return;
  }

  if (memeModal?.open && !closeModal({ syncHistory: false })) {
    // Back already consumed the guard entry. Restore it when the user chooses
    // to keep editing so the next Android Back press is still modal-scoped.
    pushModalHistoryState("meme");
  }
}
let memeGridObserver = null;
let memeGridLoadArmed = true;
let memeGridLastRequestedPage = 0;
let memeGridScrollRevision = 0;
let memeGridLastLoadScrollRevision = 0;
let memeGridRenderFrame = null;
let memePageFetchSequence = 0;
let memePendingPageIndex = 0;
let managedUsersState = [];
let deleteQueueState = [];
let auditLogState = [];
let adminBackupImportBusy = false;
let toastSequence = 0;
let deferredInstallPrompt = null;
const activeToastTimeouts = new Map();
const ADMIN_TAG_HYGIENE_DISMISSED_KEY = "memeindex.adminTagHygieneDismissed";
const ADMIN_TAG_HYGIENE_PAGE_SIZE = 50;
// Timing for re-polling the tag-hygiene report after a change: the server
// recomputes it on a debounced background worker, so the first fetch right after
// a merge can still be a step behind.
const ADMIN_TAG_HYGIENE_RETRY_MS = 1500;
const ADMIN_TAG_HYGIENE_MAX_RETRIES = 20;
const ADMIN_TAG_HYGIENE_SETTLE_MS = [2500, 7000];
let adminTagHygieneFetchToken = 0;
let adminTagHygieneSearchTimer = null;

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

// setAdminPanelLoading dims the current panel and marks it busy instead of
// blanking it, so switching tabs no longer flashes an empty container.
function setAdminPanelLoading(loading) {
  if (!adminPanelRegion) return;
  adminPanelRegion.classList.toggle("is-loading", !!loading);
  adminPanelRegion.setAttribute("aria-busy", loading ? "true" : "false");
}

// adminTabBadgeCounts derives the "needs attention" number shown on a few tabs
// from data the workspace already polls.
function adminTabBadgeCounts() {
  const linkStatus = state.admin.linkRetryStatus || {};
  const queued = Array.isArray(linkStatus.queued) ? linkStatus.queued.length : 0;
  const rejected = Array.isArray(linkStatus.rejected) ? linkStatus.rejected.length : 0;
  return {
    tagReview: Number(state.admin.tagReview?.total || 0) || 0,
    linkRetries: queued,
    rejectedLinks: rejected,
    deleteQueue: Number(state.admin.queue?.total || 0) || 0,
  };
}

let adminTabRailWired = false;

function adminTabButtons() {
  return adminTabList ? Array.from(adminTabList.querySelectorAll(".admin-tab")) : [];
}

function focusAdminTab(tab) {
  const button = adminTabList?.querySelector(`.admin-tab[data-admin-tab="${CSS.escape(tab)}"]`);
  if (button) {
    button.tabIndex = 0;
    button.focus();
  }
}

function moveAdminTabFocus(current, delta) {
  const ids = ADMIN_TAB_IDS.filter((id) => !ADMIN_TABS[id].superOnly || canManageUsers());
  if (!ids.length) return;
  const index = ids.indexOf(current);
  const nextIndex = delta === "home" ? 0
    : delta === "end" ? ids.length - 1
    : (index + delta + ids.length) % ids.length;
  const nextTab = ids[nextIndex];
  adminTabButtons().forEach((button) => { button.tabIndex = button.dataset.adminTab === nextTab ? 0 : -1; });
  focusAdminTab(nextTab);
}

function setupAdminTabRail() {
  if (adminTabRailWired || !adminTabList) return;
  adminTabRailWired = true;

  adminTabList.addEventListener("click", (event) => {
    const button = event.target.closest(".admin-tab");
    if (!button || !adminTabList.contains(button)) return;
    navigateAdminTab(button.dataset.adminTab);
  });

  adminTabList.addEventListener("keydown", (event) => {
    const button = event.target.closest(".admin-tab");
    if (!button) return;
    const current = button.dataset.adminTab;
    switch (event.key) {
      case "ArrowDown":
      case "ArrowRight":
        event.preventDefault();
        moveAdminTabFocus(current, 1);
        break;
      case "ArrowUp":
      case "ArrowLeft":
        event.preventDefault();
        moveAdminTabFocus(current, -1);
        break;
      case "Home":
        event.preventDefault();
        moveAdminTabFocus(current, "home");
        break;
      case "End":
        event.preventDefault();
        moveAdminTabFocus(current, "end");
        break;
      case "Enter":
      case " ":
        event.preventDefault();
        navigateAdminTab(current);
        break;
      default:
        break;
    }
  });
}

// renderAdminTabRail rebuilds the grouped tab rail from ADMIN_TABS, applying APG
// tab semantics (roving tabindex, aria-selected) and attention badges.
function renderAdminTabRail() {
  if (!adminTabList) return;
  setupAdminTabRail();
  const active = activeAdminTab();
  const badges = adminTabBadgeCounts();
  const canManage = canManageUsers();
  const hadFocus = adminTabList.contains(document.activeElement);

  const groups = ADMIN_TAB_GROUPS.map((group) => {
    const tabs = ADMIN_TAB_IDS.filter((id) => ADMIN_TABS[id].group === group && (!ADMIN_TABS[id].superOnly || canManage));
    if (!tabs.length) return "";
    const buttons = tabs.map((id) => {
      const config = ADMIN_TABS[id];
      const isActive = id === active;
      const count = config.badge ? (badges[config.badge] || 0) : 0;
      const badgeMarkup = count > 0
        ? `<span class="admin-tab-badge" aria-hidden="true">${count > 99 ? "99+" : count}</span>`
        : "";
      const label = count > 0 ? `${config.label}, ${count} need attention` : config.label;
      return `
        <button class="admin-tab${isActive ? " is-active" : ""}" type="button" role="tab"
          id="admin-tab-${id}" data-admin-tab="${escapeHTML(id)}"
          aria-controls="admin-panel-region" aria-selected="${isActive ? "true" : "false"}"
          aria-label="${escapeHTML(label)}" tabindex="${isActive ? "0" : "-1"}">
          <span class="admin-tab-label">${escapeHTML(config.label)}</span>${badgeMarkup}
        </button>`;
    }).join("");
    return `<div class="admin-tab-group" role="presentation"><p class="admin-tab-group-label">${escapeHTML(group)}</p>${buttons}</div>`;
  }).join("");

  adminTabList.innerHTML = groups;
  adminTabList.setAttribute(
    "aria-orientation",
    window.matchMedia("(max-width: 760px)").matches ? "horizontal" : "vertical",
  );
  if (!adminTabButtons().some((button) => button.tabIndex === 0)) {
    const first = adminTabButtons()[0];
    if (first) first.tabIndex = 0;
  }
  if (hadFocus) focusAdminTab(active);
}

// ----- Admin workspace URL state -------------------------------------------------
// The admin workspace lives at /?view=admin&tab=<tab> so a reload, bookmark, or
// browser Back/Forward lands on the same tab instead of the vault.

function readAdminTabFromURL() {
  const params = new URLSearchParams(window.location.search);
  if (params.get("view") !== "admin") return null;
  const tab = params.get("tab");
  return isValidAdminTab(tab) ? tab : ADMIN_DEFAULT_TAB;
}

function applyAdminStateFromURL() {
  const tab = readAdminTabFromURL();
  if (!tab) return false;
  if (!canManageUsers()) {
    // URL asked for admin but this session cannot manage users: clean it up.
    window.history.replaceState(window.history.state || {}, "", "/");
    return false;
  }
  state.filters.view = "admin";
  state.admin.tab = tab;
  return true;
}

function syncAdminURL({ push = false } = {}) {
  const inAdmin = isAdminView() && canManageUsers();
  const params = new URLSearchParams(window.location.search);
  const currentlyAdminURL = params.get("view") === "admin";
  if (inAdmin) {
    const desired = `/?view=admin&tab=${encodeURIComponent(activeAdminTab())}`;
    if (window.location.pathname + window.location.search === desired) return;
    const nextState = { ...(window.history.state || {}), adminTab: activeAdminTab() };
    if (push) {
      window.history.pushState(nextState, "", desired);
    } else {
      window.history.replaceState(nextState, "", desired);
    }
  } else if (currentlyAdminURL) {
    window.history.replaceState({ ...(window.history.state || {}) }, "", "/");
  }
}

// ----- Audiobooks URL state -------------------------------------------------
// The audiobooks section lives at /?view=audiobooks, with the sub-view and
// whatever it's scoped to layered on: &sub=series for the series gallery,
// &sub=manage&series=<id> for the drag-and-drop book manager, &series=<id>
// alone for a series's book list (the pre-existing "Share series" link
// shape - kept exactly as is for links already shared), and &filter=<name>
// for Favorites/Started/Finished. Mirrors the admin workspace's URL state
// just above: readAudiobooksStateFromURL/applyAudiobooksStateFromURL for
// startup and Back/Forward, syncAudiobooksURL for keeping the address bar
// honest as the user navigates around.

function readAudiobooksStateFromURL() {
  const params = new URLSearchParams(window.location.search);
  if (params.get("view") !== "audiobooks") return null;
  const seriesId = params.get("series") || "";
  const sub = params.get("sub") || "";
  // "book" is independent of which list view is active underneath - a
  // shared link opens straight to the player, laid over whatever that view
  // ends up being, so it rides along on every branch below.
  const bookId = params.get("book") || "";
  if (sub === "manage") {
    return { view: "manage-books", seriesId, filter: "", author: "", tag: "", bookId };
  }
  if (sub === "series") {
    return { view: "series", seriesId: "", filter: "", author: "", tag: "", bookId };
  }
  if (seriesId) {
    return { view: "books", seriesId, filter: "", author: "", tag: "", bookId };
  }
  const author = params.get("author") || "";
  if (author) {
    return { view: "books", seriesId: "", filter: "", author, tag: "", bookId };
  }
  const tag = params.get("tag") || "";
  if (tag) {
    return { view: "books", seriesId: "", filter: "", author: "", tag, bookId };
  }
  const filter = params.get("filter") || "";
  return { view: "books", seriesId: "", filter: ["favorites", "started", "finished"].includes(filter) ? filter : "", author: "", tag: "", bookId };
}

function applyAudiobooksStateFromURL() {
  const parsed = readAudiobooksStateFromURL();
  if (!parsed) return false;
  if (!canViewAudiobooks()) {
    // URL asked for audiobooks but this session cannot view them: clean it up.
    window.history.replaceState(window.history.state || {}, "", "/");
    return false;
  }
  if (parsed.view === "manage-books" && !canManageAudiobookSeries()) {
    parsed.view = "series";
    parsed.seriesId = "";
  }
  state.filters.view = "audiobooks";
  state.audiobooks.view = parsed.view;
  state.audiobooks.seriesId = parsed.seriesId;
  state.audiobooks.filter = parsed.filter;
  state.audiobooks.author = parsed.author || "";
  state.audiobooks.tag = parsed.tag || "";
  state.audiobooks.manageSeriesId = parsed.view === "manage-books" ? parsed.seriesId : "";
  audiobookPendingDeepLinkBookId = parsed.bookId || "";
  return true;
}

function syncAudiobooksURL({ push = false } = {}) {
  const inAudiobooks = isAudiobooksView() && canViewAudiobooks();
  const params = new URLSearchParams(window.location.search);
  const currentlyAudiobooksURL = params.get("view") === "audiobooks";
  if (inAudiobooks) {
    const qs = new URLSearchParams({ view: "audiobooks" });
    if (state.audiobooks.view === "manage-books") {
      qs.set("sub", "manage");
      if (state.audiobooks.manageSeriesId) qs.set("series", state.audiobooks.manageSeriesId);
    } else if (state.audiobooks.view === "series") {
      qs.set("sub", "series");
    } else if (state.audiobooks.seriesId) {
      qs.set("series", state.audiobooks.seriesId);
    } else if (state.audiobooks.author) {
      qs.set("author", state.audiobooks.author);
    } else if (state.audiobooks.tag) {
      qs.set("tag", state.audiobooks.tag);
    } else if (state.audiobooks.filter) {
      qs.set("filter", state.audiobooks.filter);
    }
    const desired = `/?${qs.toString()}`;
    if (window.location.pathname + window.location.search === desired) return;
    const nextState = { ...(window.history.state || {}) };
    if (push) {
      window.history.pushState(nextState, "", desired);
    } else {
      window.history.replaceState(nextState, "", desired);
    }
  } else if (currentlyAudiobooksURL) {
    window.history.replaceState({ ...(window.history.state || {}) }, "", "/");
  }
}

function navigateAdminTab(tab, { push = true } = {}) {
  if (!isValidAdminTab(tab) || !canManageUsers()) return;
  if (isAdminView() && activeAdminTab() === tab) return;
  state.filters.view = "admin";
  state.admin.tab = tab;
  syncAdminURL({ push });
  loadInitialMemes().catch((error) => {
    console.error(error);
    setAdminViewStatus("Could not load the admin workspace.");
    showToast("Could not load the admin workspace.", "error", { title: "Admin" });
  });
}

// renderAdminTabCrosslink surfaces the "sibling" tab (Tag Queue <-> Suggested
// Tags, Link Retries <-> Rejected Links) as a one-click jump so the pair is not
// something you have to hunt for in the rail.
function renderAdminTabCrosslink() {
  if (!adminTabCrosslink) return;
  const sibling = isAdminView() && canManageUsers() ? ADMIN_TABS[activeAdminTab()]?.sibling : null;
  if (!sibling || !ADMIN_TABS[sibling]) {
    adminTabCrosslink.classList.add("hidden");
    adminTabCrosslink.innerHTML = "";
    return;
  }
  adminTabCrosslink.classList.remove("hidden");
  adminTabCrosslink.innerHTML = `<span>Related:</span> <button type="button" class="text-button" data-admin-crosslink="${escapeHTML(sibling)}">${escapeHTML(ADMIN_TABS[sibling].label)} &rarr;</button>`;
  adminTabCrosslink.querySelector("[data-admin-crosslink]")?.addEventListener("click", () => {
    navigateAdminTab(sibling);
  });
}

// Admin and audiobooks state are resolved together, in one function, rather
// than as two independent popstate listeners: a Back/Forward move between
// those two sections would otherwise have each listener kick off its own
// loadInitialMemes() call against stale state the other is about to
// overwrite, racing two concurrent fetches against whichever view actually
// ends up rendered.
function handleSectionHistoryPop() {
  const tab = readAdminTabFromURL();
  if (tab && canManageUsers()) {
    if (isAdminView() && activeAdminTab() === tab) return;
    state.filters.view = "admin";
    state.admin.tab = tab;
    loadInitialMemes().catch((error) => console.error(error));
    return;
  }

  const audiobooks = readAudiobooksStateFromURL();
  if (audiobooks && canViewAudiobooks()) {
    if (audiobooks.view === "manage-books" && !canManageAudiobookSeries()) {
      audiobooks.view = "series";
      audiobooks.seriesId = "";
    }
    state.filters.view = "audiobooks";
    state.audiobooks.view = audiobooks.view;
    state.audiobooks.seriesId = audiobooks.seriesId;
    state.audiobooks.filter = audiobooks.filter;
    state.audiobooks.author = audiobooks.author || "";
    state.audiobooks.tag = audiobooks.tag || "";
    state.audiobooks.manageSeriesId = audiobooks.view === "manage-books" ? audiobooks.seriesId : "";
    loadInitialMemes().catch((error) => console.error(error));
    return;
  }

  if (isAdminView() || isAudiobooksView()) {
    state.filters.view = "home";
    loadInitialMemes().catch((error) => console.error(error));
  }
}

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

function canViewAudiobooks() {
  return !!state.auth.permissions?.canViewAudiobooks || !!state.auth.permissions?.canUploadAudiobooks || !!state.auth.permissions?.canDeleteAudiobooks || canManageUsers();
}

function canUploadAudiobooks() {
  return !!state.auth.permissions?.canUploadAudiobooks || canManageUsers();
}

function canDeleteAudiobooks() {
  return !!state.auth.permissions?.canDeleteAudiobooks || canManageUsers();
}

function canManageAudiobookSeries() {
  return !!state.auth.permissions?.canManageAudiobookSeries || canManageUsers();
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

function isAudiobooksView() {
  return state.filters.view === "audiobooks";
}

function permissionLabel() {
  if (canManageUsers()) return "Super Admin";
  const permissions = [];
  if (canView()) permissions.push("View");
  if (canUpload()) permissions.push("Upload");
  if (canAddTags()) permissions.push("Add tags");
  if (canRemoveTags()) permissions.push("Remove tags");
  if (canDeleteMemes()) permissions.push("Delete memes");
  if (canViewAudiobooks()) permissions.push("View audiobooks");
  if (canUploadAudiobooks()) permissions.push("Upload audiobooks");
  if (canDeleteAudiobooks()) permissions.push("Delete audiobooks");
  if (canManageAudiobookSeries()) permissions.push("Manage series");
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
  navAudiobooksItem?.classList.toggle("hidden", !canViewAudiobooks());
  audiobooksSubnav?.classList.toggle("hidden", !canViewAudiobooks());
  openAudiobookUploadButton?.classList.toggle("hidden", !canUploadAudiobooks());
  openAudiobookSeriesManagerButton?.classList.toggle("hidden", !canManageAudiobookSeries());
  document.body.classList.toggle("audiobooks-only-access", !canView() && canViewAudiobooks());

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
    if (payload.enabled && payload.authenticated !== false && !permissions.canView && !permissions.canViewAudiobooks) {
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
    audiobookUploadConcurrency:
      Number.isFinite(payload.audiobook_upload_concurrency) && payload.audiobook_upload_concurrency > 0
        ? payload.audiobook_upload_concurrency
        : AUDIOBOOK_UPLOAD_CONCURRENCY_DEFAULT,
    permissions: payload.permissions || {
      canView: false,
      canUpload: false,
      canAddTags: false,
      canRemoveTags: false,
      canDeleteMemes: false,
      canManageUsers: false,
      canViewAudiobooks: false,
      canUploadAudiobooks: false,
      canDeleteAudiobooks: false,
      canManageAudiobookSeries: false,
      canAdd: false,
      canManage: false,
    },
    logoutURL: payload.logout_url || "/auth/logout",
  };

  if (state.auth.enabled && state.auth.authenticated && !state.auth.permissions?.canView && !state.auth.permissions?.canViewAudiobooks) {
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
    if (!state.auth.permissions?.canView && !state.auth.permissions?.canViewAudiobooks) {
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
    const started = status.started_at ? ` Started ${formatDateTimeZoned(status.started_at)}.` : "";
    adminBackupStatus.textContent = `The server is building a backup.${started} You can leave this page and return later.${availableSuffix}`;
    return;
  }
  if (status.state === "ready" && status.download_available) {
    const details = [status.filename || "Backup ready"];
    if (status.size_bytes) details.push(formatSize(Number(status.size_bytes)));
    if (status.completed_at) details.push(`completed ${formatDateTimeZoned(status.completed_at)}`);
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
  adminBackupPoller.sync();
}

async function importPortableBackup(file) {
  if (!file || !adminBackupImport) return;
  const typed = window.prompt(
    `Import ${file.name}?\n\nThis REPLACES every meme and all database data on this server with the contents of the backup. ` +
    `The server automatically snapshots itself first, but the safest undo is your own export.\n\n` +
    `Type REPLACE to confirm:`,
    "",
  );
  if (typed === null) {
    adminBackupFile.value = "";
    return;
  }
  if (typed.trim().toUpperCase() !== "REPLACE") {
    adminBackupFile.value = "";
    adminBackupStatus.textContent = "Import cancelled — confirmation text did not match.";
    showToast("Import cancelled.", "info", { title: "Backup & Restore" });
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

function tagHygieneEndpoint(view, offset, query) {
  const params = new URLSearchParams({
    view,
    offset: String(offset),
    limit: String(ADMIN_TAG_HYGIENE_PAGE_SIZE),
  });
  if (query) {
    params.set("q", query);
  }
  return `/api/admin/tag-hygiene?${params.toString()}`;
}

let adminTagHygieneSettleTimers = [];
function scheduleTagHygieneSettleRefresh() {
  adminTagHygieneSettleTimers.forEach((id) => window.clearTimeout(id));
  adminTagHygieneSettleTimers = ADMIN_TAG_HYGIENE_SETTLE_MS.map((delay) =>
    window.setTimeout(() => {
      if (activeAdminTab() === "tag-hygiene") {
        fetchAdminTagHygiene().catch((error) => console.error(error));
      }
    }, delay),
  );
}

// fetchAdminTagHygiene reloads the first page of both the "likely variants" and
// "tag counts" lists. The report is maintained by a background worker, so when
// it is not ready yet (or when settle is set, right after a merge) this re-polls
// until the worker catches up, up to ADMIN_TAG_HYGIENE_MAX_RETRIES times.
async function fetchAdminTagHygiene({ settle = false, attempt = 0 } = {}) {
  const token = ++adminTagHygieneFetchToken;
  const hy = state.admin.tagHygiene;
  const query = hy.query || "";
  hy.busy = true;

  let pairsPayload;
  let tagsPayload;
  try {
    const [pairsRes, tagsRes] = await Promise.all([
      fetch(tagHygieneEndpoint("pairs", 0, query)),
      fetch(tagHygieneEndpoint("tags", 0, query)),
    ]);
    if (!(await expectAuthorized(pairsRes, "Failed to load tag hygiene tools."))) {
      return null;
    }
    if (!(await expectAuthorized(tagsRes, "Failed to load tag hygiene tools."))) {
      return null;
    }
    pairsPayload = await pairsRes.json();
    tagsPayload = await tagsRes.json();
  } finally {
    if (token === adminTagHygieneFetchToken) {
      hy.busy = false;
    }
  }

  if (token !== adminTagHygieneFetchToken) {
    return state.admin.tagHygiene;
  }

  hy.loaded = true;
  hy.ready = !!(pairsPayload.ready && tagsPayload.ready);
  hy.computedAt = tagsPayload.computed_at || pairsPayload.computed_at || null;
  hy.pairs = Array.isArray(pairsPayload.pairs) ? pairsPayload.pairs : [];
  hy.pairTotal = Number(pairsPayload.pair_total || 0);
  hy.pairHasMore = !!pairsPayload.has_more;
  hy.tags = Array.isArray(tagsPayload.tags) ? tagsPayload.tags : [];
  hy.tagTotal = Number(tagsPayload.tag_total || 0);
  hy.tagHasMore = !!tagsPayload.has_more;
  renderAdminTagHygiene();

  if (!hy.ready && attempt < ADMIN_TAG_HYGIENE_MAX_RETRIES) {
    window.setTimeout(() => {
      if (activeAdminTab() === "tag-hygiene") {
        fetchAdminTagHygiene({ attempt: attempt + 1 }).catch((error) => console.error(error));
      }
    }, ADMIN_TAG_HYGIENE_RETRY_MS);
  } else if (hy.ready && settle) {
    scheduleTagHygieneSettleRefresh();
  }
  return state.admin.tagHygiene;
}

async function loadMoreTagHygiene(kind) {
  const hy = state.admin.tagHygiene;
  const view = kind === "pairs" ? "pairs" : "tags";
  const offset = kind === "pairs" ? hy.pairs.length : hy.tags.length;
  const button = kind === "pairs" ? adminTagHygienePairsMore : adminTagHygieneTagsMore;
  if (button) {
    button.disabled = true;
  }
  try {
    const response = await fetch(tagHygieneEndpoint(view, offset, hy.query || ""));
    if (!(await expectAuthorized(response, "Failed to load more tags."))) {
      return;
    }
    const payload = await response.json();
    if (kind === "pairs") {
      hy.pairs = hy.pairs.concat(Array.isArray(payload.pairs) ? payload.pairs : []);
      hy.pairTotal = Number(payload.pair_total || hy.pairTotal);
      hy.pairHasMore = !!payload.has_more;
    } else {
      hy.tags = hy.tags.concat(Array.isArray(payload.tags) ? payload.tags : []);
      hy.tagTotal = Number(payload.tag_total || hy.tagTotal);
      hy.tagHasMore = !!payload.has_more;
    }
    renderAdminTagHygiene();
  } finally {
    if (button) {
      button.disabled = false;
    }
  }
}

function setTagHygieneSearch(value) {
  const hy = state.admin.tagHygiene;
  const next = String(value || "").trim().toLowerCase();
  if (next === (hy.query || "")) {
    return;
  }
  hy.query = next;
  fetchAdminTagHygiene().catch((error) => console.error(error));
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

// formatDateTimeZoned is formatDateTime plus an explicit timezone abbreviation,
// for admin columns (share expiry, backup timestamps) where the absolute moment
// matters and there is no relative-time context to lean on.
function formatDateTimeZoned(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown time";
  }
  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
      timeZoneName: "short",
    }).format(date);
  } catch (error) {
    return date.toLocaleString();
  }
}

// formatRelativeTime renders "just now" / "5m ago" / "3h ago" / "2d ago" for
// recent moments and falls back to the absolute date beyond a week. Admin feeds
// use this as the visible label and keep the absolute time in a title tooltip.
function formatRelativeTime(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown time";
  }
  const deltaSeconds = Math.round((Date.now() - date.getTime()) / 1000);
  const future = deltaSeconds < 0;
  const seconds = Math.abs(deltaSeconds);
  const frame = (text) => (future ? `in ${text}` : `${text} ago`);
  if (seconds < 45) return future ? "in a moment" : "just now";
  if (seconds < 90) return frame("a minute");
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return frame(`${minutes}m`);
  const hours = Math.round(minutes / 60);
  if (hours < 24) return frame(`${hours}h`);
  const days = Math.round(hours / 24);
  if (days < 7) return frame(`${days}d`);
  return formatDateTime(date);
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
    <div class="admin-health-summary" data-healthy="${healthyCount === health.length}"><strong>${healthyCount === health.length ? "All systems operational" : `${healthyCount} of ${health.length} healthy`}</strong><span title="${escapeHTML(formatDateTimeZoned(dashboard.generated_at))}">Updated ${escapeHTML(formatRelativeTime(dashboard.generated_at))}</span></div>
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
        <span title="${escapeHTML(formatDateTimeZoned(meme.created_at))}">${escapeHTML(formatRelativeTime(meme.created_at))}</span>
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
        <time datetime="${escapeHTML(String(event.created_at || ""))}" title="${escapeHTML(formatDateTimeZoned(event.created_at))}">${escapeHTML(formatRelativeTime(event.created_at))}</time>
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

  if (adminTagHygieneSearch && document.activeElement !== adminTagHygieneSearch) {
    adminTagHygieneSearch.value = report.query || "";
  }

  if (adminTagHygieneFreshness) {
    let freshness = "";
    if (!report.loaded || report.busy) {
      freshness = "Loading the tag hygiene report…";
    } else if (!report.ready) {
      freshness = "Building the tag hygiene report in the background — this refreshes automatically.";
    } else if (report.computedAt && !String(report.computedAt).startsWith("0001")) {
      freshness = `Report updated ${formatRelativeTime(report.computedAt)}. It recomputes automatically as tags change.`;
    }
    adminTagHygieneFreshness.textContent = freshness;
    adminTagHygieneFreshness.hidden = !freshness;
  }

  if (!report.loaded) {
    adminTagHygienePairs.innerHTML = `<p class="users-empty">Loading tag hygiene suggestions...</p>`;
    adminTagHygieneTags.innerHTML = "";
    if (adminTagHygienePairsCount) adminTagHygienePairsCount.textContent = "";
    if (adminTagHygieneTagsCount) adminTagHygieneTagsCount.textContent = "";
    if (adminTagHygienePairsMore) adminTagHygienePairsMore.hidden = true;
    if (adminTagHygieneTagsMore) adminTagHygieneTagsMore.hidden = true;
    return;
  }

  const dismissedPairs = new Set(getDismissedTagHygienePairs());
  const pairs = (Array.isArray(report.pairs) ? report.pairs : []).filter((pair) => {
    return !dismissedPairs.has(tagHygienePairKey(pair.primary || "", pair.candidate || ""));
  });
  if (pairs.length === 0) {
    adminTagHygienePairs.innerHTML = report.ready
      ? `<p class="users-empty">No likely spelling or separator variants found right now.</p>`
      : `<p class="users-empty">Checking for likely variants…</p>`;
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

  if (adminTagHygienePairsCount) {
    adminTagHygienePairsCount.textContent = report.pairTotal
      ? `Showing ${pairs.length} of ${report.pairTotal}`
      : "";
  }
  if (adminTagHygienePairsMore) {
    adminTagHygienePairsMore.hidden = !report.pairHasMore;
  }

  const tags = Array.isArray(report.tags) ? report.tags : [];
  if (tags.length === 0) {
    adminTagHygieneTags.innerHTML = report.query
      ? `<p class="users-empty">No tags match "${escapeHTML(report.query)}".</p>`
      : `<p class="users-empty">No tags found yet.</p>`;
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

  if (adminTagHygieneTagsCount) {
    adminTagHygieneTagsCount.textContent = report.tagTotal
      ? `Showing ${tags.length} of ${report.tagTotal}`
      : "";
  }
  if (adminTagHygieneTagsMore) {
    adminTagHygieneTagsMore.hidden = !report.tagHasMore;
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
  await fetchAdminTagHygiene({ settle: true });
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
  if (!canManageUsers() || !isAdminView()) {
    renderAdminTagQueueStatus();
  }
  adminTagQueuePoller.sync();
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
  if (!canManageUsers() || !isAdminView()) {
    renderAdminLinkRetryStatus();
  }
  adminLinkQueuePoller.sync();
}

// --- Audiobook conversion (MP3 -> M4B remux) -------------------------------

async function fetchAdminRemuxStatus() {
  if (!canManageUsers()) {
    return null;
  }
  const response = await fetch("/api/admin/audiobook-remux/status");
  if (!(await expectAuthorized(response, "Failed to load audiobook conversion status."))) {
    return null;
  }
  state.admin.remuxStatus = (await response.json()) || null;
  renderAdminRemuxStatus();
  return state.admin.remuxStatus;
}

const ADMIN_REMUX_STATE_LABELS = { waiting: "Waiting to start", running: "Converting", finished: "Finished", failed: "Failed", unavailable: "Audiobooks disabled" };
const ADMIN_REMUX_STATUS_LABELS = { pending: "Waiting", converting: "Converting", done: "Done", failed: "Failed", skipped: "Skipped" };

function formatRemuxBytes(bytes) {
  if (!bytes) return "0 MB";
  return bytes >= 1e9 ? `${(bytes / 1e9).toFixed(2)} GB` : `${Math.round(bytes / 1e6)} MB`;
}

function renderAdminRemuxStatus() {
  const panel = document.querySelector("#admin-remux-panel");
  const grid = document.querySelector("#admin-remux-grid");
  const list = document.querySelector("#admin-remux-list");
  const summary = document.querySelector("#admin-remux-summary");
  const bar = document.querySelector("#admin-remux-bar");
  if (!panel || !grid || !list || !summary) return;

  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "audiobook-conversion";
  panel.classList.toggle("hidden", !visible);
  if (!visible) return;

  const status = state.admin.remuxStatus;
  if (!status) {
    summary.textContent = "Loading conversion status...";
    grid.innerHTML = `<p class="users-empty">Loading audiobook conversion status...</p>`;
    list.innerHTML = "";
    return;
  }

  const counts = status.counts || {};
  const items = Array.isArray(status.items) ? status.items : [];
  const toGo = (counts.pending || 0) + (counts.converting || 0);
  summary.textContent = status.bytes_total
    ? `${ADMIN_REMUX_STATE_LABELS[status.state] || status.state} * ${formatRemuxBytes(status.bytes_done)} of ${formatRemuxBytes(status.bytes_total)}`
    : ADMIN_REMUX_STATE_LABELS[status.state] || status.state || "";
  if (bar) {
    const pct = status.bytes_total ? (status.bytes_done / status.bytes_total) * 100 : (status.state === "finished" ? 100 : 0);
    bar.style.width = `${Math.min(100, pct)}%`;
  }

  const stat = (label, value) => `
    <article class="admin-tag-queue-stat">
      <span class="admin-tag-queue-label">${escapeHTML(label)}</span>
      <strong class="admin-tag-queue-value">${escapeHTML(String(value))}</strong>
    </article>`;
  grid.innerHTML = [
    stat("Converted", counts.done || 0),
    stat("Failed or Skipped", (counts.failed || 0) + (counts.skipped || 0)),
    stat("To Go", toGo),
    stat("MP3s Left in Library", status.mp3s_remaining ?? "-"),
  ].join("");

  if (items.length === 0) {
    list.innerHTML = `
      <section class="admin-tag-queue-section">
        <div class="admin-tag-queue-list-head">
          <strong>Books</strong>
          <span>${status.state === "waiting" ? "Waiting for the startup conversion to begin." : "No MP3 audiobooks have needed converting since the server started."}</span>
        </div>
      </section>`;
    return;
  }

  list.innerHTML = `
    <section class="admin-tag-queue-section">
      <div class="admin-tag-queue-list-head">
        <strong>Books</strong>
        <span>Failed books stay on their MP3 and are retried on a server start at least a week later.</span>
      </div>
      <div class="admin-tag-queue-items">
        ${items.map((item) => {
          const parts = [formatRemuxBytes(item.size_before)];
          if (item.status === "done" && item.size_after) parts.push(`to ${formatRemuxBytes(item.size_after)}`);
          if (item.started_at) {
            const end = item.finished_at ? new Date(item.finished_at) : new Date();
            parts.push(formatDurationWords(Math.max(0, Math.round((end - new Date(item.started_at)) / 1000))));
          }
          let pct = 0;
          if (item.status === "converting" && item.size_before) {
            pct = Math.min(100, Math.round((item.bytes_done / item.size_before) * 100));
            parts.push(pct >= 100 ? "finalizing" : `${pct}%`);
          }
          return `
            <article class="admin-tag-queue-item admin-remux-item is-${escapeHTML(item.status || "")}">
              <span class="admin-tag-queue-rank">${escapeHTML(ADMIN_REMUX_STATUS_LABELS[item.status] || item.status || "")}</span>
              <div class="admin-tag-queue-item-copy">
                <strong>${escapeHTML(item.title || item.filename || item.id || "Untitled")}</strong>
                <span>${escapeHTML(parts.join(" * "))}</span>
                ${item.status === "converting" ? `<div class="admin-remux-progress is-item" aria-hidden="true"><i style="width:${pct}%"></i></div>` : ""}
                ${item.error ? `<code>${escapeHTML(item.error)}</code>` : ""}
              </div>
            </article>`;
        }).join("")}
      </div>
    </section>`;
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

function canAppendMemePage({ loading, hasMore, currentPage, requestedPage }) {
  return !loading && hasMore && requestedPage === currentPage + 1;
}

function canRequestMemeAutoPage({
  armed,
  libraryMode,
  loading,
  hasMore,
  currentPage,
  requestedPage,
  lastRequestedPage,
  scrollRevision,
  lastLoadScrollRevision,
}) {
  return (
    armed &&
    libraryMode &&
    requestedPage > lastRequestedPage &&
    scrollRevision > lastLoadScrollRevision &&
    canAppendMemePage({ loading, hasMore, currentPage, requestedPage })
  );
}

function ensureMemeGridObserver() {
  if (memeGridObserver || !memeGridSentinel || !("IntersectionObserver" in window)) {
    return;
  }

  memeGridObserver = new IntersectionObserver((entries) => {
    const entry = entries.find((candidate) => candidate.target === memeGridSentinel);
    if (!entry) {
      return;
    }

    if (!entry.isIntersecting) {
      memeGridLoadArmed = true;
      return;
    }

    const nextPage = state.library.pageIndex + 1;
    const libraryMode = document.body.classList.contains("library-mode");
    if (!canRequestMemeAutoPage({
      armed: memeGridLoadArmed,
      libraryMode,
      loading: state.library.loading,
      hasMore: state.library.hasMore,
      currentPage: state.library.pageIndex,
      requestedPage: nextPage,
      lastRequestedPage: memeGridLastRequestedPage,
      scrollRevision: memeGridScrollRevision,
      lastLoadScrollRevision: memeGridLastLoadScrollRevision,
    })) {
      return;
    }

    // One intersection and one user scroll may request exactly one page. Layout
    // changes alone cannot arm another request.
    memeGridLoadArmed = false;
    memeGridLastRequestedPage = nextPage;
    memeGridLastLoadScrollRevision = memeGridScrollRevision;
    fetchMemes({ page: nextPage, append: true }).catch((error) => {
      memeGridLastRequestedPage = state.library.pageIndex;
      console.error(error);
      setMemeGridStatus("Could not load more memes. Scroll away and back to retry.", false);
    });
  }, {
    root: null,
    rootMargin: "0px 0px 480px 0px",
    threshold: 0,
  });

  memeGridObserver.observe(memeGridSentinel);
}

function syncMemeGridObserver() {
  if (!memeGridSentinel) {
    return;
  }

  const libraryMode = document.body.classList.contains("library-mode");
  const autoPagination = "IntersectionObserver" in window;
  ensureMemeGridObserver();

  memeGridSentinel.classList.toggle(
    "hidden",
    !autoPagination || !libraryMode || (!state.library.hasMore && !state.library.appending),
  );
  memeGridSentinel.classList.toggle("is-loading", state.library.appending);
  memeGridSentinel.textContent = state.library.appending ? "Loading more memes..." : "";
  libraryToolbar?.classList.toggle("hidden", !libraryMode || !canView() || autoPagination);
  syncMemePagination();
  setMemeGridLoading(state.library.loading && !state.library.appending);

  if (state.library.loading) {
    setMemeGridStatus(state.library.appending ? "" : "Loading memes...", state.library.appending);
    return;
  }

  setMemeGridStatus("", true);
}

function renderContentMode() {
  const adminMode = isAdminView();
	const homeMode = state.filters.view === "home";
	const audiobooksMode = isAudiobooksView();
	const manageSeriesBooksMode = audiobooksMode && state.audiobooks.view === "manage-books";
	const libraryMode = !adminMode && !homeMode && !audiobooksMode;
	audiobooksView?.classList.toggle("hidden", !audiobooksMode || manageSeriesBooksMode);
	audiobookSeriesManageView?.classList.toggle("hidden", !manageSeriesBooksMode);
	document.body.classList.toggle("audiobooks-mode", audiobooksMode);
	if (tagSearchInput) {
		tagSearchInput.placeholder = audiobooksMode ? "Search audiobooks…" : "Search memes, tags, or notes…";
	}
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
  renderAdminTabRail();
  renderAdminTabCrosslink();
  if (adminMode && adminPanelRegion) {
    adminPanelRegion.setAttribute("aria-labelledby", `admin-tab-${activeAdminTab()}`);
  }
  adminUsersPanel?.classList.toggle("hidden", !showAdminUsersPanel);
  adminBackupPanel?.classList.toggle("hidden", !showAdminBackupPanel);
  adminViewTable?.classList.toggle("hidden", !usesSharedAdminTable);
  const pageState = getActiveAdminPageState();
  adminPagination?.classList.toggle("hidden", !adminMode || !pageState);
  memeGridLoader?.classList.toggle("hidden", !libraryMode || !state.library.loading || state.library.appending);
  memeGridStatus?.classList.toggle("hidden", !libraryMode || memeGridStatus.textContent === "");
  memeGridTopSpacer?.classList.toggle("hidden", !libraryMode);
  memeGridBottomSpacer?.classList.toggle("hidden", !libraryMode);
  memeGrid?.classList.toggle("hidden", !libraryMode);
  memeGridSentinel?.classList.toggle(
    "hidden",
    !("IntersectionObserver" in window) || !libraryMode || (!state.library.hasMore && !state.library.appending),
  );
  emptyState?.classList.toggle("hidden", !libraryMode || (canBrowseLibrary && state.memes.length !== 0));
  libraryToolbar?.classList.toggle(
    "hidden",
    !libraryMode || !canBrowseLibrary || ("IntersectionObserver" in window),
  );
	document.body.classList.toggle("library-list-view", state.library.viewMode === "list");
	gridViewButton?.classList.toggle("is-active", state.library.viewMode === "grid");
	listViewButton?.classList.toggle("is-active", state.library.viewMode === "list");
	gridViewButton?.setAttribute("aria-pressed", String(state.library.viewMode === "grid"));
	listViewButton?.setAttribute("aria-pressed", String(state.library.viewMode === "list"));
  renderAdminDashboard();
  renderAdminTagHygiene();
  renderAdminTagQueueStatus();
  renderAdminLinkRetryStatus();
  renderAdminRemuxStatus();
  syncAdminTagQueuePolling();
  syncAdminLinkQueuePolling();
  syncAdminBackupPolling();
  adminRemuxPoller.sync();
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

async function fetchMemes({ page = 0, append = false } = {}) {
  const requestedPage = Math.max(0, page);
  if (
    state.library.loading ||
    (append && !canAppendMemePage({
      loading: state.library.loading,
      hasMore: state.library.hasMore,
      currentPage: state.library.pageIndex,
      requestedPage,
    }))
  ) {
    return false;
  }

  if (!append) {
    memeGridLoadArmed = true;
    memeGridLastRequestedPage = requestedPage;
    memeGridLastLoadScrollRevision = memeGridScrollRevision;
  }

  const fetchSequence = ++memePageFetchSequence;
  state.library.loading = true;
  state.library.appending = append;
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
    // The server only recomputes facet counts for the first page of a filter
    // (they are identical for every scroll page); keep the ones we already have
    // when paging further in.
    if (requestedPage === 0 && payload.counts && payload.counts.total !== undefined) {
      state.library.counts = payload.counts;
    }
    state.library.hasMore = !!payload.has_more;
    const fetchedMemes = Array.isArray(payload.memes) ? payload.memes : [];
    let appendedMemes = [];
    if (append) {
      const knownIDs = new Set(state.memes.map((meme) => meme.id));
      appendedMemes = fetchedMemes.filter((meme) => !knownIDs.has(meme.id));
      state.memes = [...state.memes, ...appendedMemes];
    } else {
      state.memes = fetchedMemes;
    }
    emptyState.textContent = "No memes match this view yet.";
    renderMemes({ preserveScroll: append, appendMemes: appendedMemes });
    return true;
  } finally {
    if (fetchSequence !== memePageFetchSequence) {
      return;
    }

    state.library.loading = false;
    state.library.appending = false;
    memePendingPageIndex = state.library.pageIndex;
    syncMemeGridObserver();
  }
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
  backup: { run: () => { renderAdminBackupStatus(); return fetchAdminBackupStatus(); } },
  "audiobook-conversion": { run: () => fetchAdminRemuxStatus() },
};

async function loadInitialMemes() {
  renderSidebarViewState();
  renderContentMode();
  syncAdminURL();
  syncAudiobooksURL();
  if (canView()) {
    fetchSidebarPopularTags().catch((error) => console.error(error));
  }
	if (state.filters.view === "home") {
		await fetchVaultDashboard();
		renderContentMode();
		return;
	}
	if (isAudiobooksView()) {
		await Promise.all([fetchAudiobooks(), fetchAudiobookSeries()]);
		if (state.audiobooks.view === "manage-books") {
			await fetchAudiobookSeriesManagerBooks();
			renderAudiobookSeriesManagerView();
		}
		renderContentMode();
		return;
	}
  if (isAdminView() && canManageUsers()) {
    const tab = isValidAdminTab(activeAdminTab()) ? activeAdminTab() : ADMIN_DEFAULT_TAB;
    const config = ADMIN_TABS[tab];
    const loader = ADMIN_TAB_LOADERS[tab] || {};
    adminViewKicker.textContent = config.kicker || "Admin";
    adminViewTitle.textContent = config.title || "Admin Workspace";
    adminViewCopy.textContent = config.copy || "";
    setAdminViewStatus(loader.loading || "");
    if (loader.paginated) syncAdminPagination();
    setAdminPanelLoading(!!loader.run);
    let result = null;
    try {
      result = loader.run ? await loader.run() : null;
    } catch (error) {
      console.error(error);
      setAdminPanelLoading(false);
      setAdminViewStatus(loader.fail || "Could not load the admin workspace.");
      renderContentMode();
      return;
    }
    setAdminPanelLoading(false);
    if (typeof loader.ok === "function") {
      setAdminViewStatus(loader.ok(result));
    } else if (loader.run && loader.fail) {
      setAdminViewStatus(result ? "" : loader.fail);
    } else {
      setAdminViewStatus("");
    }
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

// confirmViaModal is a styled stand-in for window.confirm(): it resolves true
// only if the Confirm button is clicked, and false for Cancel, Escape, or any
// other way the dialog closes. Falls back to window.confirm if the modal
// markup is missing for some reason.
function confirmViaModal({ title, message, confirmLabel = "Confirm", cancelLabel = "Cancel", danger = true } = {}) {
  return new Promise((resolve) => {
    if (!confirmModal || !confirmModalConfirmButton || !confirmModalCancelButton) {
      resolve(window.confirm([title, message].filter(Boolean).join("\n\n")));
      return;
    }

    if (confirmModalTitle) confirmModalTitle.textContent = title || "Are you sure?";
    if (confirmModalMessage) confirmModalMessage.textContent = message || "";
    confirmModalConfirmButton.textContent = confirmLabel;
    confirmModalCancelButton.textContent = cancelLabel;
    confirmModalConfirmButton.classList.toggle("danger-button", danger);
    confirmModalConfirmButton.classList.toggle("primary-button", !danger);

    let result = false;
    const onConfirmClick = () => { result = true; confirmModal.close(); };
    const onCancelClick = () => { result = false; confirmModal.close(); };
    const onClose = () => {
      confirmModalConfirmButton.removeEventListener("click", onConfirmClick);
      confirmModalCancelButton.removeEventListener("click", onCancelClick);
      confirmModal.removeEventListener("close", onClose);
      resolve(result);
    };

    confirmModalConfirmButton.addEventListener("click", onConfirmClick);
    confirmModalCancelButton.addEventListener("click", onCancelClick);
    confirmModal.addEventListener("close", onClose);
    confirmModal.showModal();
  });
}

// ---------------------------------------------------------------------------
// Audiobooks: library listing, resumable chunked upload, and the player.
// ---------------------------------------------------------------------------

const AUDIOBOOK_CHUNK_SIZE = 8 * 1024 * 1024; // 8 MiB per chunk request.
// Used before the auth session response has loaded, or if the server ever
// omits audiobook_upload_concurrency; the server's own value (admin-tunable
// via the MEMEINDEX_AUDIOBOOK_UPLOAD_CONCURRENCY env var) takes over once
// fetchAuthSession() resolves.
const AUDIOBOOK_UPLOAD_CONCURRENCY_DEFAULT = 4;
const AUDIOBOOK_UPLOAD_STORAGE_KEY = "memevault.audiobookUpload";
const AUDIOBOOK_PROGRESS_SAVE_INTERVAL_MS = 15000;
// How often this device reports its playback state while actually playing,
// and how often it polls for another device doing the same. The heartbeat is
// also how a playing device hears it was asked to stop, so it's what bounds
// how long two devices overlap after "play here" - kept short for that, not a
// bandwidth concern at this scale.
const AUDIOBOOK_PLAYBACK_HEARTBEAT_INTERVAL_MS = 2000;
const AUDIOBOOK_PLAYBACK_ELSEWHERE_POLL_INTERVAL_MS = 5000;
// After asking another device to stop, how long to keep checking that it
// actually did before telling the listener it didn't respond.
const AUDIOBOOK_TAKEOVER_CONFIRM_TIMEOUT_MS = 10000;
const AUDIOBOOK_TAKEOVER_CONFIRM_POLL_MS = 1000;
// The server stops counting a device as playing after 20s without a
// heartbeat, so a reported position is never older than that.
const AUDIOBOOK_PLAYBACK_MAX_AGE_SECONDS = 20;
const AUDIOBOOK_DEVICE_ID_STORAGE_KEY = "memeindex.audiobookDeviceId";

let audiobookDeviceIdCache = null;

// getAudiobookDeviceId returns a per-browser random id, persisted in
// localStorage, used only to tell this browser's own playback heartbeats
// apart from another device's for the same account - not a real identity.
function getAudiobookDeviceId() {
  if (audiobookDeviceIdCache) return audiobookDeviceIdCache;
  const fallback = () => `dev-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  try {
    let id = window.localStorage.getItem(AUDIOBOOK_DEVICE_ID_STORAGE_KEY);
    if (!id) {
      id = window.crypto?.randomUUID ? window.crypto.randomUUID() : fallback();
      window.localStorage.setItem(AUDIOBOOK_DEVICE_ID_STORAGE_KEY, id);
    }
    audiobookDeviceIdCache = id;
  } catch (error) {
    audiobookDeviceIdCache = fallback();
  }
  return audiobookDeviceIdCache;
}

class AudiobookUploadCancelled extends Error {}

function loadSavedAudiobookUpload() {
  try {
    const raw = window.localStorage.getItem(AUDIOBOOK_UPLOAD_STORAGE_KEY);
    return raw ? JSON.parse(raw) : null;
  } catch (error) {
    return null;
  }
}

function saveAudiobookUploadMarker(marker) {
  try {
    if (marker) {
      window.localStorage.setItem(AUDIOBOOK_UPLOAD_STORAGE_KEY, JSON.stringify(marker));
    } else {
      window.localStorage.removeItem(AUDIOBOOK_UPLOAD_STORAGE_KEY);
    }
  } catch (error) {
    // Best effort only - resuming still works within the tab's lifetime.
  }
}

async function fetchAudiobooks() {
  state.audiobooks.loading = true;
  if (audiobooksStatus) audiobooksStatus.textContent = "Loading audiobooks...";
  try {
    const params = new URLSearchParams();
    if (state.audiobooks.filter) params.set("filter", state.audiobooks.filter);
    if (state.audiobooks.seriesId) params.set("series_id", state.audiobooks.seriesId);
    if (state.audiobooks.author) params.set("author", state.audiobooks.author);
    if (state.audiobooks.tag) params.set("tag", state.audiobooks.tag);
    if (state.filters.query) params.set("q", state.filters.query);
    const query = params.toString();
    const response = await fetch(`/api/audiobooks${query ? `?${query}` : ""}`, { credentials: "same-origin" });
    if (!(await expectAuthorized(response, "Could not load audiobooks."))) {
      return;
    }
    const payload = await response.json();
    state.audiobooks.items = payload.audiobooks || [];
    if (audiobooksStatus) audiobooksStatus.textContent = "";
  } catch (error) {
    console.error(error);
    if (audiobooksStatus) audiobooksStatus.textContent = "Could not load audiobooks.";
  } finally {
    state.audiobooks.loading = false;
    renderAudiobooksGrid();
  }
}

async function fetchAudiobookSeries() {
  try {
    const response = await fetch("/api/audiobook-series", { credentials: "same-origin" });
    if (!(await expectAuthorized(response, "Could not load audiobook series."))) {
      return;
    }
    const payload = await response.json();
    state.audiobooks.series = payload.series || [];
  } catch (error) {
    console.error(error);
  } finally {
    renderAudiobookSeriesFilter();
    if (state.audiobooks.view === "series") {
      renderAudiobooksGrid();
    }
    if (state.audiobooks.view === "manage-books") {
      renderAudiobookSeriesManagerView();
    }
  }
}

function renderAudiobookSeriesFilter() {
  audiobooksSubnav?.classList.toggle("hidden", !canViewAudiobooks());
  const series = state.audiobooks.series;
  const current = state.audiobooks.seriesId;
  const activeFilter = state.audiobooks.filter;
  const activeAuthor = state.audiobooks.author;
  const activeTag = state.audiobooks.tag;
  const inSeriesGallery = state.audiobooks.view === "series";

  audiobooksSubnavFilterItems.forEach((item) => {
    item.classList.toggle("is-active", !inSeriesGallery && !current && !activeAuthor && !activeTag && item.dataset.audiobookFilter === activeFilter);
  });
  audiobooksSubnavSeriesButton?.classList.toggle("is-active", inSeriesGallery);

  if (audiobooksActiveFilterLabel) {
    const activeSeries = series.find((s) => s.id === current);
    const activeSearch = state.filters.query.trim();
    const baseLabel = inSeriesGallery
      ? "Series"
      : activeSeries
        ? activeSeries.name
        : activeAuthor
          ? `Books by ${activeAuthor}`
          : activeTag
            ? `Tagged "${activeTag}"`
            : activeFilter
              ? activeFilter.charAt(0).toUpperCase() + activeFilter.slice(1)
              : "";
    const labelText = activeSearch
      ? (baseLabel ? `${baseLabel} • Search: "${activeSearch}"` : `Search: "${activeSearch}"`)
      : baseLabel;
    // Author/tag/search filters have no subnav button to switch away from
    // (unlike Favorites/Started/Finished/Series), so the label doubles as
    // the only way back to the full list.
    const clearable = !!activeAuthor || !!activeTag || !!activeSearch;
    audiobooksActiveFilterLabel.textContent = labelText + (clearable ? " ×" : "");
    audiobooksActiveFilterLabel.classList.toggle("hidden", !labelText);
    audiobooksActiveFilterLabel.classList.toggle("is-clearable", clearable);
    audiobooksActiveFilterLabel.title = clearable ? "Clear filter" : "";
    audiobooksActiveFilterLabel.onclick = clearable ? () => clearAudiobooksActiveFilter() : null;
  }
}

function audiobookSeriesCardHTML(series) {
  const slots = [];
  for (let i = 0; i < 4; i++) {
    const book = series.top_books?.[i];
    slots.push(book?.cover_url
      ? `<img class="audiobook-series-tile-thumb" src="${escapeHTML(book.cover_url)}" alt="" loading="lazy" />`
      : `<div class="audiobook-series-tile-thumb audiobook-series-tile-thumb-empty" aria-hidden="true"></div>`);
  }
  return `
    <article class="audiobook-series-tile" data-series-id="${escapeHTML(series.id)}">
      <button type="button" class="audiobook-series-tile-hitarea" data-series-open aria-label="Open ${escapeHTML(series.name)}">
        <div class="audiobook-series-tile-thumbs${series.book_count > 1 ? "" : " is-single"}">${slots.join("")}</div>
      </button>
      <div class="audiobook-series-tile-body">
        <strong class="audiobook-series-tile-title">${escapeHTML(series.name)}</strong>
        <span class="audiobook-series-tile-count">${series.book_count} book${series.book_count === 1 ? "" : "s"}</span>
      </div>
      <button type="button" class="ghost-button audiobook-series-tile-share" data-series-share title="Copy a link to this series" aria-label="Copy a link to ${escapeHTML(series.name)}">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8.6 13.5 15 9.9M8.6 10.5 15 14.1" /><circle cx="6" cy="12" r="2.3" /><circle cx="17.5" cy="6.5" r="2.3" /><circle cx="17.5" cy="17.5" r="2.3" /></svg>
      </button>
    </article>
  `;
}

function renderAudiobookSeriesGallery() {
  if (!audiobooksGrid) return;
  const series = state.audiobooks.series;
  audiobooksGrid.classList.add("audiobooks-grid-series");
  audiobooksGrid.innerHTML = series.map(audiobookSeriesCardHTML).join("");
  audiobooksEmpty?.classList.toggle("hidden", series.length !== 0 || state.audiobooks.loading);

  series.forEach((entry) => {
    const card = audiobooksGrid.querySelector(`[data-series-id="${window.CSS?.escape ? CSS.escape(entry.id) : entry.id}"]`);
    if (!card) return;
    const openThisSeries = () => {
      state.audiobooks.filter = "";
      state.audiobooks.seriesId = entry.id;
      state.audiobooks.view = "books";
      // Pushed before navigateToView (which calls loadInitialMemes, and that
      // always does its own replace-only sync) - otherwise that replace runs
      // first, the URL already matches what this push would set, and the
      // push's own "already there" short-circuit silently skips it.
      syncAudiobooksURL({ push: true });
      navigateToView("audiobooks");
    };
    card.querySelector("[data-series-open]")?.addEventListener("click", openThisSeries);
    card.querySelector("[data-series-share]")?.addEventListener("click", (event) => {
      event.stopPropagation();
      shareAudiobookSeries(entry).catch((error) => console.error(error));
    });
  });
}

// The series manager needs every audiobook in the library to offer as
// "add to series" candidates, regardless of whatever filter/series view is
// currently active in the main grid - state.audiobooks.items is scoped to
// that view and would otherwise silently hide books from the picker.
let audiobookSeriesManagerBooks = [];

async function fetchAudiobookSeriesManagerBooks() {
  try {
    const response = await fetch("/api/audiobooks", { credentials: "same-origin" });
    if (!response.ok) return;
    const payload = await response.json();
    audiobookSeriesManagerBooks = payload.audiobooks || [];
  } catch (error) {
    console.error(error);
  }
}

// seriesManagerBooksFor relies on audiobookSeriesManagerBooks already being
// ordered series_id, then series_sequence, then created_at (the same order
// the API returns it in) - filtering preserves that, so this is already each
// series's book order, no re-sorting needed.
function seriesManagerBooksFor(seriesId) {
  return audiobookSeriesManagerBooks.filter((book) => book.series_id === seriesId);
}

// ---------------------------------------------------------------------------
// Audiobook series manager: one page, no modal and no separate "manage
// books" page to navigate to. A rail on the left lists every series (name
// editable in place, cover preview, share/delete) - clicking one loads its
// ordered books on the right. Adding a book is type-to-search rather than
// scrolling your whole library, and reordering is a position number plus
// up/down nudges instead of fragile drag-and-drop.
// ---------------------------------------------------------------------------

function openAudiobookSeriesManager() {
  if (!canManageAudiobookSeries()) return;
  state.audiobooks.view = "manage-books";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
}

function closeAudiobookSeriesManagePage() {
  state.audiobooks.manageSeriesId = "";
  state.audiobooks.view = "books";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
}

async function refreshAudiobookSeriesManagePageData() {
  await fetchAudiobookSeriesManagerBooks();
  await fetchAudiobookSeries();
  renderAudiobookSeriesManagerView();
}

// renderAudiobookSeriesManagerView draws both halves: the rail of every
// series, and (if one is selected) its book list. One function covers both
// since selecting a different series, or any edit, can change either side.
function renderAudiobookSeriesManagerView() {
  renderAudiobookSeriesManagerRail();
  renderAudiobookSeriesManagerDetail();
}

async function selectAudiobookSeriesToManage(seriesId) {
  state.audiobooks.manageSeriesId = seriesId;
  audiobookSeriesAddQuery = "";
  if (audiobookSeriesAddInput) audiobookSeriesAddInput.value = "";
  syncAudiobooksURL({ push: true });
  renderAudiobookSeriesManagerView();
}

function renderAudiobookSeriesManagerRail() {
  if (!audiobookSeriesManagerRailList) return;
  const series = state.audiobooks.series;
  const selectedId = state.audiobooks.manageSeriesId;
  audiobookSeriesManagerRailList.innerHTML = series.length
    ? series.map((s) => audiobookSeriesManagerRailItemHTML(s, s.id === selectedId)).join("")
    : `<p class="status-text">No series yet. Create one above.</p>`;

  audiobookSeriesManagerRailList.querySelectorAll("[data-series-row]").forEach((row) => {
    const seriesId = row.dataset.seriesId;
    row.addEventListener("click", (event) => {
      if (event.target.closest("[data-series-name-input], [data-series-share], [data-series-delete]")) return;
      if (seriesId) selectAudiobookSeriesToManage(seriesId).catch((error) => console.error(error));
    });
  });
  audiobookSeriesManagerRailList.querySelectorAll("[data-series-name-input]").forEach((input) => {
    const seriesId = input.closest("[data-series-id]")?.dataset.seriesId;
    const current = series.find((s) => s.id === seriesId);
    input.addEventListener("click", (event) => event.stopPropagation());
    input.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        input.blur();
      } else if (event.key === "Escape") {
        input.value = current?.name || "";
        input.blur();
      }
    });
    input.addEventListener("blur", () => {
      const name = input.value.trim();
      if (!current || !name || name === current.name) {
        input.value = current?.name || "";
        return;
      }
      submitAudiobookSeriesRename(current, name);
    });
  });
  audiobookSeriesManagerRailList.querySelectorAll("[data-series-share]").forEach((button) => {
    button.addEventListener("click", (event) => {
      event.stopPropagation();
      const current = series.find((s) => s.id === button.closest("[data-series-id]")?.dataset.seriesId);
      if (current) shareAudiobookSeries(current).catch((error) => console.error(error));
    });
  });
  audiobookSeriesManagerRailList.querySelectorAll("[data-series-delete]").forEach((button) => {
    button.addEventListener("click", (event) => {
      event.stopPropagation();
      const current = series.find((s) => s.id === button.closest("[data-series-id]")?.dataset.seriesId);
      if (current) deleteAudiobookSeriesEntry(current);
    });
  });
}

function audiobookSeriesManagerRailItemHTML(s, selected) {
  const cover = s.top_books?.[0]?.cover_url
    ? `<img class="audiobook-series-manager-row-cover" src="${escapeHTML(s.top_books[0].cover_url)}" alt="" loading="lazy" />`
    : `<div class="audiobook-series-manager-row-cover audiobook-series-manager-row-cover-empty" aria-hidden="true"></div>`;
  return `
    <div class="audiobook-series-manager-row${selected ? " is-selected" : ""}" data-series-row data-series-id="${escapeHTML(s.id)}">
      ${cover}
      <div class="audiobook-series-manager-row-info">
        <input type="text" class="audiobook-series-manager-row-name-input" value="${escapeHTML(s.name)}" maxlength="200" data-series-name-input aria-label="Rename series" title="Click to rename" />
        <span class="audiobook-series-manager-row-count">${s.book_count} book${s.book_count === 1 ? "" : "s"}</span>
      </div>
      <div class="audiobook-series-manager-row-actions">
        <button type="button" class="audiobook-series-icon-button" data-series-share title="Share" aria-label="Copy a link to ${escapeHTML(s.name)}">&#8599;</button>
        <button type="button" class="audiobook-series-icon-button audiobook-series-book-remove" data-series-delete title="Delete series" aria-label="Delete ${escapeHTML(s.name)}">&#10005;</button>
      </div>
    </div>
  `;
}

function renderAudiobookSeriesManagerDetail() {
  const series = state.audiobooks.series.find((s) => s.id === state.audiobooks.manageSeriesId);
  audiobookSeriesManagerEmpty?.classList.toggle("hidden", !!series);
  audiobookSeriesManagerSelected?.classList.toggle("hidden", !series);
  if (!series) return;

  if (audiobookSeriesManagerSelectedName) audiobookSeriesManagerSelectedName.textContent = series.name;
  const books = seriesManagerBooksFor(series.id);
  if (audiobookSeriesManagerSelectedCount) {
    audiobookSeriesManagerSelectedCount.textContent = `${books.length} book${books.length === 1 ? "" : "s"}`;
  }

  if (audiobookSeriesManagerBooksList) {
    audiobookSeriesManagerBooksList.innerHTML = books.length
      ? books.map((book, index) => audiobookSeriesManagerBookRowHTML(book, index, books.length)).join("")
      : `<p class="status-text audiobook-series-manager-no-books">No books yet - search above to add one.</p>`;
  }
  wireAudiobookSeriesManagerBookRows(series.id);
  renderAudiobookSeriesAddList();
}

function audiobookSeriesManagerBookRowHTML(book, index, total) {
  const cover = book.cover_url
    ? `<img class="audiobook-series-manager-book-cover" src="${escapeHTML(book.cover_url)}" alt="" loading="lazy" />`
    : `<div class="audiobook-series-manager-book-cover audiobook-series-manager-book-cover-empty" aria-hidden="true"></div>`;
  // The sequence number is directly editable (rather than only derived from
  // up/down position) so a novella or side-story can be slotted in as, say,
  // 7.5 between Book 7 and Book 8 without renumbering the whole series.
  const sequenceValue = Number.isFinite(book.series_sequence) ? book.series_sequence : index + 1;
  return `
    <div class="audiobook-series-manager-book" data-book-id="${escapeHTML(book.id)}">
      ${cover}
      <div class="audiobook-series-manager-book-info">
        <span class="audiobook-series-manager-book-title">${escapeHTML(audiobookDisplayTitle(book))}</span>
        ${book.author ? `<span class="audiobook-series-manager-book-author">${escapeHTML(book.author)}</span>` : ""}
      </div>
      <div class="audiobook-series-book-order">
        <input type="number" step="any" class="audiobook-series-sequence-input" data-manage-sequence value="${escapeHTML(String(sequenceValue))}" title="Exact position - use e.g. 7.5 to slot a book between 7 and 8" aria-label="Sequence number" />
        <button type="button" class="audiobook-series-icon-button" data-manage-up ${index === 0 ? "disabled" : ""} title="Move up" aria-label="Move up">&#9650;</button>
        <button type="button" class="audiobook-series-icon-button" data-manage-down ${index === total - 1 ? "disabled" : ""} title="Move down" aria-label="Move down">&#9660;</button>
      </div>
      <button type="button" class="audiobook-series-icon-button audiobook-series-book-remove" data-manage-remove title="Remove from series" aria-label="Remove from series">&#10005;</button>
    </div>
  `;
}

// Rebuilt via innerHTML on every render, so re-attaching listeners each time
// is fine - the old nodes, and their listeners, go with them.
function wireAudiobookSeriesManagerBookRows(seriesId) {
  audiobookSeriesManagerBooksList?.querySelectorAll("[data-manage-up]").forEach((button) => {
    button.addEventListener("click", () => {
      const bookId = button.closest("[data-book-id]")?.dataset.bookId;
      if (bookId) moveAudiobookInSeries(seriesId, bookId, -1).catch((error) => console.error(error));
    });
  });
  audiobookSeriesManagerBooksList?.querySelectorAll("[data-manage-down]").forEach((button) => {
    button.addEventListener("click", () => {
      const bookId = button.closest("[data-book-id]")?.dataset.bookId;
      if (bookId) moveAudiobookInSeries(seriesId, bookId, 1).catch((error) => console.error(error));
    });
  });
  audiobookSeriesManagerBooksList?.querySelectorAll("[data-manage-remove]").forEach((button) => {
    button.addEventListener("click", () => {
      const bookId = button.closest("[data-book-id]")?.dataset.bookId;
      if (bookId) removeAudiobookFromSeries(bookId).catch((error) => console.error(error));
    });
  });
  audiobookSeriesManagerBooksList?.querySelectorAll("[data-manage-sequence]").forEach((input) => {
    input.addEventListener("keydown", (event) => {
      if (event.key === "Enter") {
        event.preventDefault();
        input.blur();
      }
    });
    input.addEventListener("change", () => {
      const bookId = input.closest("[data-book-id]")?.dataset.bookId;
      const sequence = parseFloat(input.value);
      if (!bookId || !Number.isFinite(sequence)) return;
      setAudiobookSeriesSequenceDirect(seriesId, bookId, sequence).catch((error) => console.error(error));
    });
  });
}

// ---------------------------------------------------------------------------
// Add-to-series search: filters the already-fetched full library client side
// (no extra network round trip per keystroke) and lets a click add a book to
// the currently selected series straight away - the fast replacement for
// scrolling a whole separate "library" pane.
// ---------------------------------------------------------------------------

let audiobookSeriesAddQuery = "";

// With no filter text typed, every book not already in this series is a
// candidate - this is the browsable "add books" list, not just a search
// dropdown, so there's still a way to look rather than already knowing what
// to type.
function audiobookSeriesAddCandidates(seriesId, query) {
  const needle = query.trim().toLowerCase();
  return audiobookSeriesManagerBooks
    .filter((book) => book.series_id !== seriesId)
    .filter((book) => !needle
      || audiobookDisplayTitle(book).toLowerCase().includes(needle)
      || (book.author || "").toLowerCase().includes(needle))
    .sort((a, b) => audiobookDisplayTitle(a).localeCompare(audiobookDisplayTitle(b)));
}

function renderAudiobookSeriesAddList() {
  if (!audiobookSeriesAddList) return;
  const seriesId = state.audiobooks.manageSeriesId;
  const candidates = seriesId ? audiobookSeriesAddCandidates(seriesId, audiobookSeriesAddQuery) : [];
  if (!candidates.length) {
    audiobookSeriesAddList.innerHTML = `<p class="status-text audiobook-series-add-list-empty">${
      audiobookSeriesAddQuery.trim() ? "No matching books." : "Every book is already in this series."
    }</p>`;
    return;
  }
  audiobookSeriesAddList.innerHTML = candidates.map((book) => {
    const otherSeries = book.series_id ? state.audiobooks.series.find((s) => s.id === book.series_id) : null;
    const cover = book.cover_url
      ? `<img class="audiobook-series-add-row-cover" src="${escapeHTML(book.cover_url)}" alt="" loading="lazy" />`
      : `<div class="audiobook-series-add-row-cover audiobook-series-add-row-cover-empty" aria-hidden="true"></div>`;
    return `
      <div class="audiobook-series-add-row" data-book-id="${escapeHTML(book.id)}">
        ${cover}
        <div class="audiobook-series-add-row-info">
          <span class="audiobook-series-add-row-title">${escapeHTML(audiobookDisplayTitle(book))}</span>
          ${book.author ? `<span class="audiobook-series-add-row-author">${escapeHTML(book.author)}</span>` : ""}
          ${otherSeries ? `<span class="audiobook-manage-item-badge">In ${escapeHTML(otherSeries.name)}</span>` : ""}
        </div>
        <button type="button" class="audiobook-series-icon-button" data-add-book title="Add to series" aria-label="Add ${escapeHTML(audiobookDisplayTitle(book))} to series">+</button>
      </div>
    `;
  }).join("");
  audiobookSeriesAddList.querySelectorAll("[data-add-book]").forEach((button) => {
    button.addEventListener("click", () => {
      const bookId = button.closest("[data-book-id]")?.dataset.bookId;
      if (bookId && seriesId) addAudiobookToSeries(seriesId, bookId).catch((error) => console.error(error));
    });
  });
}

async function shareAudiobookSeries(series) {
  const url = `${window.location.origin}/?view=audiobooks&series=${encodeURIComponent(series.id)}`;
  try {
    await copyShareText(url);
    showToast(`Link to "${series.name}" copied.`, "success", { title: "Audiobook Series" });
  } catch (error) {
    console.error(error);
    showToast("Could not copy the series link.", "error", { title: "Audiobook Series" });
  }
}

// Same idea as shareAudiobookSeries, one level down: a deep link that opens
// straight to this one book's player. Whoever opens it still needs their own
// account and view access - this isn't a public, unauthenticated link like a
// meme share.
async function shareAudiobookItem(item) {
  const url = `${window.location.origin}/?view=audiobooks&book=${encodeURIComponent(item.id)}`;
  try {
    await copyShareText(url);
    showToast(`Link to "${audiobookDisplayTitle(item)}" copied.`, "success", { title: "Audiobooks" });
  } catch (error) {
    console.error(error);
    showToast("Could not copy the link.", "error", { title: "Audiobooks" });
  }
}

// Consumes a ?book=<id> deep link once, after the initial audiobooks fetch
// has resolved. Prefers the already-loaded list item (it carries this
// listener's own playback position/favorite state); falls back to a direct
// fetch for a book that isn't in whatever's currently loaded - e.g. shared
// from a different filter than the one this session starts on.
async function openDeepLinkedAudiobook() {
  const id = audiobookPendingDeepLinkBookId;
  audiobookPendingDeepLinkBookId = "";
  if (!id || !canViewAudiobooks()) return;
  const existing = state.audiobooks.items.find((entry) => entry.id === id);
  if (existing) {
    await openAudiobookPlayer(existing).catch((error) => console.error(error));
    return;
  }
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(id)}`, { credentials: "same-origin" });
    if (!(await expectAuthorized(response, "Could not open the shared audiobook."))) return;
    const item = await response.json();
    await openAudiobookPlayer(item);
  } catch (error) {
    console.error(error);
    showToast("Could not open the shared audiobook.", "error", { title: "Audiobooks" });
  }
}

async function createAudiobookSeriesEntry(name) {
  if (audiobookSeriesManagerStatus) audiobookSeriesManagerStatus.textContent = "Creating series...";
  try {
    const response = await fetch("/api/audiobook-series", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (response.status === 409) {
      if (audiobookSeriesManagerStatus) audiobookSeriesManagerStatus.textContent = "A series with that name already exists.";
      return;
    }
    if (!(await expectAuthorized(response, "Could not create series."))) {
      return;
    }
    if (audiobookSeriesManagerStatus) audiobookSeriesManagerStatus.textContent = "";
    await fetchAudiobookSeries();
  } catch (error) {
    console.error(error);
    if (audiobookSeriesManagerStatus) audiobookSeriesManagerStatus.textContent = "Could not create series.";
  }
}

async function submitAudiobookSeriesRename(series, name) {
  try {
    const response = await fetch(`/api/audiobook-series/${encodeURIComponent(series.id)}`, {
      method: "PATCH",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name }),
    });
    if (response.status === 409) {
      showToast("A series with that name already exists.", "error", { title: "Audiobook Series" });
      return;
    }
    if (!(await expectAuthorized(response, "Could not rename series."))) {
      return;
    }
    await fetchAudiobookSeries();
    await fetchAudiobooks();
  } catch (error) {
    console.error(error);
    showToast("Could not rename series.", "error", { title: "Audiobook Series" });
  }
}

async function deleteAudiobookSeriesEntry(series) {
  const confirmed = await confirmViaModal({
    title: "Delete series?",
    message: `Delete "${series.name}"? Its ${series.book_count} book${series.book_count === 1 ? "" : "s"} will stay in your library, just no longer grouped under this series.`,
    confirmLabel: "Delete",
  });
  if (!confirmed) return;
  try {
    const response = await fetch(`/api/audiobook-series/${encodeURIComponent(series.id)}`, {
      method: "DELETE",
      credentials: "same-origin",
    });
    if (!(await expectAuthorized(response, "Could not delete series."))) {
      return;
    }
    if (state.audiobooks.seriesId === series.id) {
      state.audiobooks.seriesId = "";
      syncAudiobooksURL();
    }
    if (state.audiobooks.manageSeriesId === series.id) {
      state.audiobooks.manageSeriesId = "";
      syncAudiobooksURL();
    }
    await fetchAudiobookSeries();
    await fetchAudiobooks();
    showToast("Series deleted.", "success", { title: "Audiobook Series" });
  } catch (error) {
    console.error(error);
    showToast("Could not delete series.", "error", { title: "Audiobook Series" });
  }
}

async function setAudiobookSeriesRaw(audiobookId, seriesId, sequence) {
  const response = await fetch(`/api/audiobooks/${encodeURIComponent(audiobookId)}/series`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ seriesId, sequence }),
  });
  if (!(await expectAuthorized(response, "Could not update that book's series."))) {
    throw new Error("set audiobook series failed");
  }
}

// setAudiobookSeriesSequenceDirect sets one book's exact sequence number
// without touching any other book's - unlike placeAudiobookInSeries (used by
// drag-and-drop and the up/down buttons), which renumbers the whole series to
// clean 1..N integers. This is the escape hatch for a sequence that isn't a
// clean integer, e.g. a 7.5 novella between Book 7 and Book 8.
async function setAudiobookSeriesSequenceDirect(seriesId, bookId, sequence) {
  try {
    await setAudiobookSeriesRaw(bookId, seriesId, sequence);
    await refreshAudiobookSeriesManagePageData();
    await fetchAudiobooks();
  } catch (error) {
    console.error(error);
    showToast("Could not update that book's position.", "error", { title: "Audiobook Series" });
  }
}

async function addAudiobookToSeries(seriesId, bookId) {
  const nextSequence = seriesManagerBooksFor(seriesId)
    .reduce((max, book) => Math.max(max, Number(book.series_sequence) || 0), 0) + 1;
  try {
    await setAudiobookSeriesRaw(bookId, seriesId, nextSequence);
    await refreshAudiobookSeriesManagePageData();
    await fetchAudiobooks();
  } catch (error) {
    console.error(error);
    showToast("Could not add book to series.", "error", { title: "Audiobook Series" });
  }
}

async function removeAudiobookFromSeries(bookId) {
  try {
    await setAudiobookSeriesRaw(bookId, "", null);
    await refreshAudiobookSeriesManagePageData();
    await fetchAudiobooks();
  } catch (error) {
    console.error(error);
    showToast("Could not remove book from series.", "error", { title: "Audiobook Series" });
  }
}

// moveAudiobookInSeries is a thin wrapper over placeAudiobookInSeries: moving
// an item by delta positions, then re-splicing it back in at that same index
// into the array with itself removed, lands it at the right final position
// either direction - the standard array-move identity.
async function moveAudiobookInSeries(seriesId, bookId, delta) {
  const books = seriesManagerBooksFor(seriesId);
  const fromIndex = books.findIndex((book) => book.id === bookId);
  const toIndex = fromIndex + delta;
  if (fromIndex === -1 || toIndex < 0 || toIndex >= books.length) return;
  await placeAudiobookInSeries(seriesId, bookId, toIndex);
}

// Adds (if not already in the series) or repositions a book at an exact
// index within the series's ordered list - used by both drag-and-drop and
// the up/down buttons - then renumbers every book 1..N so later moves have
// clean integer sequences to work with.
async function placeAudiobookInSeries(seriesId, bookId, atIndex) {
  const current = seriesManagerBooksFor(seriesId).filter((book) => book.id !== bookId);
  const moved = audiobookSeriesManagerBooks.find((book) => book.id === bookId);
  if (!moved) return;
  const clampedIndex = Math.max(0, Math.min(atIndex, current.length));
  const next = current.slice();
  next.splice(clampedIndex, 0, moved);

  try {
    await Promise.all(next.map((book, index) => {
      const sequence = index + 1;
      if (book.id !== bookId && book.series_id === seriesId && Number(book.series_sequence) === sequence) {
        return Promise.resolve();
      }
      return setAudiobookSeriesRaw(book.id, seriesId, sequence);
    }));
  } catch (error) {
    console.error(error);
    // Some of the requests above may have already landed even though this
    // one failed - refresh in the finally block below either way so the UI
    // never keeps showing a pre-move order that the server has moved on from.
    showToast("Could not fully update the series - reopen it to check the order.", "error", { title: "Audiobook Series" });
  } finally {
    await refreshAudiobookSeriesManagePageData();
    await fetchAudiobooks();
  }
}

function audiobookDisplayTitle(item) {
  return item.title || item.filename || "Untitled";
}

function audiobookFileExtension(filename) {
  const match = /\.[^./\\]+$/.exec(filename || "");
  return match ? match[0] : "";
}

function formatAudiobookRemaining(seconds) {
  const total = Math.max(0, Math.round(seconds || 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours > 0) return `${hours}h ${minutes}m left`;
  if (minutes > 0) return `${minutes}m left`;
  return "Less than a minute left";
}

function audiobookCardHTML(item) {
  const canDelete = canManageUsers() || canDeleteAudiobooks() || (!!state.auth.user?.id && state.auth.user.id === item.uploaded_by);
  const progressPct = item.duration_seconds > 0
    ? Math.min(100, Math.round((item.position_seconds / item.duration_seconds) * 100))
    : 0;
  const title = audiobookDisplayTitle(item);
  const metaParts = [formatSize(item.size_bytes || 0)];
  if (item.duration_seconds > 0) metaParts.push(formatMediaTime(item.duration_seconds));
  const coverArt = item.cover_url
    ? `<img class="audiobook-card-cover" src="${escapeHTML(item.cover_url)}" alt="" loading="lazy" />`
    : `<svg class="audiobook-card-cover-fallback" viewBox="0 0 24 24" aria-hidden="true"><path d="M6 5.5h8.5a3.5 3.5 0 0 1 3.5 3.5v9.5H8a2 2 0 0 1-2-2V5.5Z"/><path d="M6 15.5h12"/><path d="M9.5 9.5h5"/></svg>`;
  const progressBadge = progressPct >= 99
    ? `<span class="audiobook-card-badge is-complete">Finished</span>`
    : progressPct > 0
      ? `<span class="audiobook-card-badge is-progress">${progressPct}% played</span>`
      : "";
  const progressLine = progressPct > 0 && progressPct < 99
    ? `<span class="audiobook-card-progress-text">${escapeHTML(formatAudiobookRemaining(item.duration_seconds - item.position_seconds))}</span>`
    : "";
  const seriesLine = item.series_name
    ? `<span class="audiobook-card-series">${escapeHTML(item.series_name)}${item.series_sequence ? ` • Book ${escapeHTML(String(item.series_sequence))}` : ""}</span>`
    : "";
  const tagsLine = item.tags?.length
    ? `<div class="audiobook-card-tags">${item.tags.map((tag) => `<button type="button" class="audiobook-card-tag" data-audiobook-tag="${escapeHTML(tag)}">${escapeHTML(tag)}</button>`).join("")}</div>`
    : "";
  return `
    <article class="audiobook-card" data-audiobook-id="${escapeHTML(item.id)}">
      <button type="button" class="favorite-button audiobook-card-favorite${item.is_favorite ? " is-favorite" : ""}" data-audiobook-favorite aria-pressed="${item.is_favorite ? "true" : "false"}" aria-label="${item.is_favorite ? "Remove from favorites" : "Add to favorites"}" title="${item.is_favorite ? "Remove from favorites" : "Add to favorites"}">
        <svg class="favorite-icon" viewBox="0 0 24 24" aria-hidden="true"><path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78L12 21.23l8.84-8.84a5.5 5.5 0 0 0 0-7.78Z" /></svg>
      </button>
      <div class="audiobook-card-hitarea" role="button" tabindex="0" aria-label="Open ${escapeHTML(title)}">
        <div class="audiobook-card-cover-wrap">
          ${coverArt}
          ${progressBadge}
          ${progressPct > 0 ? `<div class="audiobook-card-progress-track" aria-hidden="true"><span style="width:${progressPct}%"></span></div>` : ""}
        </div>
        <div class="audiobook-card-body">
          <strong class="audiobook-card-title">${escapeHTML(title)}</strong>
          ${item.author ? `<button type="button" class="audiobook-card-author" data-audiobook-author="${escapeHTML(item.author)}" title="Show all books by ${escapeHTML(item.author)}">${escapeHTML(item.author)}</button>` : ""}
          ${seriesLine}
          <span class="audiobook-card-meta">${metaParts.map(escapeHTML).join(" • ")}</span>
          ${progressLine}
          ${tagsLine}
        </div>
      </div>
      <div class="audiobook-card-actions">
        <button type="button" class="primary-button audiobook-card-icon-button audiobook-card-play" data-audiobook-play aria-label="${progressPct > 0 && progressPct < 99 ? "Resume" : "Play"} ${escapeHTML(title)}" title="${progressPct > 0 && progressPct < 99 ? "Resume" : "Play"}">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M7 4.5v15l13-7.5Z" /></svg>
        </button>
        <a class="ghost-button audiobook-card-icon-button" href="${escapeHTML(item.stream_url)}" download="${escapeHTML(title)}${escapeHTML(audiobookFileExtension(item.filename))}" aria-label="Download ${escapeHTML(title)}" title="Download">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 4v11" /><path d="m7.5 11 4.5 4.5 4.5-4.5" /><path d="M5 19.5h14" /></svg>
        </a>
        <button type="button" class="ghost-button audiobook-card-icon-button" data-audiobook-share aria-label="Copy a link to ${escapeHTML(title)}" title="Share">
          <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8.6 13.5 15 9.9M8.6 10.5 15 14.1" /><circle cx="6" cy="12" r="2.3" /><circle cx="17.5" cy="6.5" r="2.3" /><circle cx="17.5" cy="17.5" r="2.3" /></svg>
        </button>
        ${canDelete ? `<button type="button" class="ghost-button audiobook-card-icon-button" data-audiobook-edit aria-label="Edit ${escapeHTML(title)}" title="Edit"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 20h4.5L19 9.5a2.1 2.1 0 0 0-3-3L5.5 17v3Z" /><path d="M14.5 8 17 10.5" /></svg></button>` : ""}
        ${canDelete ? `<button type="button" class="ghost-button audiobook-card-icon-button" data-audiobook-delete aria-label="Delete ${escapeHTML(title)}" title="Delete"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16" /><path d="M9 7V4.8c0-.44.36-.8.8-.8h4.4c.44 0 .8.36.8.8V7" /><path d="M6 7l.9 12.1a2 2 0 0 0 2 1.9h6.2a2 2 0 0 0 2-1.9L18 7" /><path d="M10 11v6M14 11v6" /></svg></button>` : ""}
      </div>
    </article>
  `;
}

function renderAudiobooksGrid() {
  if (!audiobooksGrid) return;
  if (state.audiobooks.view === "series") {
    renderAudiobookSeriesGallery();
    return;
  }
  audiobooksGrid.classList.remove("audiobooks-grid-series");
  const items = state.audiobooks.items;
  audiobooksGrid.innerHTML = items.map(audiobookCardHTML).join("");
  audiobooksEmpty?.classList.toggle("hidden", items.length !== 0 || state.audiobooks.loading);

  items.forEach((item) => {
    const card = audiobooksGrid.querySelector(`[data-audiobook-id="${window.CSS?.escape ? CSS.escape(item.id) : item.id}"]`);
    if (!card) return;
    const openThisBook = () => openAudiobookPlayer(item).catch((error) => console.error(error));
    const hitarea = card.querySelector(".audiobook-card-hitarea");
    hitarea?.addEventListener("click", openThisBook);
    hitarea?.addEventListener("keydown", (event) => {
      if (event.key === "Enter" || event.key === " ") {
        event.preventDefault();
        openThisBook();
      }
    });
    card.querySelector("[data-audiobook-play]")?.addEventListener("click", openThisBook);
    card.querySelector("[data-audiobook-delete]")?.addEventListener("click", () => {
      deleteAudiobookItem(item).catch((error) => console.error(error));
    });
    card.querySelector("[data-audiobook-favorite]")?.addEventListener("click", (event) => {
      event.stopPropagation();
      toggleAudiobookFavorite(item).catch((error) => console.error(error));
    });
    card.querySelector("[data-audiobook-author]")?.addEventListener("click", (event) => {
      event.stopPropagation();
      navigateToAudiobookAuthor(item.author);
    });
    card.querySelectorAll("[data-audiobook-tag]").forEach((tagButton) => {
      tagButton.addEventListener("click", (event) => {
        event.stopPropagation();
        navigateToAudiobookTag(tagButton.dataset.audiobookTag || "");
      });
    });
    card.querySelector("[data-audiobook-edit]")?.addEventListener("click", (event) => {
      event.stopPropagation();
      openAudiobookEditModal(item);
    });
    card.querySelector("[data-audiobook-share]")?.addEventListener("click", (event) => {
      event.stopPropagation();
      shareAudiobookItem(item).catch((error) => console.error(error));
    });
  });
}

async function toggleAudiobookFavorite(item) {
  const nextFavorite = !item.is_favorite;
  const button = audiobooksGrid?.querySelector(`[data-audiobook-id="${window.CSS?.escape ? CSS.escape(item.id) : item.id}"] [data-audiobook-favorite]`);
  button?.classList.toggle("is-favorite", nextFavorite);
  button?.setAttribute("aria-pressed", nextFavorite ? "true" : "false");
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(item.id)}/favorite`, {
      method: nextFavorite ? "PUT" : "DELETE",
      credentials: "same-origin",
    });
    if (!(await expectAuthorized(response, "Could not update favorite."))) {
      button?.classList.toggle("is-favorite", item.is_favorite);
      return;
    }
    item.is_favorite = nextFavorite;
    if (state.audiobooks.filter === "favorites" && !nextFavorite) {
      state.audiobooks.items = state.audiobooks.items.filter((entry) => entry.id !== item.id);
      renderAudiobooksGrid();
    }
  } catch (error) {
    console.error(error);
    button?.classList.toggle("is-favorite", item.is_favorite);
    showToast("Could not update favorite.", "error", { title: "Audiobooks" });
  }
}

async function deleteAudiobookItem(item) {
  const confirmed = await confirmViaModal({
    title: "Delete audiobook?",
    message: `Delete "${item.title || item.filename}"? This cannot be undone.`,
    confirmLabel: "Delete",
  });
  if (!confirmed) {
    return;
  }
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(item.id)}`, {
      method: "DELETE",
      credentials: "same-origin",
    });
    if (!(await expectAuthorized(response, "Could not delete this audiobook."))) {
      return;
    }
    state.audiobooks.items = state.audiobooks.items.filter((entry) => entry.id !== item.id);
    renderAudiobooksGrid();
    showToast("Audiobook deleted.", "success", { title: "Audiobooks" });
  } catch (error) {
    console.error(error);
    showToast("Could not delete this audiobook.", "error", { title: "Audiobooks" });
  }
}

// The audiobook currently loaded into the edit modal, plus any cover change
// staged there - applied together when Save is pressed, discarded on Cancel.
let audiobookEditingItem = null;
let audiobookEditPendingCoverFile = null;
let audiobookEditRemoveCoverRequested = false;

// The audiobook edit modal's tag editor - token chips with autocomplete
// against the shared `tags` table, mirroring the meme modal's tag editor
// (modalTagState / renderTagEditor / fetchTagSuggestions) rather than a
// plain comma-separated input.
let audiobookEditTagState = [];
let audiobookEditTagSequence = 0;
let audiobookEditTagSuggestionState = [];
let audiobookEditActiveTagSuggestionIndex = -1;
let audiobookEditTagSuggestionAbortController = null;

function createAudiobookEditTag(tag) {
  return { id: `ab-tag-${audiobookEditTagSequence += 1}`, value: normalizeTagValue(tag) };
}

function setAudiobookEditTags(tags) {
  const seen = new Set();
  audiobookEditTagState = [];
  for (const rawTag of tags || []) {
    const value = normalizeTagValue(rawTag);
    if (!value || seen.has(value)) continue;
    seen.add(value);
    audiobookEditTagState.push(createAudiobookEditTag(value));
  }
}

function getAudiobookEditTagValues() {
  return audiobookEditTagState.map((tag) => tag.value);
}

function addAudiobookEditTag(rawTag) {
  const tag = normalizeTagValue(rawTag);
  if (!tag || audiobookEditTagState.some((entry) => entry.value === tag)) return;
  audiobookEditTagState = [...audiobookEditTagState, createAudiobookEditTag(tag)];
  if (audiobookEditTagsInput) audiobookEditTagsInput.value = "";
  audiobookEditTagSuggestionState = [];
  audiobookEditActiveTagSuggestionIndex = -1;
  renderAudiobookEditTagEditor();
  renderAudiobookEditTagSuggestions();
}

function removeAudiobookEditTag(tagId) {
  audiobookEditTagState = audiobookEditTagState.filter((tag) => tag.id !== tagId);
  renderAudiobookEditTagEditor();
  renderAudiobookEditTagSuggestions();
}

function renderAudiobookEditTagEditor() {
  if (!audiobookEditTagChips) return;
  audiobookEditTagChips.innerHTML = "";
  for (const tag of audiobookEditTagState) {
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
    removeButton.textContent = "×";
    removeButton.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      removeAudiobookEditTag(tag.id);
      audiobookEditTagsInput?.focus();
    });

    chip.append(label, removeButton);
    audiobookEditTagChips.appendChild(chip);
  }
}

function renderAudiobookEditTagSuggestions() {
  if (!audiobookEditTagsInput || !audiobookEditTagSuggestions) return;
  renderTagSuggestionList(audiobookEditTagsInput, audiobookEditTagSuggestions, audiobookEditTagSuggestionState, audiobookEditActiveTagSuggestionIndex,
    (index) => { audiobookEditActiveTagSuggestionIndex = index; }, addAudiobookEditTag);
}

async function fetchAudiobookEditTagSuggestions() {
  const needle = normalizeTagValue(audiobookEditTagsInput?.value || "");
  if (!needle) {
    if (audiobookEditTagSuggestionAbortController) {
      audiobookEditTagSuggestionAbortController.abort();
      audiobookEditTagSuggestionAbortController = null;
    }
    audiobookEditTagSuggestionState = [];
    renderAudiobookEditTagSuggestions();
    return;
  }

  if (audiobookEditTagSuggestionAbortController) {
    audiobookEditTagSuggestionAbortController.abort();
  }
  audiobookEditTagSuggestionAbortController = new AbortController();

  try {
    const response = await fetch(`/api/tags?q=${encodeURIComponent(needle)}`, {
      signal: audiobookEditTagSuggestionAbortController.signal,
    });
    if (!response.ok) return;
    const payload = await response.json();
    if (normalizeTagValue(audiobookEditTagsInput?.value || "") !== needle) return;
    audiobookEditTagSuggestionState = (payload.tags || []).filter((tag) => !audiobookEditTagState.some((entry) => entry.value === tag));
    renderAudiobookEditTagSuggestions();
  } catch (error) {
    if (error.name !== "AbortError") console.error(error);
  }
}

function renderAudiobookEditCoverPreview(url) {
  if (audiobookEditCoverPreviewImg) {
    if (url) {
      audiobookEditCoverPreviewImg.src = url;
      audiobookEditCoverPreviewImg.classList.remove("hidden");
      audiobookEditCoverPreviewFallback?.classList.add("hidden");
    } else {
      audiobookEditCoverPreviewImg.removeAttribute("src");
      audiobookEditCoverPreviewImg.classList.add("hidden");
      audiobookEditCoverPreviewFallback?.classList.remove("hidden");
    }
  }
  audiobookEditCoverRemoveButton?.classList.toggle("hidden", !url);
}

function setAudiobookEditFormBusy(busy) {
  if (audiobookEditSaveButton) audiobookEditSaveButton.disabled = busy;
  audiobookEditCancelButton?.toggleAttribute("disabled", busy);
}

function openAudiobookEditModal(item) {
  audiobookEditingItem = item;
  audiobookEditPendingCoverFile = null;
  audiobookEditRemoveCoverRequested = false;
  if (audiobookEditCoverInput) audiobookEditCoverInput.value = "";
  if (audiobookEditTitleInput) audiobookEditTitleInput.value = item.title || "";
  if (audiobookEditAuthorInput) audiobookEditAuthorInput.value = item.author || "";
  if (audiobookEditNarratorInput) audiobookEditNarratorInput.value = item.narrator || "";
  if (audiobookEditTagsInput) audiobookEditTagsInput.value = "";
  setAudiobookEditTags(item.tags || []);
  audiobookEditTagSuggestionState = [];
  audiobookEditActiveTagSuggestionIndex = -1;
  renderAudiobookEditTagEditor();
  renderAudiobookEditTagSuggestions();
  renderAudiobookEditCoverPreview(item.cover_url || "");
  if (audiobookEditStatus) audiobookEditStatus.textContent = "";
  setAudiobookEditFormBusy(false);
  audiobookEditModal?.showModal();
}

// applyUpdatedAudiobookItem merges a known-complete patch (every field this
// modal can change) into both the library list and, if it's the book
// currently loaded in the player, the open player's display - built from the
// values just submitted rather than a possibly-partial server response, so
// there's no ambiguity about whether an absent field means "unchanged" or
// "cleared".
function applyUpdatedAudiobookItem(id, patch) {
  const index = state.audiobooks.items.findIndex((entry) => entry.id === id);
  if (index >= 0) {
    state.audiobooks.items[index] = { ...state.audiobooks.items[index], ...patch };
  }
  if (audiobookPlayerOpenItem?.id === id) {
    audiobookPlayerOpenItem = { ...audiobookPlayerOpenItem, ...patch };
    if (audiobookPlayerTitle) audiobookPlayerTitle.textContent = audiobookDisplayTitle(audiobookPlayerOpenItem);
    if (audiobookPlayerAuthor) {
      audiobookPlayerAuthor.textContent = audiobookPlayerOpenItem.author || "";
      audiobookPlayerAuthor.classList.toggle("hidden", !audiobookPlayerOpenItem.author);
    }
    if (audiobookPlayerCover) {
      if (audiobookPlayerOpenItem.cover_url) {
        audiobookPlayerCover.src = audiobookPlayerOpenItem.cover_url;
        audiobookPlayerCover.classList.remove("hidden");
        audiobookPlayerCoverFallback?.classList.add("hidden");
      } else {
        audiobookPlayerCover.removeAttribute("src");
        audiobookPlayerCover.classList.add("hidden");
        audiobookPlayerCoverFallback?.classList.remove("hidden");
      }
    }
    renderAudiobookPlayerMeta(audiobookPlayerOpenItem);
  }
  renderAudiobooksGrid();
}

async function submitAudiobookEdit(event) {
  event.preventDefault();
  const item = audiobookEditingItem;
  if (!item) return;

  const title = audiobookEditTitleInput?.value.trim() || "";
  if (!title) {
    if (audiobookEditStatus) audiobookEditStatus.textContent = "Title is required.";
    return;
  }
  const author = audiobookEditAuthorInput?.value.trim() || "";
  const narrator = audiobookEditNarratorInput?.value.trim() || "";
  const tags = getAudiobookEditTagValues();

  setAudiobookEditFormBusy(true);
  if (audiobookEditStatus) audiobookEditStatus.textContent = "Saving...";
  try {
    const metadataResponse = await fetch(`/api/audiobooks/${encodeURIComponent(item.id)}`, {
      method: "PATCH",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title, author, narrator }),
    });
    if (!(await expectAuthorized(metadataResponse, "Could not update audiobook."))) return;

    let coverURL = item.cover_url || "";
    if (audiobookEditPendingCoverFile) {
      const coverResponse = await fetch(`/api/audiobooks/${encodeURIComponent(item.id)}/cover`, {
        method: "PUT",
        credentials: "same-origin",
        headers: { "Content-Type": audiobookEditPendingCoverFile.type || "application/octet-stream" },
        body: audiobookEditPendingCoverFile,
      });
      if (!(await expectAuthorized(coverResponse, "Could not update cover."))) return;
      const coverPayload = await coverResponse.json();
      coverURL = coverPayload.cover_url || "";
    } else if (audiobookEditRemoveCoverRequested) {
      const coverResponse = await fetch(`/api/audiobooks/${encodeURIComponent(item.id)}/cover`, {
        method: "DELETE",
        credentials: "same-origin",
      });
      if (!(await expectAuthorized(coverResponse, "Could not remove cover."))) return;
      coverURL = "";
    }

    const tagsResponse = await fetch(`/api/audiobooks/${encodeURIComponent(item.id)}/tags`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ tags }),
    });
    if (!(await expectAuthorized(tagsResponse, "Could not update tags."))) return;
    const tagsPayload = await tagsResponse.json();

    applyUpdatedAudiobookItem(item.id, { title, author, narrator, cover_url: coverURL, tags: tagsPayload.tags || [] });
    audiobookEditModal?.close();
    showToast("Audiobook updated.", "success", { title: "Audiobooks" });
  } catch (error) {
    console.error(error);
    if (audiobookEditStatus) audiobookEditStatus.textContent = "Could not update audiobook.";
  } finally {
    setAudiobookEditFormBusy(false);
  }
}

function openAudiobookUploadModal() {
  if (!canUploadAudiobooks()) return;
  audiobookUploadForm?.reset();
  if (audiobookUploadStatus) audiobookUploadStatus.textContent = "";
  audiobookUploadProgressWrap?.classList.add("hidden");
  audiobookUploadPartsWrap?.classList.add("hidden");
  if (audiobookUploadParts) audiobookUploadParts.innerHTML = "";
  audiobookUploadCellEls = [];
  audiobookUploadCellBytes = 0;
  audiobookUploadCancel?.classList.add("hidden");
  setAudiobookUploadFormBusy(false);
  setAudiobookUploadQueue([]);
  audiobookUploadModal?.showModal();
}

// ---------------------------------------------------------------------------
// Audiobook upload queue: selecting several files queues them up to upload
// one at a time (each still spread over several concurrent chunk streams,
// same as before) rather than forcing one trip through this modal per file.
// ---------------------------------------------------------------------------

// { file, status: "queued"|"uploading"|"done"|"error"|"cancelled", message }
let audiobookUploadQueue = [];
// Checked at the top of every queue iteration, not just mid-file (where
// state.audiobooks.upload.cancelled is): Cancel can land in the brief gap
// between one file finishing and the next one's upload session being
// created, when there is no in-flight upload for that flag to interrupt.
let audiobookUploadQueueCancelled = false;

// Toggling the file input along with the submit button keeps someone from
// reselecting files - which replaces the whole queue - mid-upload and
// silently wiping out in-progress/finished entries.
function setAudiobookUploadFormBusy(busy) {
  if (audiobookUploadSubmit) {
    audiobookUploadSubmit.disabled = busy;
    audiobookUploadSubmit.classList.toggle("hidden", busy);
  }
  if (audiobookUploadFileInput) audiobookUploadFileInput.disabled = busy;
}

function setAudiobookUploadQueue(files) {
  audiobookUploadQueueCancelled = false;
  audiobookUploadQueue = Array.from(files).map((file) => ({ file, status: "queued", message: "" }));
  renderAudiobookUploadQueue();
}

function removeAudiobookUploadQueueEntry(index) {
  if (audiobookUploadQueue[index]?.status !== "queued") return;
  audiobookUploadQueue.splice(index, 1);
  renderAudiobookUploadQueue();
}

function audiobookUploadQueueStatusLabel(entry) {
  switch (entry.status) {
    case "uploading": return "Uploading...";
    case "done": return "Uploaded";
    case "error": return entry.message || "Failed";
    case "cancelled": return "Cancelled";
    default: return "Queued";
  }
}

function renderAudiobookUploadQueue() {
  const multiple = audiobookUploadQueue.length > 1;
  audiobookUploadMetadataFields?.classList.toggle("hidden", multiple);
  audiobookUploadMetadataNote?.classList.toggle("hidden", !multiple);

  if (!audiobookUploadQueueList) return;
  if (audiobookUploadQueue.length <= 1) {
    audiobookUploadQueueList.classList.add("hidden");
    audiobookUploadQueueList.innerHTML = "";
    return;
  }
  audiobookUploadQueueList.classList.remove("hidden");
  audiobookUploadQueueList.innerHTML = audiobookUploadQueue.map((entry, index) => `
    <div class="audiobook-upload-queue-item is-${entry.status}">
      <span class="audiobook-upload-queue-name">${escapeHTML(entry.file.name)}</span>
      <span class="audiobook-upload-queue-size">${escapeHTML(formatSize(entry.file.size))}</span>
      <span class="audiobook-upload-queue-status">${escapeHTML(audiobookUploadQueueStatusLabel(entry))}</span>
      ${entry.status === "queued"
        ? `<button type="button" class="audiobook-upload-queue-remove" data-queue-remove="${index}" aria-label="Remove ${escapeHTML(entry.file.name)} from the queue">&#10005;</button>`
        : "<span></span>"}
    </div>
  `).join("");
  audiobookUploadQueueList.querySelectorAll("[data-queue-remove]").forEach((button) => {
    button.addEventListener("click", () => removeAudiobookUploadQueueEntry(Number(button.dataset.queueRemove)));
  });
}

// The upload grid is a fixed-size file-manager-style grid (think Free
// Download Manager's connection view): the file is sliced into many small,
// uniform cells - independent of the much larger 8 MiB request chunks - and
// each cell lights up on its own as soon as the chunk covering it is picked
// up or finishes, regardless of what any other cell/chunk is doing. That's
// the point of showing it: several cells going "active" at once, scattered
// across the grid rather than marching through it in order, is what proves
// the parallel streams are really running concurrently rather than one at a
// time.
const AUDIOBOOK_UPLOAD_GRID_TARGET_CELLS = 400;
const AUDIOBOOK_UPLOAD_GRID_MIN_CELL_BYTES = 256 * 1024;

let audiobookUploadCellEls = [];
let audiobookUploadCellBytes = 0;

function audiobookUploadCellIndexForByte(byteOffset) {
  const lastIndex = audiobookUploadCellEls.length - 1;
  return Math.min(Math.floor(byteOffset / audiobookUploadCellBytes), lastIndex);
}

// renderAudiobookUploadParts lays out the grid for the file being uploaded
// and marks whatever's already received (a resumed upload) solid up front;
// everything else starts pending and is lit up by setAudiobookUploadPartState
// as chunks are picked up and finished.
function renderAudiobookUploadParts(receivedRanges, pendingChunks, totalBytes, workerCount) {
  audiobookUploadCellEls = [];
  audiobookUploadCellBytes = 0;
  if (!audiobookUploadParts || !audiobookUploadPartsWrap) return;
  audiobookUploadParts.innerHTML = "";
  if (totalBytes <= 0 || (receivedRanges.length === 0 && pendingChunks.length === 0)) {
    audiobookUploadPartsWrap.classList.add("hidden");
    return;
  }

  const cellBytes = Math.max(AUDIOBOOK_UPLOAD_GRID_MIN_CELL_BYTES, Math.ceil(totalBytes / AUDIOBOOK_UPLOAD_GRID_TARGET_CELLS));
  const cellCount = Math.max(Math.ceil(totalBytes / cellBytes), 1);
  audiobookUploadCellBytes = cellBytes;

  const fragment = document.createDocumentFragment();
  for (let i = 0; i < cellCount; i++) {
    const el = document.createElement("span");
    el.className = "audiobook-upload-part";
    fragment.appendChild(el);
    audiobookUploadCellEls.push(el);
  }
  audiobookUploadParts.appendChild(fragment);

  for (const range of receivedRanges) {
    setAudiobookUploadPartState(range.start, range.end, "is-done");
  }

  if (audiobookUploadPartsCaption) {
    audiobookUploadPartsCaption.textContent = pendingChunks.length
      ? `Uploading in up to ${workerCount} parallel part${workerCount === 1 ? "" : "s"} at once`
      : "";
  }
  audiobookUploadPartsWrap.classList.remove("hidden");
}

// setAudiobookUploadPartState marks every grid cell overlapping [start, end)
// with the given state - a byte range the size of one upload chunk (8 MiB)
// typically spans several of the smaller grid cells at once.
function setAudiobookUploadPartState(start, end, state) {
  if (!audiobookUploadCellBytes || !audiobookUploadCellEls.length) return;
  const firstIndex = audiobookUploadCellIndexForByte(start);
  const lastIndex = audiobookUploadCellIndexForByte(Math.max(end - 1, start));
  for (let i = firstIndex; i <= lastIndex; i++) {
    const el = audiobookUploadCellEls[i];
    if (!el) continue;
    el.classList.remove("is-active", "is-done");
    el.classList.add(state);
  }
}

function updateAudiobookUploadProgress(receivedBytes, totalBytes) {
  const pct = totalBytes > 0 ? Math.min(100, Math.round((receivedBytes / totalBytes) * 100)) : 0;
  if (audiobookUploadProgressBar) audiobookUploadProgressBar.style.width = `${pct}%`;
  if (audiobookUploadProgressLabel) audiobookUploadProgressLabel.textContent = `${pct}%`;
  if (audiobookUploadProgressSize) {
    audiobookUploadProgressSize.textContent = `${formatSize(receivedBytes)} / ${formatSize(totalBytes)}`;
  }
  if (audiobookUploadStatus) {
    audiobookUploadStatus.textContent = pct >= 100 ? "Finishing up..." : "Uploading...";
  }
}

function probeAudioDuration(file) {
  return new Promise((resolve) => {
    const probe = new Audio();
    const url = URL.createObjectURL(file);
    const cleanup = () => URL.revokeObjectURL(url);
    const finish = (duration) => {
      cleanup();
      resolve(Number.isFinite(duration) && duration > 0 ? duration : 0);
    };
    probe.preload = "metadata";
    probe.addEventListener("loadedmetadata", () => finish(probe.duration));
    probe.addEventListener("error", () => finish(0));
    probe.src = url;
  });
}

class AudiobookUploadForbidden extends Error {}

// computeAudiobookUploadGaps turns the server's list of already-received
// byte ranges into the complement within [0, totalBytes) - the spans that
// still need uploading. Ranges arrive sorted and non-overlapping (the server
// merges them as chunks land), but this re-sorts defensively rather than
// assume that.
function computeAudiobookUploadGaps(receivedRanges, totalBytes) {
  const sorted = [...receivedRanges].sort((a, b) => a.start - b.start);
  const gaps = [];
  let cursor = 0;
  for (const range of sorted) {
    if (range.start > cursor) gaps.push({ start: cursor, end: range.start });
    cursor = Math.max(cursor, range.end);
  }
  if (cursor < totalBytes) gaps.push({ start: cursor, end: totalBytes });
  return gaps;
}

// splitAudiobookUploadChunks slices the still-missing spans into
// AUDIOBOOK_CHUNK_SIZE pieces, each uploaded as its own PATCH request. Several
// of these run concurrently (see submitAudiobookUpload), which is what lets a
// large file use multiple streams instead of one.
function splitAudiobookUploadChunks(gaps) {
  const chunks = [];
  for (const gap of gaps) {
    for (let start = gap.start; start < gap.end; start += AUDIOBOOK_CHUNK_SIZE) {
      chunks.push({ start, end: Math.min(start + AUDIOBOOK_CHUNK_SIZE, gap.end) });
    }
  }
  return chunks;
}

// uploadAudiobookChunk retries transient failures (network blips, a 429 while
// other chunks are mid-flight, a brief 5xx) quietly in the background - a
// multi-gigabyte upload can run for a long time and hiccups are expected, so
// this deliberately avoids expectAuthorized's window.alert, which would
// otherwise pop a blocking dialog on every rate-limited chunk.
async function uploadAudiobookChunk(uploadId, offset, chunk, attempt = 0) {
  if (state.audiobooks.upload?.cancelled) {
    throw new AudiobookUploadCancelled();
  }
  try {
    const response = await fetch(`/api/audiobooks/uploads/${encodeURIComponent(uploadId)}`, {
      method: "PATCH",
      credentials: "same-origin",
      headers: { "Upload-Offset": String(offset) },
      body: chunk,
    });
    if (response.status === 401) {
      window.location.href = "/auth/login";
      throw new AudiobookUploadCancelled();
    }
    if (response.status === 403) {
      throw new AudiobookUploadForbidden("You do not have permission to upload audiobooks.");
    }
    if (!response.ok) {
      throw new Error(`Chunk upload failed (${response.status})`);
    }
  } catch (error) {
    if (error instanceof AudiobookUploadCancelled || error instanceof AudiobookUploadForbidden || attempt >= 8) {
      throw error;
    }
    await new Promise((resolve) => window.setTimeout(resolve, Math.min(1000 * 2 ** attempt, 20000)));
    return uploadAudiobookChunk(uploadId, offset, chunk, attempt + 1);
  }
}

class AudiobookUploadDuplicate extends Error {}

async function submitAudiobookUpload(event) {
  event.preventDefault();
  if (!audiobookUploadQueue.length) {
    if (audiobookUploadStatus) audiobookUploadStatus.textContent = "Choose a file first.";
    return;
  }
  // Reset in case a previous Cancel on this same queue left it set - this
  // click is what "resume the rest of the queue" means, including right
  // after a cancel.
  audiobookUploadQueueCancelled = false;

  setAudiobookUploadFormBusy(true);
  audiobookUploadCancel?.classList.remove("hidden");
  audiobookUploadProgressWrap?.classList.remove("hidden");

  // Leave title/author blank when the user didn't type one, or when queuing
  // several files at once (each then reads its own tags) - the server falls
  // back to the file name only if neither the user nor the file's own tags
  // supplied a title.
  const single = audiobookUploadQueue.length === 1;
  const title = single ? (audiobookUploadTitleInput?.value.trim() || "") : "";
  const author = single ? (audiobookUploadAuthorInput?.value.trim() || "") : "";

  for (let index = 0; index < audiobookUploadQueue.length; index++) {
    if (audiobookUploadQueueCancelled) {
      if (audiobookUploadStatus) audiobookUploadStatus.textContent = "Upload cancelled.";
      setAudiobookUploadFormBusy(false);
      audiobookUploadCancel?.classList.add("hidden");
      return;
    }
    const entry = audiobookUploadQueue[index];
    if (entry.status !== "queued") continue;

    entry.status = "uploading";
    renderAudiobookUploadQueue();
    if (audiobookUploadStatus) {
      audiobookUploadStatus.textContent = single
        ? "Starting upload..."
        : `Uploading ${index + 1} of ${audiobookUploadQueue.length}: ${entry.file.name}`;
    }

    try {
      await uploadOneAudiobookFile(entry.file, title, author);
      entry.status = "done";
      renderAudiobookUploadQueue();
    } catch (error) {
      if (error instanceof AudiobookUploadDuplicate) {
        entry.status = "error";
        entry.message = "An audiobook with this title already exists.";
        renderAudiobookUploadQueue();
        showToast(`"${entry.file.name}": an audiobook with this title already exists.`, "error", { title: "Audiobooks" });
        continue;
      }

      entry.status = error instanceof AudiobookUploadCancelled ? "cancelled" : "error";
      if (error instanceof AudiobookUploadCancelled) {
        entry.message = "Cancelled";
        if (audiobookUploadStatus) audiobookUploadStatus.textContent = "Upload cancelled.";
      } else if (error instanceof AudiobookUploadForbidden) {
        entry.message = error.message;
        if (audiobookUploadStatus) audiobookUploadStatus.textContent = error.message;
      } else {
        console.error(error);
        entry.message = "Failed";
        if (audiobookUploadStatus) {
          audiobookUploadStatus.textContent = "Upload failed. Check your connection and click Upload again to resume.";
        }
      }
      renderAudiobookUploadQueue();
      setAudiobookUploadFormBusy(false);
      audiobookUploadCancel?.classList.add("hidden");
      return;
    }
  }

  const doneCount = audiobookUploadQueue.filter((entry) => entry.status === "done").length;
  const failedCount = audiobookUploadQueue.length - doneCount;
  if (failedCount === 0) {
    showToast(single ? "Audiobook uploaded." : `${doneCount} audiobooks uploaded.`, "success", { title: "Audiobooks" });
    audiobookUploadModal?.close();
  } else {
    if (audiobookUploadStatus) {
      audiobookUploadStatus.textContent = `Uploaded ${doneCount} of ${audiobookUploadQueue.length} - see the list above for what failed.`;
    }
    setAudiobookUploadFormBusy(false);
    audiobookUploadCancel?.classList.add("hidden");
  }
  await fetchAudiobooks();
  renderContentMode();
}

// uploadOneAudiobookFile runs the resumable-chunked-upload dance for a single
// file: create or resume the upload session, stream whatever's missing over
// several concurrent chunk workers, then finalize it server-side. Throws
// AudiobookUploadDuplicate for a 409 (caller skips to the next queued file),
// AudiobookUploadCancelled/AudiobookUploadForbidden to stop the whole queue,
// or any other Error for a hard failure that also stops the queue.
async function uploadOneAudiobookFile(file, title, author) {
  let uploadId = "";
  let receivedRanges = [];

  const saved = loadSavedAudiobookUpload();
  if (saved && saved.filename === file.name && saved.totalBytes === file.size) {
    try {
      const statusResponse = await fetch(`/api/audiobooks/uploads/${encodeURIComponent(saved.uploadId)}`, {
        credentials: "same-origin",
      });
      if (statusResponse.ok) {
        const statusPayload = await statusResponse.json();
        uploadId = saved.uploadId;
        receivedRanges = statusPayload.received_ranges || [];
        if (audiobookUploadStatus) audiobookUploadStatus.textContent = "Resuming previous upload...";
      }
    } catch (error) {
      console.error(error);
    }
  }

  if (!uploadId) {
    const createResponse = await fetch("/api/audiobooks/uploads", {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ filename: file.name, mimeType: file.type, totalBytes: file.size, title, author }),
    });
    if (!(await expectAuthorized(createResponse, "Could not start the upload."))) {
      throw new Error("audiobook upload create failed");
    }
    const createPayload = await createResponse.json();
    uploadId = createPayload.upload_id;
    receivedRanges = createPayload.received_ranges || [];
  }

  state.audiobooks.upload = { id: uploadId, cancelled: false };
  saveAudiobookUploadMarker({ uploadId, filename: file.name, totalBytes: file.size });

  let uploadedBytes = receivedRanges.reduce((sum, range) => sum + (range.end - range.start), 0);
  updateAudiobookUploadProgress(uploadedBytes, file.size);

  // Upload whatever is still missing as a queue of fixed-size chunks, with
  // several workers pulling from it concurrently - this is what spreads one
  // large file over multiple streams instead of a single sequential PATCH
  // after another.
  const chunkQueue = splitAudiobookUploadChunks(computeAudiobookUploadGaps(receivedRanges, file.size));
  let nextChunkIndex = 0;
  // Once any one stream gives up for good (retries exhausted), the other
  // concurrent streams should stop picking up new chunks too, rather than
  // keep hammering a doomed upload in the background after the UI has
  // already moved on to the failure state.
  let uploadFailed = false;
  const concurrency = state.auth.audiobookUploadConcurrency || AUDIOBOOK_UPLOAD_CONCURRENCY_DEFAULT;
  const workerCount = Math.min(concurrency, Math.max(1, chunkQueue.length));
  renderAudiobookUploadParts(receivedRanges, chunkQueue, file.size, workerCount);

  async function runAudiobookUploadWorker() {
    while (nextChunkIndex < chunkQueue.length) {
      if (state.audiobooks.upload?.cancelled) {
        throw new AudiobookUploadCancelled();
      }
      if (uploadFailed) return;
      const { start, end } = chunkQueue[nextChunkIndex++];
      setAudiobookUploadPartState(start, end, "is-active");
      try {
        await uploadAudiobookChunk(uploadId, start, file.slice(start, end));
      } catch (error) {
        uploadFailed = true;
        throw error;
      }
      setAudiobookUploadPartState(start, end, "is-done");
      uploadedBytes += end - start;
      updateAudiobookUploadProgress(uploadedBytes, file.size);
    }
  }

  await Promise.all(Array.from({ length: workerCount }, runAudiobookUploadWorker));

  if (audiobookUploadStatus) audiobookUploadStatus.textContent = "Finishing up...";
  const duration = await probeAudioDuration(file);
  const completeResponse = await fetch(`/api/audiobooks/uploads/${encodeURIComponent(uploadId)}/complete`, {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ durationSeconds: duration }),
  });
  if (completeResponse.status === 409) {
    saveAudiobookUploadMarker(null);
    state.audiobooks.upload = null;
    throw new AudiobookUploadDuplicate();
  }
  if (!(await expectAuthorized(completeResponse, "Could not finish the upload."))) {
    throw new Error("audiobook upload complete failed");
  }

  saveAudiobookUploadMarker(null);
  state.audiobooks.upload = null;
}

async function cancelAudiobookUpload() {
  audiobookUploadQueueCancelled = true;
  const upload = state.audiobooks.upload;
  if (!upload) {
    audiobookUploadModal?.close();
    return;
  }
  upload.cancelled = true;
  try {
    await fetch(`/api/audiobooks/uploads/${encodeURIComponent(upload.id)}`, {
      method: "DELETE",
      credentials: "same-origin",
    });
  } catch (error) {
    console.error(error);
  }
  saveAudiobookUploadMarker(null);
  state.audiobooks.upload = null;
}

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
const AUDIOBOOK_ELSEWHERE_STATUS = "Playing on another device — tap play to pause it there and play here.";

// Progress save, heartbeat, and "playing elsewhere" poll are all driven off
// one shared Worker tick (see getAudiobookSyncWorker) rather than their own
// window.setInterval, so they stay on schedule even while this tab is
// backgrounded. audiobookSyncActive is just "is a player open right now" -
// the worker itself runs for the page's whole lifetime once created, same as
// audiobookAudioEl, since a tick with nothing open to act on is a no-op.
let audiobookSyncWorker = null;
let audiobookSyncTickCount = 0;
let audiobookSyncActive = false;
const AUDIOBOOK_SYNC_TICK_MS = 1000;

function getAudiobookSyncWorker() {
  if (audiobookSyncWorker) return audiobookSyncWorker;
  const worker = new Worker("/static/audiobook-sync-worker.js");
  worker.onmessage = () => {
    audiobookSyncTickCount++;
    if (!audiobookSyncActive || !state.audiobooks.player) return;
    if (audiobookSyncTickCount % (AUDIOBOOK_PLAYBACK_ELSEWHERE_POLL_INTERVAL_MS / AUDIOBOOK_SYNC_TICK_MS) === 0) {
      pollAudiobookPlaybackElsewhere();
    }
    const audio = audiobookAudioEl;
    if (!audio || audio.paused) return;
    if (audiobookSyncTickCount % (AUDIOBOOK_PLAYBACK_HEARTBEAT_INTERVAL_MS / AUDIOBOOK_SYNC_TICK_MS) === 0) {
      sendAudiobookPlaybackHeartbeat();
    }
    if (audiobookSyncTickCount % (AUDIOBOOK_PROGRESS_SAVE_INTERVAL_MS / AUDIOBOOK_SYNC_TICK_MS) === 0) {
      saveAudiobookProgressNow();
    }
  };
  audiobookSyncWorker = worker;
  return worker;
}

let audiobookVolumeAutoHideTimer = null;
const AUDIOBOOK_VOLUME_AUTO_HIDE_MS = 30000;

// The volume popover works the same way as the progress toggle above - tucked
// behind a button until tapped, and folding itself away after
// AUDIOBOOK_VOLUME_AUTO_HIDE_MS of no interaction.
function setAudiobookVolumeExpanded(expanded) {
  audiobookPlayerVolumeWrap?.classList.toggle("is-expanded", expanded);
  audiobookPlayerVolumeToggle?.setAttribute("aria-expanded", String(expanded));
  window.clearTimeout(audiobookVolumeAutoHideTimer);
  audiobookVolumeAutoHideTimer = null;
  if (expanded) {
    audiobookVolumeAutoHideTimer = window.setTimeout(() => setAudiobookVolumeExpanded(false), AUDIOBOOK_VOLUME_AUTO_HIDE_MS);
  }
}

// scheduleAudiobookVolumeAutoHide restarts the auto-hide countdown without
// forcing the popover open - used while the slider is actively being dragged.
function scheduleAudiobookVolumeAutoHide() {
  if (!audiobookPlayerVolumeWrap?.classList.contains("is-expanded")) return;
  window.clearTimeout(audiobookVolumeAutoHideTimer);
  audiobookVolumeAutoHideTimer = window.setTimeout(() => setAudiobookVolumeExpanded(false), AUDIOBOOK_VOLUME_AUTO_HIDE_MS);
}

// renderAudiobookPlaybackElsewhere shows or hides the "playing on another
// device" banner. info is {positionSeconds, ageSeconds, fetchedAt} from
// fetchAudiobookPlaybackStatus, or null once nothing is playing elsewhere
// (either we never found one, or the listener just took over).
function renderAudiobookPlaybackElsewhere(info) {
  audiobookPlaybackElsewhereState = info;
  if (!info) clearAudiobookPlayerStatus(AUDIOBOOK_ELSEWHERE_STATUS);
  renderAudiobookMiniPlayerInfo();
  if (!audiobookPlayerElsewhereBanner) return;
  if (!info) {
    audiobookPlayerElsewhereBanner.classList.add("hidden");
    return;
  }
  if (audiobookPlayerElsewhereText) {
    audiobookPlayerElsewhereText.textContent = `Playing on another device — ${formatMediaTime(estimateAudiobookElsewherePosition(info))}`;
  }
  audiobookPlayerElsewhereBanner.classList.remove("hidden");
}

// estimateAudiobookElsewherePosition works out where the other device has
// gotten to by now, not where it was at its last heartbeat: that report is
// already ageSeconds old when the server hands it over, plus however long ago
// this browser received it. Assumes 1x speed - the server doesn't track rate.
function estimateAudiobookElsewherePosition(info) {
  const sinceFetch = (performance.now() - info.fetchedAt) / 1000;
  const age = Math.min(info.ageSeconds + sinceFetch, AUDIOBOOK_PLAYBACK_MAX_AGE_SECONDS);
  return info.positionSeconds + Math.max(age, 0);
}

// fetchAudiobookPlaybackStatus returns the raw "other devices" picture for an
// audiobook: elsewhere is {positionSeconds, ageSeconds, fetchedAt} for another
// device still playing it (or null), and stopPending says a stop request this
// device sent hasn't been picked up yet. null on a failed request.
async function fetchAudiobookPlaybackStatus(audiobookId) {
  try {
    const response = await fetch(
      `/api/audiobooks/${encodeURIComponent(audiobookId)}/playback-elsewhere?deviceId=${encodeURIComponent(getAudiobookDeviceId())}`,
      { credentials: "same-origin" },
    );
    if (!response.ok) return null;
    const payload = await response.json();
    return {
      elsewhere: payload.active
        ? { positionSeconds: payload.position_seconds || 0, ageSeconds: payload.age_seconds || 0, fetchedAt: performance.now() }
        : null,
      stopPending: !!payload.stop_pending,
    };
  } catch (error) {
    console.error(error);
    return null;
  }
}

async function fetchAudiobookPlaybackElsewhere(audiobookId) {
  const status = await fetchAudiobookPlaybackStatus(audiobookId);
  return status?.elsewhere || null;
}

// fetchAudiobookSavedPosition reads the server's latest saved position for
// a book, or null if it can't be fetched (the caller falls back to the list's
// copy).
async function fetchAudiobookSavedPosition(audiobookId) {
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(audiobookId)}/progress`, { credentials: "same-origin" });
    if (!response.ok) return null;
    const payload = await response.json();
    return payload.found ? Number(payload.position_seconds) || 0 : null;
  } catch (error) {
    console.error(error);
    return null;
  }
}

// pollAudiobookPlaybackElsewhere re-checks for another active device while
// the player is open, so the banner can appear (or disappear) mid-session,
// not just at open time.
async function pollAudiobookPlaybackElsewhere() {
  const player = state.audiobooks.player;
  if (!player) return;
  // While a takeover is still being confirmed, the device being stopped can
  // report itself as playing one last time before it hears the request -
  // showing the banner for that would just flash it back up for a moment.
  if (audiobookTakeoverConfirming) return;
  const info = await fetchAudiobookPlaybackElsewhere(player.id);
  if (state.audiobooks.player?.id !== player.id || audiobookTakeoverConfirming) return;
  renderAudiobookPlaybackElsewhere(info);
}

// pauseAudiobookElsewhereAndPlayHere asks every other device playing this
// book to stop, then takes over playback here from wherever that device had
// gotten to. Used both by the "playing elsewhere" banner's explicit button
// and by the main play button, which does the same thing implicitly - this
// is the one place that actually requests the other device stop, since just
// calling audio.play() locally (e.g. the plain play() in the button handler
// below) only starts a second simultaneous playback.
async function pauseAudiobookElsewhereAndPlayHere() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  const elsewhere = audiobookPlaybackElsewhereState;
  if (!player || !audio || !elsewhere) return;
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/playback-elsewhere/pause`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ deviceId: getAudiobookDeviceId() }),
    });
    if (!(await expectAuthorized(response, "Could not pause the other device."))) return;
  } catch (error) {
    console.error(error);
    showToast("Could not pause the other device.", "error", { title: "Audiobooks" });
    return;
  }
  const targetPosition = estimateAudiobookElsewherePosition(elsewhere);
  renderAudiobookPlaybackElsewhere(null);
  const knownDuration = Number.isFinite(audio.duration) && audio.duration > 0 ? audio.duration : player.durationSeconds || 0;
  if (Number.isFinite(targetPosition) && targetPosition > 0 && (!knownDuration || targetPosition < knownDuration - 1)) {
    audio.currentTime = targetPosition;
  }
  audio.play().catch((error) => console.error(error));
  confirmAudiobookTakeover(player.id).catch((error) => console.error(error));
}

// confirmAudiobookTakeover waits for the other device to actually pick up the
// stop request (the server reports it pending until that device's next
// heartbeat) instead of announcing success the moment the request is sent.
// A device that never answers - a phone whose browser froze the tab in the
// background, say - gets an honest "may still be playing" instead.
let audiobookTakeoverConfirmToken = 0;
let audiobookTakeoverConfirming = false;
const AUDIOBOOK_TAKEOVER_STATUS = "Pausing the other device...";
async function confirmAudiobookTakeover(audiobookId) {
  const token = ++audiobookTakeoverConfirmToken;
  const isCurrent = () => token === audiobookTakeoverConfirmToken && state.audiobooks.player?.id === audiobookId;
  audiobookTakeoverConfirming = true;
  setAudiobookPlayerStatus(AUDIOBOOK_TAKEOVER_STATUS);
  try {
    const deadline = performance.now() + AUDIOBOOK_TAKEOVER_CONFIRM_TIMEOUT_MS;
    while (performance.now() < deadline) {
      await new Promise((resolve) => window.setTimeout(resolve, AUDIOBOOK_TAKEOVER_CONFIRM_POLL_MS));
      if (!isCurrent()) return;
      const status = await fetchAudiobookPlaybackStatus(audiobookId);
      if (!isCurrent()) return;
      if (status && !status.stopPending) {
        showToast("Paused on the other device.", "success", { title: "Audiobooks" });
        return;
      }
    }
    showToast("The other device didn't respond — it may still be playing.", "info", { title: "Audiobooks" });
  } finally {
    if (token === audiobookTakeoverConfirmToken) {
      audiobookTakeoverConfirming = false;
      clearAudiobookPlayerStatus(AUDIOBOOK_TAKEOVER_STATUS);
    }
  }
}

// Set once this device pauses itself because another device took over, and
// held until the listener plays or seeks here again. While set, progress saves
// from this device are skipped - its position is a few seconds ahead of where
// the other one resumed, and the pause event, closing the player, or the tab
// going to the background would otherwise save it over the newer position
// the other device is now writing.
let audiobookYieldedToOtherDevice = false;
let audiobookLastHeartbeatAt = 0;

// sendAudiobookPlaybackHeartbeat reports this device's actual play state and
// position, and self-pauses if another device just forced this one to stop
// (the "stopRequested" flag the server clears as it reports it). resumed is
// set for the heartbeat sent the moment the listener presses play here, which
// tells the server to drop any older stop request instead of acting on it.
async function sendAudiobookPlaybackHeartbeat({ resumed = false } = {}) {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio || !isAudiobookAudioForPlayer(audio, player)) return;
  audiobookLastHeartbeatAt = performance.now();
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/playback-heartbeat`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ deviceId: getAudiobookDeviceId(), positionSeconds: audio.currentTime || 0, isPlaying: !audio.paused, resumed }),
      keepalive: true,
    });
    if (!response.ok) return;
    const payload = await response.json();
    if (payload.stopRequested && state.audiobooks.player?.id === player.id && !audio.paused) {
      audiobookYieldedToOtherDevice = true;
      audio.pause();
      setAudiobookPlayerStatus("Paused — now playing on another device.");
      showToast("Paused here — resumed on another device.", "info", { title: "Audiobooks" });
      pollAudiobookPlaybackElsewhere();
    }
  } catch (error) {
    console.error(error);
  }
}

// sendAudiobookPlaybackStoppedSignal tells the server this device stopped
// regardless of the audio element's actual paused state at the moment - used
// on page unload, where the tab is closing out from under an still-playing
// <audio> and there's no "pause" event to piggyback a heartbeat on.
function sendAudiobookPlaybackStoppedSignal() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/playback-heartbeat`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deviceId: getAudiobookDeviceId(), positionSeconds: audio.currentTime || 0, isPlaying: false }),
    keepalive: true,
  }).catch((error) => console.error(error));
}

function getAudiobookAudioElement() {
  if (audiobookAudioEl) return audiobookAudioEl;
  const audio = new Audio();
  audio.addEventListener("timeupdate", () => {
    updateAudiobookPlayerProgressUI();
    updateAudiobookBufferingUI();
    // Backstop for the worker-driven heartbeat: some mobile browsers suspend
    // workers in a backgrounded tab while still delivering timeupdate to the
    // page as long as audio is playing. Without this a locked phone can keep
    // playing but go quiet to the server, so another device neither sees it
    // as playing nor can tell it to stop.
    if (!audio.paused && performance.now() - audiobookLastHeartbeatAt > AUDIOBOOK_PLAYBACK_HEARTBEAT_INTERVAL_MS * 1.5) {
      sendAudiobookPlaybackHeartbeat();
    }
  });
  audio.addEventListener("loadedmetadata", updateAudiobookPlayerProgressUI);
  // progress fires as more of the file downloads - only the buffered band on
  // the seek bar depends on it.
  audio.addEventListener("progress", updateAudiobookPlayerProgressUI);
  // Buffering state is derived from the element itself (see
  // updateAudiobookBufferingUI), so every event that can change readyState,
  // paused, or seeking just re-derives it rather than each one guessing.
  for (const type of ["waiting", "playing", "canplay", "canplaythrough", "seeking", "seeked", "stalled", "loadstart", "emptied"]) {
    audio.addEventListener(type, updateAudiobookBufferingUI);
  }
  // durationchange covers files whose real length isn't known yet at
  // loadedmetadata (e.g. an M4B with its moov atom at the end of the file,
  // needing an extra range request the browser hasn't made yet) - once it
  // resolves, the progress bar and time labels (which depend on duration)
  // need to be recomputed instead of staying wrong for the rest of playback.
  audio.addEventListener("durationchange", updateAudiobookPlayerProgressUI);
  audio.addEventListener("ratechange", () => setAudiobookMediaSessionPositionState(audio));
  audio.addEventListener("play", () => {
    setAudiobookPlayIcon(true);
    // Save the position the instant playback starts, not just on the 15s
    // timer - this is what clears a "Finished"/"not started" card badge as
    // soon as the listener presses play again, instead of leaving the old
    // marked-as state showing until the next periodic save.
    audiobookYieldedToOtherDevice = false;
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat({ resumed: true });
    setAudiobookMediaSessionPlaybackState(audio);
    clearAudiobookPlayerStatus("Paused — now playing on another device.");
    updateAudiobookBufferingUI();
  });
  audio.addEventListener("pause", () => {
    setAudiobookPlayIcon(false);
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat();
    setAudiobookMediaSessionPlaybackState(audio);
    updateAudiobookBufferingUI();
  });
  audio.addEventListener("ended", () => {
    setAudiobookPlayIcon(false);
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat();
    setAudiobookMediaSessionPlaybackState(audio);
  });
  // seeked fires for every way currentTime can change - the scrub bar,
  // chapter/bookmark jumps, the +/-30s buttons - so hooking it here instead
  // of each seek source is what makes a seek immediately persist and show up
  // on another open instance, rather than waiting on the next 15s save or 5s
  // heartbeat (or being lost entirely if the tab closes before either fires).
  audio.addEventListener("seeked", () => {
    // Moving the playhead here means the listener is back on this device, so
    // its position is worth saving again.
    audiobookYieldedToOtherDevice = false;
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat();
    setAudiobookMediaSessionPositionState(audio);
  });
  audio.addEventListener("error", () => {
    const wasPlaying = !audio.paused;
    setAudiobookPlayIcon(false);
    updateAudiobookBufferingUI();
    // An emptied src (closing the player) also raises "error" on some
    // browsers - only a real load failure is worth telling anyone about.
    if (!audio.getAttribute("src")) return;
    if (reloadAudiobookStreamAfterError(audio, wasPlaying)) return;
    setAudiobookPlayerStatus("Playback error - check your connection, then press play to retry.");
  });
  setupAudiobookMediaSessionHandlers(audio);
  audiobookAudioEl = audio;
  return audio;
}

// The server can replace a book's file mid-listen - an MP3 is swapped for its
// lossless .m4b remux at the same URL - after which the element's next range
// request is answered from a different, differently sized file and fails.
// Reloading the source once, back at the same position, recovers without the
// listener noticing more than a brief stall; a second failure soon after is a
// real problem and is reported as before.
const AUDIOBOOK_STREAM_RELOAD_WINDOW_MS = 30000;
let audiobookStreamReloadedAt = 0;

function reloadAudiobookStreamAfterError(audio, wasPlaying) {
  const player = state.audiobooks.player;
  if (!player || !isAudiobookAudioForPlayer(audio, player)) return false;
  if (Date.now() - audiobookStreamReloadedAt < AUDIOBOOK_STREAM_RELOAD_WINDOW_MS) return false;
  audiobookStreamReloadedAt = Date.now();

  const playerId = player.id;
  const resumeAt = audio.currentTime;
  // Only this reload's outcome counts: if it fails too, the listener must not
  // linger and fire on a later load (reopening the book), where it would seek
  // and autoplay ahead of the open flow's own resume/other-device checks.
  // A second loadstart means some other load replaced this one (the player
  // was closed and a book opened).
  const reload = new AbortController();
  let loadStarts = 0;
  audio.addEventListener("loadstart", () => {
    loadStarts += 1;
    if (loadStarts > 1) reload.abort();
  }, { signal: reload.signal });
  audio.addEventListener("error", () => reload.abort(), { signal: reload.signal });
  audio.addEventListener("loadedmetadata", () => {
    reload.abort();
    if (state.audiobooks.player?.id !== playerId || !isAudiobookAudioForPlayer(audio, state.audiobooks.player)) return;
    audio.currentTime = resumeAt;
    if (wasPlaying) audio.play().catch(() => setAudiobookPlayerStatus("Tap play to keep listening."));
  }, { signal: reload.signal });
  audio.src = player.streamUrl;
  return true;
}

// --- Media Session (lock screen / notification drawer controls) -----------
//
// Without this, a backgrounded tab's notification shows only a generic
// "playing audio" card with just a mute/stop action - Android and iOS both
// drive their now-playing UI off the Media Session API rather than the
// <audio> element itself, so title/author/cover and play/pause/seek/chapter
// controls have to be set up explicitly.

function setAudiobookMediaSessionPlaybackState(audio) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.playbackState = audio.paused ? "paused" : "playing";
}

function setAudiobookMediaSessionPositionState(audio) {
  if (!("mediaSession" in navigator) || !navigator.mediaSession.setPositionState) return;
  const duration = Number.isFinite(audio.duration) ? audio.duration : 0;
  if (!(duration > 0)) return;
  try {
    navigator.mediaSession.setPositionState({
      duration,
      playbackRate: audio.playbackRate || 1,
      position: Math.min(Math.max(audio.currentTime || 0, 0), duration),
    });
  } catch (error) {
    console.error(error);
  }
}

// setAudiobookMediaSessionMetadata fills in the lock-screen/notification
// now-playing card (title, author, cover) so background playback shows real
// book details instead of a generic tab name. Artwork is listed at several
// sizes pointing at the same cover image - the OS picks whichever it needs,
// and we don't know the source image's real dimensions.
function setAudiobookMediaSessionMetadata(item) {
  if (!("mediaSession" in navigator) || typeof MediaMetadata === "undefined") return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: audiobookDisplayTitle(item),
    artist: item.author || "",
    album: "Audiobook",
    artwork: item.cover_url
      ? [96, 128, 192, 256, 384, 512].map((size) => ({ src: item.cover_url, sizes: `${size}x${size}` }))
      : [],
  });
}

// setupAudiobookMediaSessionHandlers wires the OS-level notification/lock-
// screen controls to the same audio element and helpers the in-page player
// buttons use, so background playback stays controllable from the
// notification drawer. Called once, when the shared <audio> element is
// created.
function setupAudiobookMediaSessionHandlers(audio) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.setActionHandler("play", () => {
    if (audiobookPlaybackElsewhereState) {
      pauseAudiobookElsewhereAndPlayHere().catch((error) => console.error(error));
    } else {
      resumeAudiobookPlaybackFromSavedPosition().catch((error) => console.error(error));
    }
  });
  navigator.mediaSession.setActionHandler("pause", () => audio.pause());
  navigator.mediaSession.setActionHandler("stop", () => audio.pause());
  navigator.mediaSession.setActionHandler("seekbackward", (details) => {
    audio.currentTime = Math.max(0, audio.currentTime - (details?.seekOffset || 30));
  });
  navigator.mediaSession.setActionHandler("seekforward", (details) => {
    audio.currentTime = Math.min(audio.duration || audio.currentTime, audio.currentTime + (details?.seekOffset || 30));
  });
  try {
    navigator.mediaSession.setActionHandler("seekto", (details) => {
      if (!details || !Number.isFinite(details.seekTime)) return;
      if (details.fastSeek && "fastSeek" in audio) {
        audio.fastSeek(details.seekTime);
      } else {
        audio.currentTime = details.seekTime;
      }
    });
  } catch (error) {
    // Some browsers (Safari) don't support the "seekto" action - safe to skip.
  }
  updateAudiobookMediaSessionChapterHandlers(false);
}

// updateAudiobookMediaSessionChapterHandlers enables/disables the
// previous/next-track notification buttons for chapter navigation - left
// unset (rather than wired to a no-op) when the open book has no chapters, so
// the OS doesn't show controls that do nothing.
function updateAudiobookMediaSessionChapterHandlers(hasChapters) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.setActionHandler("previoustrack", hasChapters ? () => jumpAudiobookChapter(-1) : null);
  navigator.mediaSession.setActionHandler("nexttrack", hasChapters ? () => jumpAudiobookChapter(1) : null);
}

const AUDIOBOOK_PLAY_ICON_SVG = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5.5v13a1 1 0 0 0 1.5.86l10.5-6.5a1 1 0 0 0 0-1.72L9.5 4.64A1 1 0 0 0 8 5.5Z" /></svg>';
const AUDIOBOOK_PAUSE_ICON_SVG = '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="6" y="5" width="4" height="14" rx="1.2" /><rect x="14" y="5" width="4" height="14" rx="1.2" /></svg>';

function setAudiobookPlayIcon(playing) {
  if (audiobookPlayerPlayIcon) audiobookPlayerPlayIcon.innerHTML = playing ? AUDIOBOOK_PAUSE_ICON_SVG : AUDIOBOOK_PLAY_ICON_SVG;
  audiobookPlayerPlayButton?.setAttribute("aria-label", playing ? "Pause" : "Play");
}

// The player's one-line status (top bar of the full player, subtitle of the
// mini bar). setAudiobookPlayerStatus replaces it; clearAudiobookPlayerStatus
// only clears it if it still says `text`, so a transient message ("Pausing
// the other device...") finishing doesn't wipe out a newer one that replaced
// it in the meantime.
function setAudiobookPlayerStatus(text) {
  if (audiobookPlayerStatus) audiobookPlayerStatus.textContent = text;
  renderAudiobookMiniPlayerInfo();
}

function clearAudiobookPlayerStatus(text) {
  if (audiobookPlayerStatus?.textContent === text) setAudiobookPlayerStatus("");
}

const AUDIOBOOK_BUFFERING_STATUS = "Buffering...";
let audiobookBuffering = false;

// updateAudiobookBufferingUI derives "is this stuck waiting on data" from the
// audio element's own state - it's meant to be playing (not paused/ended) but
// doesn't have enough data to move forward, or is mid-seek - rather than
// tracking waiting/playing events, which miss cases like a seek while paused
// that then never fires "playing" to clear it. Shows as a spinning ring on
// the play button, which is the same node in both the full and mini player.
function updateAudiobookBufferingUI() {
  const audio = audiobookAudioEl;
  const buffering = !!audio && !!audio.getAttribute("src") && !audio.paused && !audio.ended && !audio.error
    && (audio.readyState < HTMLMediaElement.HAVE_FUTURE_DATA || audio.seeking);
  if (buffering === audiobookBuffering) return;
  audiobookBuffering = buffering;
  audiobookPlayerPlayButton?.classList.toggle("is-buffering", buffering);
  audiobookPlayerPlayButton?.setAttribute("aria-busy", String(buffering));
  if (buffering) {
    if (!audiobookPlayerStatus?.textContent) setAudiobookPlayerStatus(AUDIOBOOK_BUFFERING_STATUS);
    else renderAudiobookMiniPlayerInfo();
  } else {
    clearAudiobookPlayerStatus(AUDIOBOOK_BUFFERING_STATUS);
    renderAudiobookMiniPlayerInfo();
  }
}

// renderAudiobookMiniPlayerInfo fills the mini bar's title line and the
// subtitle under it, which carries whatever is most worth knowing while the
// full player is hidden: another device playing, buffering, a status
// message, or else just the author.
function renderAudiobookMiniPlayerInfo() {
  if (!audiobookMiniPlayerSubtitle) return;
  const item = audiobookPlayerOpenItem;
  let subtitle = audiobookActiveChapterLabel || item?.author || "";
  let tone = "";
  const statusText = audiobookPlayerStatus?.textContent || "";
  if (audiobookPlaybackElsewhereState) {
    subtitle = "Playing on another device — press play to take over";
    tone = "warning";
  } else if (audiobookBuffering) {
    subtitle = AUDIOBOOK_BUFFERING_STATUS;
    tone = "busy";
  } else if (statusText) {
    subtitle = statusText;
  }
  audiobookMiniPlayerSubtitle.textContent = subtitle;
  audiobookMiniPlayerSubtitle.dataset.tone = tone;
}

function renderAudiobookMiniPlayerItem(item) {
  if (audiobookMiniPlayerTitle) audiobookMiniPlayerTitle.textContent = audiobookDisplayTitle(item);
  if (audiobookMiniPlayerCover) {
    if (item.cover_url) {
      audiobookMiniPlayerCover.src = item.cover_url;
      audiobookMiniPlayerCover.classList.remove("hidden");
      audiobookMiniPlayerCoverFallback?.classList.add("hidden");
    } else {
      audiobookMiniPlayerCover.removeAttribute("src");
      audiobookMiniPlayerCover.classList.add("hidden");
      audiobookMiniPlayerCoverFallback?.classList.remove("hidden");
    }
  }
  renderAudiobookMiniPlayerInfo();
}

// setAudiobookMiniPlayerOpen shows/hides the mini bar and keeps a body class
// in sync so other mobile chrome (the raised "+" nav button, which would
// otherwise poke up through the bar) can shrink out of its way via CSS.
function setAudiobookMiniPlayerOpen(open) {
  audiobookMiniPlayer?.classList.toggle("is-open", open);
  document.body.classList.toggle("audiobook-mini-player-open", open);
}

// setAudiobookMiniPlayerCollapsed toggles between the full mini bar (progress
// + controls visible) and just its handle peeking above the nav - unlike the
// bar's own close button, playback, the audio element, and every background
// timer are left completely alone either way. The handle is the one thing
// that's always on screen while a book is docked, so there's always
// something to tap to bring the rest back.
function setAudiobookMiniPlayerCollapsed(collapsed) {
  audiobookMiniPlayer?.classList.toggle("is-collapsed", collapsed);
  audiobookMiniPlayerHandle?.setAttribute("aria-label", collapsed ? "Show player" : "Collapse player");
  audiobookMiniPlayerHandle?.setAttribute("aria-expanded", String(!collapsed));
}

// restoreAudiobookBottombarNodes moves the seek slider and transport controls
// back into the full player's bottombar (ahead of the speed/timer/bookmark
// row they started before) and hides the mini player. Safe to call whether or
// not they are currently out in the mini player.
function restoreAudiobookBottombarNodes() {
  if (audiobookPlayerBottombar && audiobookPlayerBottombarRow) {
    if (audiobookPlayerProgressWrap) audiobookPlayerBottombar.insertBefore(audiobookPlayerProgressWrap, audiobookPlayerBottombarRow);
    if (audiobookPlayerControls) audiobookPlayerBottombar.insertBefore(audiobookPlayerControls, audiobookPlayerBottombarRow);
  }
  setAudiobookMiniPlayerOpen(false);
  setAudiobookMiniPlayerCollapsed(false);
}

// minimizeAudiobookPlayer hides the full player dialog but keeps the audio
// element, its state, and all the background timers (heartbeat, progress
// save, elsewhere-poll) running exactly as they were - only the seek slider
// and transport controls move, into the mini bar docked at the bottom of the
// screen, so playback is uninterrupted.
function minimizeAudiobookPlayer() {
  if (!state.audiobooks.player) return;
  setAudiobookVolumeExpanded(false);
  if (audiobookMiniPlayerProgressSlot && audiobookPlayerProgressWrap) audiobookMiniPlayerProgressSlot.appendChild(audiobookPlayerProgressWrap);
  if (audiobookMiniPlayerControlsSlot && audiobookPlayerControls) audiobookMiniPlayerControlsSlot.appendChild(audiobookPlayerControls);
  setAudiobookMiniPlayerCollapsed(false);
  setAudiobookMiniPlayerOpen(true);
  audiobookPlayerModal?.close();
}

// expandAudiobookPlayer reopens the full player from the mini bar, picking up
// the same in-progress playback rather than reloading the audiobook.
function expandAudiobookPlayer() {
  if (!state.audiobooks.player) return;
  restoreAudiobookBottombarNodes();
  audiobookPlayerModal?.showModal();
}

async function openAudiobookPlayer(item) {
  audiobookPlayerOpenItem = item;
  const canEditThisBook = canManageUsers() || canDeleteAudiobooks() || (!!state.auth.user?.id && state.auth.user.id === item.uploaded_by);
  audiobookPlayerEditButton?.classList.toggle("hidden", !canEditThisBook);
  // Re-opening the book that's already loaded (just minimized) should pick up
  // where it is, not reload the audiobook and restart playback from scratch.
  if (state.audiobooks.player?.id === item.id && audiobookAudioEl && !audiobookPlayerModal?.open) {
    expandAudiobookPlayer();
    return;
  }
  // A different book than the one minimized (or none at all) - make sure the
  // slider/controls are back in the dialog before it shows, instead of stuck
  // out in a mini player that's about to be hidden.
  restoreAudiobookBottombarNodes();

  const audio = getAudiobookAudioElement();
  // Switching straight from another book: save its position and tell other
  // devices it stopped now, while player state still points at it. The
  // pause event this queues lands after state (and src) have moved to the
  // new book, and is ignored by saveAudiobookProgressNow's loaded check.
  if (state.audiobooks.player && state.audiobooks.player.id !== item.id) {
    saveAudiobookProgressNow();
    sendAudiobookPlaybackStoppedSignal();
  }
  audio.pause();
  cancelSleepTimer({ silent: true });
  const chapters = normalizeAudiobookChapters(item.chapters || [], item.duration_seconds || 0);
  state.audiobooks.player = { id: item.id, streamUrl: item.stream_url, chapters, durationSeconds: item.duration_seconds || 0, bookmarks: [] };

  audiobookPlayerFlip?.classList.remove("is-flipped");
  if (audiobookPlayerTitle) audiobookPlayerTitle.textContent = audiobookDisplayTitle(item);
  if (audiobookPlayerAuthor) {
    audiobookPlayerAuthor.textContent = item.author || "";
    audiobookPlayerAuthor.classList.toggle("hidden", !item.author);
  }
  if (audiobookPlayerCover) {
    if (item.cover_url) {
      audiobookPlayerCover.src = item.cover_url;
      audiobookPlayerCover.classList.remove("hidden");
      audiobookPlayerCoverFallback?.classList.add("hidden");
    } else {
      audiobookPlayerCover.removeAttribute("src");
      audiobookPlayerCover.classList.add("hidden");
      audiobookPlayerCoverFallback?.classList.remove("hidden");
    }
  }
  renderAudiobookPlayerMeta(item);
  renderAudiobookPlayerChapters(chapters);
  setAudiobookMediaSessionMetadata(item);
  updateAudiobookMediaSessionChapterHandlers(chapters.length > 0);
  audiobookTakeoverConfirmToken++;
  audiobookTakeoverConfirming = false;
  audiobookYieldedToOtherDevice = false;
  setAudiobookPlayerStatus("");
  // Clear the previous book's chapter label before rendering the mini
  // player's info line below - renderAudiobookMiniPlayerItem reads
  // audiobookActiveChapterLabel as its subtitle's first choice, so clearing
  // it after would leave the old book's chapter name showing (as the new
  // book's subtitle) until the next progress tick caught up.
  audiobookActiveChapterLabel = "";
  if (audiobookPlayerChapterLabel) audiobookPlayerChapterLabel.classList.add("hidden");
  renderAudiobookMiniPlayerItem(item);
  if (audiobookPlayerProgress) audiobookPlayerProgress.value = "0";
  if (audiobookPlayerCurrentTime) audiobookPlayerCurrentTime.textContent = "0:00";
  if (audiobookPlayerDuration) audiobookPlayerDuration.textContent = "0:00";
  if (audiobookPlayerRemaining) audiobookPlayerRemaining.textContent = "";
  setAudiobookVolumeExpanded(false);
  if (audiobookPlayerSpeed) audiobookPlayerSpeed.value = "1";
  audio.playbackRate = 1;
  applyDefaultMediaVolume(audio);
  if (audiobookPlayerVolume) audiobookPlayerVolume.value = `${Math.round(loadPreferredMediaVolume() * 100)}`;
  updateSleepTimerButtonUI();

  audiobookPlayerModal?.showModal();
  audio.src = item.stream_url;
  renderAudiobookPlaybackElsewhere(null);

  // Autoplay-on-open has to wait on the audio metadata, the elsewhere-check,
  // and a fresh read of the saved position, whichever lands last - jumping
  // straight to play() the instant metadata is ready would start a second
  // simultaneous playback before we've had a chance to find out this book is
  // already playing on another device. The position is re-read rather than
  // taken from the library list, which is only as fresh as when it loaded
  // and misses anything listened to on another device since.
  let resumeAt = item.position_seconds || 0;
  let metadataReady = false;
  let elsewhereChecked = false;
  let positionChecked = false;
  const tryAutoplayDecision = () => {
    if (!metadataReady || !elsewhereChecked || !positionChecked) return;
    // audio.duration can still be NaN here for some files even after
    // loadedmetadata (e.g. an M4B whose real length needs another range
    // request the browser hasn't made yet) - falling back to the server's
    // own stored duration keeps a resume from silently being skipped just
    // because the upper-bound check had nothing finite to compare against.
    const knownDuration = Number.isFinite(audio.duration) && audio.duration > 0
      ? audio.duration
      : state.audiobooks.player?.durationSeconds || 0;
    if (resumeAt > 1 && (!knownDuration || resumeAt < knownDuration - 2)) {
      audio.currentTime = resumeAt;
    }
    if (audiobookPlaybackElsewhereState) {
      setAudiobookPlayerStatus(AUDIOBOOK_ELSEWHERE_STATUS);
      return;
    }
    audio.play().catch(() => {
      setAudiobookPlayerStatus("Tap play to start listening.");
    });
  };
  const onReady = () => {
    metadataReady = true;
    tryAutoplayDecision();
    audio.removeEventListener("loadedmetadata", onReady);
  };
  audio.addEventListener("loadedmetadata", onReady);

  fetchAudiobookPlaybackElsewhere(item.id).then((info) => {
    elsewhereChecked = true;
    if (state.audiobooks.player?.id !== item.id) return;
    renderAudiobookPlaybackElsewhere(info);
    tryAutoplayDecision();
  }).catch((error) => {
    console.error(error);
    elsewhereChecked = true;
    tryAutoplayDecision();
  });

  fetchAudiobookSavedPosition(item.id).then((position) => {
    if (state.audiobooks.player?.id !== item.id) return;
    // This device's own save from just before (closing the player, then
    // reopening the same book) can still be in flight, so the server may not
    // have it yet - a save that recent is newer than anything it could hold.
    const localSave = audiobookLastLocalSave;
    const localSaveIsFresh = localSave?.id === item.id && performance.now() - localSave.at < AUDIOBOOK_LOCAL_SAVE_TRUST_MS;
    if (position !== null && !localSaveIsFresh) {
      resumeAt = position;
      item.position_seconds = position;
    }
    positionChecked = true;
    tryAutoplayDecision();
  });

  audiobookSyncTickCount = 0;
  audiobookSyncActive = true;
  getAudiobookSyncWorker();

  fetchAudiobookBookmarks(item.id).catch((error) => console.error(error));
}

function renderAudiobookPlayerMeta(item) {
  if (!audiobookPlayerMeta) return;
  const rows = [
    ["Title", audiobookDisplayTitle(item)],
    ["Author", item.author || "Unknown"],
    ["Narrator", item.narrator || ""],
    ["File", item.filename || ""],
    ["Size", formatSize(item.size_bytes || 0)],
    ["Duration", item.duration_seconds > 0 ? formatMediaTime(item.duration_seconds) : "Unknown"],
  ];
  if (item.chapters?.length) rows.push(["Chapters", String(item.chapters.length)]);
  if (item.tags?.length) rows.push(["Tags", item.tags.join(", ")]);
  audiobookPlayerMeta.innerHTML = rows
    .filter(([, value]) => value)
    .map(([label, value]) => `<div class="audiobook-player-meta-row"><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>`)
    .join("");
}

// A seek to a chapter's start can land a few milliseconds short of it - the
// browser snaps to an audio frame boundary - which would otherwise count as
// still being at the very end of the previous chapter: the wrong chapter
// highlighted, a "-0:00" seek bar, and "next chapter" re-jumping to the same
// spot. Positions this close before a chapter start count as inside it.
const AUDIOBOOK_CHAPTER_BOUNDARY_TOLERANCE_SECONDS = 0.5;

// normalizeAudiobookChapters sorts chapters into playback order and repairs
// missing/zero-length/overlapping end times (each chapter runs until the
// next starts, the last until the end of the book) - the same repair the
// server now applies at upload, repeated here for books probed before that.
function normalizeAudiobookChapters(chapters, durationSeconds) {
  const sorted = chapters
    .map((chapter) => ({ ...chapter, start_seconds: Math.max(0, Number(chapter.start_seconds) || 0), end_seconds: Number(chapter.end_seconds) || 0 }))
    .sort((a, b) => a.start_seconds - b.start_seconds);
  sorted.forEach((chapter, index) => {
    const limit = index + 1 < sorted.length ? sorted[index + 1].start_seconds : durationSeconds;
    if (chapter.end_seconds <= chapter.start_seconds || (limit > chapter.start_seconds && chapter.end_seconds > limit)) {
      chapter.end_seconds = limit;
    }
  });
  return sorted;
}

// getCurrentAudiobookChapterBounds returns the {start, end} of the chapter
// containing currentTime, or null when there is no chapter data - callers
// fall back to book-wide progress in that case.
function getCurrentAudiobookChapterBounds(currentTime) {
  const chapters = state.audiobooks.player?.chapters;
  if (!chapters || chapters.length === 0) return null;
  const index = findActiveAudiobookChapterIndex(currentTime, chapters);
  if (index < 0) return null;
  const chapter = chapters[index];
  if (!(chapter.end_seconds > chapter.start_seconds)) return null;
  return { start: chapter.start_seconds, end: chapter.end_seconds };
}

function updateAudiobookPlayerProgressUI() {
  const audio = audiobookAudioEl;
  const player = state.audiobooks.player;
  if (!audio || !player) return;
  const duration = Number.isFinite(audio.duration) ? audio.duration : 0;
  const bookRemaining = (duration || player.durationSeconds) - audio.currentTime;

  // The current-time/duration labels and the slider are chapter-relative
  // when chapter data is available - "how far into this chapter, how much of
  // it is left" - matching how most audiobook apps scrub. The remaining-time
  // line underneath always tracks the whole book regardless.
  const chapterBounds = getCurrentAudiobookChapterBounds(audio.currentTime);
  if (chapterBounds) {
    const chapterLength = chapterBounds.end - chapterBounds.start;
    const elapsedInChapter = Math.min(Math.max(audio.currentTime - chapterBounds.start, 0), chapterLength);
    if (audiobookPlayerCurrentTime) audiobookPlayerCurrentTime.textContent = formatMediaTime(elapsedInChapter);
    if (audiobookPlayerDuration) audiobookPlayerDuration.textContent = `-${formatMediaTime(chapterLength - elapsedInChapter)}`;
    if (audiobookPlayerProgress && !audiobookPlayerProgressDragging) {
      audiobookPlayerProgress.value = String(Math.round((elapsedInChapter / chapterLength) * 1000));
    }
  } else {
    if (audiobookPlayerCurrentTime) audiobookPlayerCurrentTime.textContent = formatMediaTime(audio.currentTime);
    if (audiobookPlayerDuration) audiobookPlayerDuration.textContent = formatMediaTime(duration);
    if (duration > 0 && audiobookPlayerProgress && !audiobookPlayerProgressDragging) {
      audiobookPlayerProgress.value = String(Math.round((audio.currentTime / duration) * 1000));
    }
  }

  if (audiobookPlayerRemaining) {
    audiobookPlayerRemaining.textContent = bookRemaining > 1 ? formatAudiobookRemaining(bookRemaining) : "";
  }
  updateAudiobookProgressTrack(audio, chapterBounds, duration);
  highlightActiveAudiobookChapter(audio.currentTime);
  updateAudiobookChapterNavState(audio.currentTime);
  updateAudiobookActiveChapterLabel(audio.currentTime);
  setAudiobookMediaSessionPositionState(audio);
}

// updateAudiobookProgressTrack paints the seek bar's track: the played part,
// then a lighter band for what's already downloaded ahead of the playhead -
// so it's visible whether jumping forward will play right away or stall.
// Same chapter-relative scale as the slider itself. Only the buffered range
// containing the playhead counts; a range further ahead (from an earlier
// seek) doesn't help playback continue from here.
function updateAudiobookProgressTrack(audio, chapterBounds, duration) {
  if (!audiobookPlayerProgress) return;
  const start = chapterBounds ? chapterBounds.start : 0;
  const length = chapterBounds ? chapterBounds.end - chapterBounds.start : duration;
  let bufferedEnd = audio.currentTime;
  const ranges = audio.buffered;
  for (let i = 0; i < ranges.length; i++) {
    if (ranges.start(i) <= audio.currentTime + 0.5 && ranges.end(i) >= audio.currentTime) {
      bufferedEnd = ranges.end(i);
      break;
    }
  }
  const played = Number(audiobookPlayerProgress.value) / 10;
  const buffered = length > 0 ? Math.min(Math.max(((bufferedEnd - start) / length) * 100, 0), 100) : 0;
  audiobookPlayerProgress.style.setProperty("--played", `${played}%`);
  audiobookPlayerProgress.style.setProperty("--buffered", `${Math.max(buffered, played)}%`);
}

function renderAudiobookPlayerChapters(chapters) {
  if (!audiobookPlayerChapters) return;
  const hasChapters = chapters.length > 0;
  audiobookPlayerChaptersToggle?.classList.toggle("hidden", !hasChapters);
  if (audiobookPlayerPrevChapter) audiobookPlayerPrevChapter.disabled = !hasChapters;
  if (audiobookPlayerNextChapter) audiobookPlayerNextChapter.disabled = !hasChapters;
  // Always start collapsed - the panel overlays the cover art once opened, so
  // it should be an explicit choice rather than something that is already
  // covering part of the player when it opens.
  setAudiobookSidePanel(audiobookPlayerChaptersPanel, audiobookPlayerChaptersToggle, false);
  audiobookPlayerChapters.innerHTML = chapters.map((chapter, index) => `
    <button type="button" class="audiobook-player-chapter" data-chapter-index="${index}" data-chapter-start="${chapter.start_seconds}">
      <span class="audiobook-player-chapter-title">${escapeHTML(chapter.title || `Chapter ${index + 1}`)}</span>
      <span class="audiobook-player-chapter-time">${formatMediaTime(chapter.start_seconds)}</span>
    </button>
  `).join("");

  audiobookPlayerChapters.querySelectorAll(".audiobook-player-chapter").forEach((button) => {
    button.addEventListener("click", () => {
      const audio = getAudiobookAudioElement();
      audio.currentTime = Number(button.dataset.chapterStart) || 0;
      if (audio.paused) audio.play().catch((error) => console.error(error));
      // Below the chapters-panel-as-overlay breakpoint the panel covers the
      // whole player, so close it once a chapter is picked instead of
      // leaving it blocking the view.
      if (window.innerWidth < 900) setAudiobookSidePanel(audiobookPlayerChaptersPanel, audiobookPlayerChaptersToggle, false);
    });
  });
}

function highlightActiveAudiobookChapter(currentTime) {
  const chapters = state.audiobooks.player?.chapters;
  if (!chapters || chapters.length === 0 || !audiobookPlayerChapters) return;
  const activeIndex = findActiveAudiobookChapterIndex(currentTime, chapters);
  audiobookPlayerChapters.querySelectorAll(".audiobook-player-chapter").forEach((button, index) => {
    button.classList.toggle("is-active", index === activeIndex);
  });
}

// audiobookActiveChapterLabel is the current "Chapter N" / chapter title
// text, shared by the full player's label under the author line and the
// mini bar's subtitle (see renderAudiobookMiniPlayerInfo). Kept as module
// state, rather than recomputed inline wherever it's needed, so a change
// only triggers a mini-bar re-render when the chapter actually changes.
let audiobookActiveChapterLabel = "";

function updateAudiobookActiveChapterLabel(currentTime) {
  const chapters = state.audiobooks.player?.chapters || [];
  const activeIndex = findActiveAudiobookChapterIndex(currentTime, chapters);
  const chapter = activeIndex >= 0 ? chapters[activeIndex] : null;
  const label = chapter ? (chapter.title || `Chapter ${activeIndex + 1}`) : "";
  if (audiobookPlayerChapterLabel) {
    audiobookPlayerChapterLabel.textContent = label;
    audiobookPlayerChapterLabel.classList.toggle("hidden", !label);
  }
  if (label !== audiobookActiveChapterLabel) {
    audiobookActiveChapterLabel = label;
    renderAudiobookMiniPlayerInfo();
  }
}

// findActiveAudiobookChapterIndex returns the index of the chapter containing
// currentTime, or -1 if there are no chapters / none has started yet.
function findActiveAudiobookChapterIndex(currentTime, chapters) {
  let activeIndex = -1;
  for (let i = 0; i < chapters.length; i += 1) {
    if (currentTime + AUDIOBOOK_CHAPTER_BOUNDARY_TOLERANCE_SECONDS >= chapters[i].start_seconds) activeIndex = i;
  }
  return activeIndex;
}

function updateAudiobookChapterNavState(currentTime) {
  const chapters = state.audiobooks.player?.chapters || [];
  if (!audiobookPlayerPrevChapter || !audiobookPlayerNextChapter) return;
  if (chapters.length === 0) {
    audiobookPlayerPrevChapter.disabled = true;
    audiobookPlayerNextChapter.disabled = true;
    return;
  }
  const activeIndex = findActiveAudiobookChapterIndex(currentTime, chapters);
  audiobookPlayerPrevChapter.disabled = activeIndex <= 0 && currentTime < 3;
  audiobookPlayerNextChapter.disabled = activeIndex >= chapters.length - 1;
}

// jumpAudiobookChapter moves to the previous/next chapter boundary. Jumping
// "back" while more than 3s into the current chapter restarts that chapter
// instead of skipping to the one before it, matching how most audiobook/
// podcast players treat the previous-track button.
function jumpAudiobookChapter(direction) {
  const audio = getAudiobookAudioElement();
  const chapters = state.audiobooks.player?.chapters || [];
  if (chapters.length === 0) return;
  const activeIndex = findActiveAudiobookChapterIndex(audio.currentTime, chapters);

  if (direction < 0) {
    const current = chapters[activeIndex];
    if (current && audio.currentTime - current.start_seconds > 3) {
      audio.currentTime = current.start_seconds;
    } else {
      audio.currentTime = chapters[Math.max(0, activeIndex - 1)].start_seconds;
    }
  } else {
    const nextIndex = Math.min(chapters.length - 1, activeIndex + 1);
    audio.currentTime = chapters[nextIndex].start_seconds;
  }
  updateAudiobookPlayerProgressUI();
}

// --- Side panels (chapters / bookmarks) -----------------------------------

function setAudiobookSidePanel(panel, toggle, open) {
  panel?.classList.toggle("hidden", !open);
  toggle?.setAttribute("aria-expanded", String(open));
  toggle?.classList.toggle("is-active", open);
}

// toggleAudiobookSidePanel opens the given panel and closes the other one:
// chapters and bookmarks share the same overlay footprint, so showing both at
// once would just stack them on top of each other.
function toggleAudiobookSidePanel(panel, toggle, otherPanel, otherToggle) {
  const opening = panel?.classList.contains("hidden");
  setAudiobookSidePanel(panel, toggle, !!opening);
  if (opening) setAudiobookSidePanel(otherPanel, otherToggle, false);
}

// --- Bookmarks -------------------------------------------------------------

async function fetchAudiobookBookmarks(audiobookId) {
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(audiobookId)}/bookmarks`, { credentials: "same-origin" });
    if (!response.ok) return;
    const payload = await response.json();
    // The player may have moved on to a different book (or closed) while this
    // request was in flight.
    if (state.audiobooks.player?.id !== audiobookId) return;
    state.audiobooks.player.bookmarks = payload.bookmarks || [];
    renderAudiobookPlayerBookmarks();
  } catch (error) {
    console.error(error);
  }
}

function renderAudiobookPlayerBookmarks() {
  const bookmarks = state.audiobooks.player?.bookmarks || [];
  if (!audiobookPlayerBookmarks) return;
  audiobookPlayerBookmarksEmpty?.classList.toggle("hidden", bookmarks.length > 0);
  audiobookPlayerBookmarks.innerHTML = bookmarks.map((bookmark) => `
    <div class="audiobook-player-bookmark-row" data-bookmark-id="${escapeHTML(bookmark.id)}">
      <button type="button" class="audiobook-player-bookmark-jump" data-bookmark-start="${bookmark.position_seconds}">
        <span class="audiobook-player-bookmark-label">${escapeHTML(bookmark.label || "Bookmark")}</span>
        <span class="audiobook-player-bookmark-time">${formatMediaTime(bookmark.position_seconds)}</span>
        <span class="audiobook-player-bookmark-created" title="${escapeHTML(formatDateTimeZoned(bookmark.created_at))}">${escapeHTML(formatRelativeTime(bookmark.created_at))}</span>
      </button>
      <button type="button" class="audiobook-player-bookmark-delete" aria-label="Delete bookmark">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 7h14" /><path d="M8 7V5.5A1.5 1.5 0 0 1 9.5 4h5A1.5 1.5 0 0 1 16 5.5V7" /><path d="M7 7v11.5A1.5 1.5 0 0 0 8.5 20h7a1.5 1.5 0 0 0 1.5-1.5V7" /></svg>
      </button>
    </div>
  `).join("");

  audiobookPlayerBookmarks.querySelectorAll(".audiobook-player-bookmark-row").forEach((row) => {
    const id = row.dataset.bookmarkId;
    row.querySelector(".audiobook-player-bookmark-jump")?.addEventListener("click", (event) => {
      const audio = getAudiobookAudioElement();
      audio.currentTime = Number(event.currentTarget.dataset.bookmarkStart) || 0;
      if (audio.paused) audio.play().catch((error) => console.error(error));
      if (window.innerWidth < 900) setAudiobookSidePanel(audiobookPlayerBookmarksPanel, audiobookPlayerBookmarksToggle, false);
    });
    row.querySelector(".audiobook-player-bookmark-delete")?.addEventListener("click", () => {
      deleteAudiobookBookmark(id).catch((error) => console.error(error));
    });
  });
}

// flashAudiobookActionButton briefly pulses a button green (and, if a label
// element/text is given, swaps its text) to confirm an action landed, on top
// of whatever toast is also shown - a toast is easy to miss while looking at
// the button you just tapped.
function flashAudiobookActionButton(button, labelEl, successText) {
  if (!button) return;
  const originalText = labelEl ? labelEl.textContent : null;
  if (labelEl && successText) labelEl.textContent = successText;
  button.classList.remove("is-success");
  // Force a reflow so re-adding the class restarts the CSS animation even if
  // it is still playing from a previous tap.
  void button.offsetWidth;
  button.classList.add("is-success");
  window.setTimeout(() => {
    button.classList.remove("is-success");
    if (labelEl && originalText !== null) labelEl.textContent = originalText;
  }, 1200);
}

async function createAudiobookBookmark(label) {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/bookmarks`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ positionSeconds: audio.currentTime || 0, label: label || "" }),
    });
    if (!(await expectAuthorized(response, "Could not add bookmark."))) return;
    const bookmark = await response.json();
    if (state.audiobooks.player?.id !== player.id) return;
    state.audiobooks.player.bookmarks = [...(state.audiobooks.player.bookmarks || []), bookmark]
      .sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
    renderAudiobookPlayerBookmarks();
    return bookmark;
  } catch (error) {
    console.error(error);
    showToast("Could not add bookmark.", "error", { title: "Audiobooks" });
  }
}

async function deleteAudiobookBookmark(bookmarkId) {
  const player = state.audiobooks.player;
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player?.id || "")}/bookmarks/${encodeURIComponent(bookmarkId)}`, {
      method: "DELETE",
      credentials: "same-origin",
    });
    if (!(await expectAuthorized(response, "Could not delete bookmark."))) return;
    if (state.audiobooks.player) {
      state.audiobooks.player.bookmarks = (state.audiobooks.player.bookmarks || []).filter((entry) => entry.id !== bookmarkId);
      renderAudiobookPlayerBookmarks();
    }
  } catch (error) {
    console.error(error);
    showToast("Could not delete bookmark.", "error", { title: "Audiobooks" });
  }
}

// --- Sleep timer -------------------------------------------------------------

// A running timer is either a fixed number of minutes (wall clock, endsAt)
// or "end of chapter" (chapterEnd, in book seconds - it follows playback, so
// pausing or changing speed moves it with the audio).
let audiobookSleepTimer = null; // { mode: "minutes"|"chapter", endsAt?, chapterEnd?, minutes?, intervalId }
// What the picker currently shows; only becomes a timer on "Start timer".
let sleepTimerSelection = { mode: "minutes", minutes: 30 };
let sleepTimerRepeat = null; // hold-to-repeat timers for the +/- buttons

const SLEEP_TIMER_MIN_MINUTES = 5;
const SLEEP_TIMER_MAX_MINUTES = 24 * 60;
const SLEEP_TIMER_STORAGE_KEY = "memeindex.sleepTimerMinutes";

function openSleepTimerPicker() {
  if (!sleepTimerModal) return;
  const hasChapters = !!getCurrentAudiobookChapterBounds(audiobookAudioEl?.currentTime || 0);
  sleepTimerChapterPreset?.classList.toggle("hidden", !hasChapters);
  if (audiobookSleepTimer?.mode === "chapter" && hasChapters) {
    sleepTimerSelection = { mode: "chapter", minutes: sleepTimerSelection.minutes };
  } else {
    let minutes = audiobookSleepTimer?.minutes;
    if (!minutes) {
      try { minutes = Number(window.localStorage.getItem(SLEEP_TIMER_STORAGE_KEY)); } catch (error) { /* Optional preference. */ }
    }
    sleepTimerSelection = { mode: "minutes", minutes: clampSleepTimerMinutes(minutes || 30) };
  }
  renderSleepTimerPicker();
  sleepTimerModal.showModal();
}

function clampSleepTimerMinutes(minutes) {
  return Math.min(SLEEP_TIMER_MAX_MINUTES, Math.max(SLEEP_TIMER_MIN_MINUTES, Math.round(minutes)));
}

// Finer steps for short timers, coarser once it's past an hour, so neither
// end of the range takes forever to reach with the +/- buttons.
function stepSleepTimerSelection(direction) {
  if (sleepTimerSelection.mode === "chapter") {
    sleepTimerSelection = { mode: "minutes", minutes: sleepTimerSelection.minutes };
  } else {
    const current = sleepTimerSelection.minutes;
    const step = direction > 0 ? (current < 60 ? 5 : 15) : (current <= 60 ? 5 : 15);
    // Move to the next multiple of the step rather than adding it, so odd
    // values (a remembered 37) snap back onto round numbers.
    const snapped = direction > 0 ? Math.floor(current / step) * step + step : Math.ceil(current / step) * step - step;
    sleepTimerSelection.minutes = clampSleepTimerMinutes(snapped);
    // At a limit the button disables itself and may never see pointerup.
    if (sleepTimerSelection.minutes === current) stopSleepTimerRepeat();
  }
  renderSleepTimerPicker();
}

function formatSleepTimerDuration(minutes) {
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest ? `${hours}h ${rest}m` : `${hours}h`;
}

function renderSleepTimerPicker() {
  if (!sleepTimerValue) return;
  const { mode, minutes } = sleepTimerSelection;
  sleepTimerValue.replaceChildren();
  sleepTimerValue.classList.toggle("is-chapter", mode === "chapter");
  const addPart = (text, className) => {
    const span = document.createElement("span");
    span.className = className;
    span.textContent = text;
    sleepTimerValue.append(span);
  };
  if (mode === "chapter") {
    addPart("End of chapter", "sleep-timer-value-number");
  } else if (minutes < 60) {
    addPart(String(minutes), "sleep-timer-value-number");
    addPart("min", "sleep-timer-value-unit");
  } else {
    addPart(String(Math.floor(minutes / 60)), "sleep-timer-value-number");
    addPart("h", "sleep-timer-value-unit");
    if (minutes % 60) {
      addPart(String(minutes % 60), "sleep-timer-value-number");
      addPart("m", "sleep-timer-value-unit");
    }
  }
  if (sleepTimerMinusButton) sleepTimerMinusButton.disabled = mode === "minutes" && minutes <= SLEEP_TIMER_MIN_MINUTES;
  if (sleepTimerPlusButton) sleepTimerPlusButton.disabled = mode === "minutes" && minutes >= SLEEP_TIMER_MAX_MINUTES;
  sleepTimerPresets?.querySelectorAll(".sleep-timer-preset").forEach((button) => {
    const active = button.dataset.mode === "chapter" ? mode === "chapter" : mode === "minutes" && Number(button.dataset.minutes) === minutes;
    button.classList.toggle("is-active", active);
  });
  sleepTimerOffButton?.classList.toggle("hidden", !audiobookSleepTimer);
  if (sleepTimerStartButton) sleepTimerStartButton.textContent = audiobookSleepTimer ? "Restart timer" : "Start timer";
}

// Press-and-hold on +/- keeps stepping, since there's no upper limit and
// tapping out to several hours would be tedious.
function startSleepTimerRepeat(direction) {
  stopSleepTimerRepeat();
  stepSleepTimerSelection(direction);
  sleepTimerRepeat = {
    timeoutId: window.setTimeout(() => {
      sleepTimerRepeat.intervalId = window.setInterval(() => stepSleepTimerSelection(direction), 110);
    }, 420),
  };
}

function stopSleepTimerRepeat() {
  if (!sleepTimerRepeat) return;
  window.clearTimeout(sleepTimerRepeat.timeoutId);
  window.clearInterval(sleepTimerRepeat.intervalId);
  sleepTimerRepeat = null;
}

async function startSleepTimer(selection) {
  cancelSleepTimer({ silent: true });

  let chapterEnd = null;
  if (selection.mode === "chapter") {
    chapterEnd = getCurrentAudiobookChapterBounds(audiobookAudioEl?.currentTime || 0)?.end;
    if (!chapterEnd) {
      showToast("This audiobook has no chapter data.", "error", { title: "Audiobooks" });
      return;
    }
  } else {
    try { window.localStorage.setItem(SLEEP_TIMER_STORAGE_KEY, String(selection.minutes)); } catch (error) { /* Optional preference. */ }
  }

  // Drop a bookmark right where playback is now, before the timer starts
  // counting down - if the listener drifts off, this marks roughly where
  // they still were awake rather than wherever playback happened to stop.
  const bookmark = await createAudiobookBookmark("Sleep timer started");
  // Another start may have finished while the bookmark was saving - replace
  // it rather than leaving its interval running.
  cancelSleepTimer({ silent: true });

  // Chapter mode ticks faster so the pause lands close to the boundary
  // instead of up to a second into the next chapter.
  const intervalId = window.setInterval(updateSleepTimerButtonUI, selection.mode === "chapter" ? 250 : 1000);
  audiobookSleepTimer = selection.mode === "chapter"
    ? { mode: "chapter", chapterEnd, lastTime: audiobookAudioEl?.currentTime || 0, lastTick: Date.now(), intervalId }
    : { mode: "minutes", endsAt: Date.now() + selection.minutes * 60 * 1000, minutes: selection.minutes, intervalId };
  updateSleepTimerButtonUI();
  // The label already switches to a live countdown, which is its own
  // feedback - just pulse the button rather than swapping text that the
  // countdown tick would immediately overwrite anyway.
  flashAudiobookActionButton(audiobookPlayerSleepTimerButton, null, null);
  const what = selection.mode === "chapter" ? "the end of this chapter" : formatSleepTimerDuration(selection.minutes);
  showToast(
    bookmark ? `Sleep timer set for ${what}. Bookmarked your spot.` : `Sleep timer set for ${what}.`,
    "success",
    { title: "Audiobooks" },
  );
}

// getSleepTimerRemainingSeconds returns how long is left on the running
// timer, or 0 once it's due. In chapter mode only playing across the
// boundary fires: a seek (including the next-chapter button, which lands
// exactly on the boundary) retargets to the end of whichever chapter it
// landed in. A move counts as playback when it's no further than the
// elapsed wall time at the current speed, plus slack for timer jitter.
function getSleepTimerRemainingSeconds() {
  const timer = audiobookSleepTimer;
  if (timer.mode !== "chapter") return Math.max(0, (timer.endsAt - Date.now()) / 1000);
  const audio = audiobookAudioEl;
  if (!audio) return 0;
  const time = audio.currentTime;
  const now = Date.now();
  const rate = audio.playbackRate || 1;
  const moved = time - timer.lastTime;
  const played = moved >= 0 && moved <= ((now - timer.lastTick) / 1000 + 0.5) * rate;
  const crossed = timer.lastTime < timer.chapterEnd && time >= timer.chapterEnd;
  timer.lastTime = time;
  timer.lastTick = now;
  if (crossed && played) return 0;
  // Only retarget after a seek: chapter lookup treats the last moment
  // before a boundary as the next chapter, so doing it during normal
  // playback would hop the target forward just before it's reached.
  if (!played) {
    const bounds = getCurrentAudiobookChapterBounds(time);
    if (bounds) timer.chapterEnd = bounds.end;
  }
  return Math.max(0, (timer.chapterEnd - time) / rate);
}

function formatSleepCountdown(totalSeconds) {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const mmss = `${String(minutes).padStart(hours ? 2 : 1, "0")}:${String(seconds).padStart(2, "0")}`;
  return hours ? `${hours}:${mmss}` : mmss;
}

function cancelSleepTimer({ silent } = {}) {
  if (!audiobookSleepTimer) return;
  window.clearInterval(audiobookSleepTimer.intervalId);
  audiobookSleepTimer = null;
  updateSleepTimerButtonUI();
  if (!silent) showToast("Sleep timer turned off.", "info", { title: "Audiobooks" });
}

// Keeps every sleep timer surface in step: the action button label, the big
// countdown over the cover, and the status line in the picker.
function updateSleepTimerButtonUI() {
  const remainingSeconds = audiobookSleepTimer ? getSleepTimerRemainingSeconds() : 0;
  if (audiobookSleepTimer && remainingSeconds <= 0) {
    sleepTimerElapsed();
    return;
  }
  const active = !!audiobookSleepTimer;
  audiobookPlayerSleepTimerButton?.classList.toggle("is-active", active);
  audiobookPlayerSleepOverlay?.classList.toggle("hidden", !active);
  sleepTimerStatus?.classList.toggle("is-active", active);
  if (!active) {
    if (audiobookPlayerSleepTimerLabel) audiobookPlayerSleepTimerLabel.textContent = "Sleep";
    if (sleepTimerStatus) sleepTimerStatus.textContent = "Pauses playback and bookmarks your spot.";
    // The timer can end while the picker is open; drop its "Turn off" and
    // "Restart" state so the picker matches.
    if (sleepTimerModal?.open) renderSleepTimerPicker();
    return;
  }
  const totalSeconds = Math.ceil(remainingSeconds);
  const countdown = formatSleepCountdown(totalSeconds);
  if (audiobookPlayerSleepTimerLabel) audiobookPlayerSleepTimerLabel.textContent = countdown;
  if (audiobookPlayerSleepOverlayTime) audiobookPlayerSleepOverlayTime.textContent = countdown;
  audiobookPlayerSleepOverlay?.classList.toggle("is-ending", totalSeconds <= 60);
  audiobookPlayerSleepOverlay?.classList.toggle("has-hours", totalSeconds >= 3600);
  if (sleepTimerStatus) {
    sleepTimerStatus.textContent = audiobookSleepTimer.mode === "chapter" ? `Pausing at chapter end · ${countdown}` : `Pausing in ${countdown}`;
  }
}

function sleepTimerElapsed() {
  const chapterEnd = audiobookSleepTimer?.mode === "chapter" ? audiobookSleepTimer.chapterEnd : null;
  cancelSleepTimer({ silent: true });
  const audio = audiobookAudioEl;
  if (audio && !audio.paused) {
    audio.pause();
    // The tick can land a fraction of a second past the boundary - park
    // exactly on it so resuming starts cleanly at the next chapter.
    if (chapterEnd !== null && audio.currentTime > chapterEnd) audio.currentTime = chapterEnd;
    showToast("Sleep timer ended - playback paused.", "info", { title: "Audiobooks" });
  }
}

// isAudiobookAudioForPlayer reports whether the shared audio element is
// actually loaded with the open book's stream. getAttribute (not .src) so
// it compares the same relative URL that was assigned.
function isAudiobookAudioForPlayer(audio, player) {
  return !!player.streamUrl && audio.getAttribute("src") === player.streamUrl;
}

// hasAudiobookPositionToSave is isAudiobookAudioForPlayer plus "something is
// actually loaded": assigning a new src resets readyState to HAVE_NOTHING and
// currentTime to 0 synchronously, so an event queued by the previous book
// (its pause, when switching books mid-playback) would otherwise run after
// the new src is set and save 0 over the new book's real position.
function hasAudiobookPositionToSave(audio, player) {
  return isAudiobookAudioForPlayer(audio, player) && audio.readyState > HTMLMediaElement.HAVE_NOTHING;
}

// The last progress save this tab sent, so reopening a book right after
// closing it can tell its own just-sent save apart from what the server
// returns - see openAudiobookPlayer.
let audiobookLastLocalSave = null;
const AUDIOBOOK_LOCAL_SAVE_TRUST_MS = 5000;

function saveAudiobookProgressNow() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  if (audiobookYieldedToOtherDevice) return;
  // The audio element can still be holding (or just unloading) the previous
  // book for a moment after player state moves to a new one - its position
  // belongs to that book, not this one.
  if (!hasAudiobookPositionToSave(audio, player)) return;
  const position = audio.currentTime || 0;
  audiobookLastLocalSave = { id: player.id, at: performance.now() };
  fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/progress`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ positionSeconds: position }),
    keepalive: true,
  }).catch((error) => console.error(error));

  const item = state.audiobooks.items.find((entry) => entry.id === player.id);
  if (item) item.position_seconds = position;
}

// Below this, a seek onto the saved position isn't worth doing - it's within
// normal drift (a heartbeat/save that landed a couple of seconds before the
// pause) rather than a real "someone else moved the bookmark" difference.
const AUDIOBOOK_RESUME_POSITION_TOLERANCE_SECONDS = 2;

// A press is only superseded by a later one while still paused (see the
// token check below) - once playback actually starts, audio.paused itself
// is enough to tell a stale resolution it's no longer relevant.
let audiobookResumeRequestToken = 0;

// resumeAudiobookPlaybackFromSavedPosition is what the play button actually
// calls (when no other device is actively playing - see the click handler):
// it re-fetches the server's saved position right before playing and jumps
// there first if it disagrees with whatever this tab already had loaded, so
// pressing play always reconciles with the database instead of trusting a
// position that might be stale (e.g. another device paused further along
// since this tab last opened or synced). Mirrors the "elsewhere" takeover's
// own re-check in pauseAudiobookElsewhereAndPlayHere, just without an active
// device to ask to stop first.
async function resumeAudiobookPlaybackFromSavedPosition() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  // Mashing play (or play arriving from both a UI click and a media-session/
  // hardware key in close succession) while still paused would otherwise fire
  // this twice - each fetching the position independently and racing to seek
  // right before calling play(). Only the most recent press's result applies.
  const token = ++audiobookResumeRequestToken;
  const position = await fetchAudiobookSavedPosition(player.id);
  if (token !== audiobookResumeRequestToken) return;
  // The player can close, switch to a different book, or already be playing
  // again (a second press, a media-session/hardware play key) while this was
  // in flight - any of those make the fetched position no longer relevant.
  if (state.audiobooks.player?.id !== player.id || !audio.paused) return;
  if (position !== null && Math.abs(position - audio.currentTime) > AUDIOBOOK_RESUME_POSITION_TOLERANCE_SECONDS) {
    const knownDuration = Number.isFinite(audio.duration) && audio.duration > 0 ? audio.duration : player.durationSeconds || 0;
    if (!knownDuration || position < knownDuration - 1) {
      audio.currentTime = position;
    }
  }
  audio.play().catch((error) => console.error(error));
}

// setAudiobookPlayerPosition is the shared path for the "mark as not
// started"/"mark as finished" controls: it moves playback to an explicit
// position, saves it immediately (not on the usual 15s timer), and updates
// the card grid underneath so the progress badge reflects it right away.
async function setAudiobookPlayerPosition(positionSeconds) {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;

  audio.currentTime = positionSeconds;
  updateAudiobookPlayerProgressUI();

  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/progress`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ positionSeconds }),
    });
    if (!(await expectAuthorized(response, "Could not update playback progress."))) {
      return;
    }
  } catch (error) {
    console.error(error);
    showToast("Could not update playback progress.", "error", { title: "Audiobooks" });
    return;
  }

  const item = state.audiobooks.items.find((entry) => entry.id === player.id);
  if (item) item.position_seconds = positionSeconds;
  renderAudiobooksGrid();
}

function closeAudiobookPlayer() {
  const audio = audiobookAudioEl;
  if (audio) {
    audio.pause();
    saveAudiobookProgressNow();
    // The "pause" event (and its heartbeat) fires as a queued task, which can
    // land after player state below is cleared - send the stopped signal
    // directly here instead of relying on that event's timing.
    sendAudiobookPlaybackStoppedSignal();
    audio.removeAttribute("src");
    audio.load();
  }
  audiobookSyncActive = false;
  audiobookTakeoverConfirmToken++;
  audiobookTakeoverConfirming = false;
  renderAudiobookPlaybackElsewhere(null);
  restoreAudiobookBottombarNodes();
  setAudiobookVolumeExpanded(false);
  cancelSleepTimer({ silent: true });
  state.audiobooks.player = null;
  audiobookPlayerModal?.close();
  if ("mediaSession" in navigator) {
    navigator.mediaSession.metadata = null;
    navigator.mediaSession.playbackState = "none";
  }
  renderAudiobooksGrid();
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
		layoutCardTags(card._tagList, card._tags, 1);
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

function renderMemes({ preserveScroll = false, appendMemes = [] } = {}) {
  const visibleMemes = getVisibleMemes();
  renderSidebarCounts();
  renderSidebarViewState();
  emptyState.classList.toggle("hidden", visibleMemes.length !== 0);
  renderContentMode();
  if (!preserveScroll) {
    contentPanel.scrollTop = 0;
  }
  if (appendMemes.length > 0) {
    appendLoadedMemeCards(appendMemes);
  } else if (!preserveScroll) {
    queueRenderLoadedMemes({ force: true });
  }
}

function buildMemeCardElement(meme) {
  const fragment = cardTemplate.content.cloneNode(true);
  const card = fragment.querySelector(".meme-card");
  const cardHitarea = fragment.querySelector(".card-hitarea");
  const previewFrame = fragment.querySelector(".preview-frame");
  const favoriteButton = fragment.querySelector(".favorite-button");
  const tagList = fragment.querySelector(".tag-list");
  const copyLinkButton = fragment.querySelector(".card-copy-link");
  copyLinkButton.addEventListener("click", () => shareMeme(meme.id, copyLinkButton));
  card._copyLinkButton = copyLinkButton;

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
  card._copyLinkButton.disabled = !canView();
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
    layoutCardTags(card._tagList, card._tags, 1);
  });
}

function appendLoadedMemeCards(memes) {
  if (!Array.isArray(memes) || memes.length === 0) {
    return;
  }

  const fragment = document.createDocumentFragment();
  const cards = memes.map((meme) => {
    const card = buildMemeCardElement(meme);
    fragment.appendChild(card);
    return card;
  });

  memeGrid.appendChild(fragment);
  memeGrid.classList.toggle("has-single-item", state.memes.length === 1);
  cards.forEach((card) => {
    layoutCardTags(card._tagList, card._tags, 1);
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

function layoutCardTags(tagList, tags, maxRows = 1) {
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
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${escapeHTML(meme.contentType)}</span>`;
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
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${escapeHTML(meme.contentType)}</span><span>Use Share to create a 30-day access link.</span>`;
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
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${escapeHTML(meme.originalName)}</span><span>Use Share to create a 30-day access link.</span>`;
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
    preview.addEventListener("durationchange", syncRandomReelMediaControls);
    preview.addEventListener("timeupdate", syncRandomReelMediaControls);
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
  const isVideo = media instanceof HTMLVideoElement;
  const playLabel = randomReelPlay.querySelector(".random-reel-nav-label");

  randomReelPlay.disabled = !supportsMediaControls;
  randomReelVolumeToggle.disabled = !supportsMediaControls;
  randomReelVolume.disabled = !supportsMediaControls;
  randomReelPlay.classList.toggle("hidden", !supportsMediaControls);
  randomReelVolumeWrap.classList.toggle("hidden", !supportsMediaControls);
  randomReelProgressWrap?.classList.toggle("hidden", !isVideo);

  if (!supportsMediaControls) {
    randomReelPlay.setAttribute("aria-label", "Play media");
    randomReelPlay.setAttribute("data-tooltip", "Play");
    randomReelPlayIcon.innerHTML = "&#9654;";
    if (playLabel) playLabel.textContent = "Play";
    randomReelVolumeToggle.setAttribute("aria-label", mobileSearchHeaderMediaQuery.matches ? "Adjust volume" : "Mute media");
    randomReelVolumeToggle.setAttribute("aria-expanded", "false");
    randomReelVolumeIcon.innerHTML = "&#128266;";
    randomReelVolume.value = `${Math.round(loadPreferredMediaVolume() * 100)}`;
    if (randomReelVolumeValue) randomReelVolumeValue.textContent = `${randomReelVolume.value}%`;
    if (randomReelCurrentTime) randomReelCurrentTime.textContent = "0:00";
    if (randomReelDuration) randomReelDuration.textContent = "0:00";
    if (randomReelProgress) randomReelProgress.value = "0";
    return;
  }

  const paused = media.paused;
  const muted = media.muted || media.volume === 0;

  randomReelPlay.setAttribute("aria-label", paused ? "Play media" : "Pause media");
  randomReelPlay.setAttribute("data-tooltip", paused ? "Play" : "Pause");
  randomReelPlayIcon.innerHTML = paused ? "&#9654;" : "&#10074;&#10074;";
  if (playLabel) playLabel.textContent = paused ? "Play" : "Pause";

  const volumeControlExpanded = randomReelVolumeWrap?.classList.contains("is-expanded") || false;
  randomReelVolumeToggle.setAttribute(
    "aria-label",
    mobileSearchHeaderMediaQuery.matches
      ? (volumeControlExpanded ? "Close volume control" : "Adjust volume")
      : (muted ? "Unmute media" : "Mute media"),
  );
  randomReelVolumeToggle.setAttribute("aria-expanded", String(volumeControlExpanded));
  randomReelVolumeIcon.innerHTML = muted ? "&#128263;" : "&#128266;";
  randomReelVolume.value = `${Math.round((media.muted ? 0 : media.volume) * 100)}`;
  if (randomReelVolumeValue) randomReelVolumeValue.textContent = `${randomReelVolume.value}%`;

  if (!isVideo) {
    return;
  }

  const duration = Number.isFinite(media.duration) ? media.duration : 0;
  const currentTime = Number.isFinite(media.currentTime) ? media.currentTime : 0;
  const progressValue = duration > 0
    ? Math.round((currentTime / duration) * MODAL_PROGRESS_SCALE_MAX)
    : 0;

  if (randomReelCurrentTime) randomReelCurrentTime.textContent = formatMediaTime(currentTime);
  if (randomReelDuration) randomReelDuration.textContent = formatMediaTime(duration);
  if (randomReelProgress) {
    randomReelProgress.value = `${Math.max(0, Math.min(MODAL_PROGRESS_SCALE_MAX, progressValue))}`;
    randomReelProgress.disabled = duration <= 0;
  }
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
    pushModalHistoryState("random-reel");
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

function closeRandomReel(options = {}) {
  clearRandomReelUIHideTimer();
  randomReelModal.classList.remove("random-reel-ui-hidden");
  randomReelVolumeWrap?.classList.remove("is-expanded");
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
  if (options.syncHistory !== false) {
    unwindModalHistoryState("random-reel");
  }
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
  icon.innerHTML = `<strong>${pickIcon(type || fileName)}</strong><span>${escapeHTML(fileName)}</span><span>Preview availability depends on the file type.</span>`;
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

function syncMemeCardElements(updatedMeme) {
  if (!updatedMeme?.id) return;

  getCardsByMemeId(updatedMeme.id).forEach((card) => {
    updateMemeCardElement(card, updatedMeme);
    layoutCardTags(card._tagList, card._tags, 1);
  });
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
    if (!memeModal.open) {
      unwindModalHistoryState("meme");
    }
  }
}

// pauseAudiobookForMemeModal pauses an actively-playing audiobook when the
// meme modal opens - attention-grabbing narration competing with whatever the
// listener just opened is rarely wanted. It's a no-op (and leaves the "did we
// pause this" flag false) if nothing was playing, so closing the modal later
// won't start playback the listener hadn't started themselves.
function pauseAudiobookForMemeModal() {
  if (audiobookAudioEl && !audiobookAudioEl.paused) {
    audiobookAudioEl.pause();
    audiobookPausedForMemeModal = true;
  }
}

// resumeAudiobookAfterMemeModal undoes pauseAudiobookForMemeModal once the
// meme modal actually closes.
function resumeAudiobookAfterMemeModal() {
  if (!audiobookPausedForMemeModal) return;
  audiobookPausedForMemeModal = false;
  audiobookAudioEl?.play().catch((error) => console.error(error));
}

function openModalWithMeme(meme) {
  if (!meme) return;
  pauseAudiobookForMemeModal();

  const modalURL = window.location.href;
  const openedFromDeepLink = !memeModal.open && !!deepLinkedMemeID();
  if (openedFromDeepLink) {
    // Give an app launched directly at /m/:id an in-app page to return to.
    // The modal entry keeps the share URL visible until the modal closes.
    window.history.replaceState(window.history.state || {}, "", "/");
  }

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
    pushModalHistoryState("meme", modalURL);
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
    // The admin tag-review flow closes this modal only to immediately reopen
    // it on the next meme - resuming here would just mean pausing again a
    // moment later, an audible blip for no reason.
    resumeAudiobookAfterMemeModal();
  }
  clearMemeDeepLinkURL();
  if (options.syncHistory !== false && !options.continueAdminReview) {
    unwindModalHistoryState("meme");
  }
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
  modalVolumeWrap?.classList.remove("is-expanded");
  modalVolumeToggle?.setAttribute("aria-expanded", "false");
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
    modalVolumeToggle.setAttribute("aria-label", mobileSearchHeaderMediaQuery.matches ? "Adjust volume" : "Mute media");
    modalVolumeToggle.setAttribute("aria-expanded", "false");
    modalVolumeIcon.innerHTML = "&#128266;";
    modalVolume.value = `${Math.round(loadPreferredMediaVolume() * 100)}`;
    if (modalVolumeValue) modalVolumeValue.textContent = `${modalVolume.value}%`;
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

  const volumeControlExpanded = modalVolumeWrap?.classList.contains("is-expanded") || false;
  modalVolumeToggle.setAttribute(
    "aria-label",
    mobileSearchHeaderMediaQuery.matches
      ? (volumeControlExpanded ? "Close volume control" : "Adjust volume")
      : (muted ? "Unmute media" : "Mute media"),
  );
  modalVolumeToggle.setAttribute("aria-expanded", String(volumeControlExpanded));
  modalVolumeIcon.innerHTML = muted ? "&#128263;" : "&#128266;";
  modalVolume.value = `${Math.round((media.muted ? 0 : media.volume) * 100)}`;
  if (modalVolumeValue) modalVolumeValue.textContent = `${modalVolume.value}%`;

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
    const previousMeme = getMemeById(updatedMeme.id);
    const wasUntagged = previousMeme ? isUntaggedMeme(previousMeme) : false;
    const savedMeme = updateMemeInState(updatedMeme) || updatedMeme;
    const isNowUntagged = isUntaggedMeme(savedMeme);
    if (previousMeme && wasUntagged !== isNowUntagged) {
      state.library.counts.untagged = Math.max(
        0,
        Number(state.library.counts.untagged || 0) + (isNowUntagged ? 1 : -1),
      );
      renderSidebarCounts();
    }
    syncMemeCardElements(savedMeme);
    syncMemeGridObserver();
  }

  showToast("Saved meme updates.", "success", { title: "Meme" });

  return updatedMeme || true;
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
  const label = button.querySelector(".modal-action-label, .random-reel-nav-label, .card-copy-label");
  const originalAriaLabel = button.getAttribute("aria-label");
  const originalTooltip = button.getAttribute("data-tooltip");
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
    if (originalAriaLabel) button.setAttribute("aria-label", originalAriaLabel);
    else button.removeAttribute("aria-label");
    if (originalTooltip) button.setAttribute("data-tooltip", originalTooltip);
    else button.removeAttribute("data-tooltip");
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
  window.history.replaceState(window.history.state || {}, "", "/");
}

// consumeShareTargetResult reads the ?shared= status the server sets after an
// operating-system share sheet hands content to the installed PWA, shows a
// matching toast, and strips the marker so a reload does not repeat it.
function consumeShareTargetResult() {
  const params = new URLSearchParams(window.location.search);
  const status = params.get("shared");
  if (!status) return;

  const count = Math.max(0, parseInt(params.get("n") || "0", 10) || 0);
  const messages = {
    ok: [`Added ${count || 1} shared ${count === 1 || !count ? "item" : "items"} to your archive.`, "success", "Shared to MemeIndex"],
    dup: ["That item is already in your archive.", "info", "Shared to MemeIndex"],
    queued: ["Shared link queued for download. It will appear once it finishes.", "info", "Shared to MemeIndex"],
    linkerror: ["Could not import that shared link.", "error", "Shared to MemeIndex"],
    toolarge: ["That shared file is larger than the upload limit.", "error", "Shared to MemeIndex"],
    forbidden: ["You do not have permission to add memes.", "error", "Shared to MemeIndex"],
    empty: ["Nothing usable was in that share.", "info", "Shared to MemeIndex"],
    error: ["Something went wrong handling that share.", "error", "Shared to MemeIndex"],
  };
  const [message, type, title] = messages[status] || messages.error;
  showToast(message, type, { title, duration: type === "error" ? 5200 : 3600 });

  params.delete("shared");
  params.delete("n");
  const query = params.toString();
  window.history.replaceState(window.history.state || {}, "", query ? `/?${query}` : "/");
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

function navigateToAudiobookAuthor(author) {
  if (!author) return;
  state.audiobooks.filter = "";
  state.audiobooks.seriesId = "";
  state.audiobooks.tag = "";
  state.audiobooks.author = author;
  state.audiobooks.view = "books";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
}

function navigateToAudiobookTag(tag) {
  if (!tag) return;
  state.audiobooks.filter = "";
  state.audiobooks.seriesId = "";
  state.audiobooks.author = "";
  state.audiobooks.tag = tag;
  state.audiobooks.view = "books";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
}

function clearAudiobooksActiveFilter() {
  state.audiobooks.filter = "";
  state.audiobooks.seriesId = "";
  state.audiobooks.author = "";
  state.audiobooks.tag = "";
  state.audiobooks.view = "books";
  state.filters.query = "";
  if (tagSearchInput) tagSearchInput.value = "";
  syncAudiobooksURL({ push: true });
  navigateToView("audiobooks");
}

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

// Fetch the archive via XHR so an expired session surfaces as a handled error
// instead of navigating the tab to a broken/forbidden page.
adminBackupDownload?.addEventListener("click", async (event) => {
  event.preventDefault();
  const link = adminBackupDownload;
  if (link.dataset.busy === "1") return;
  link.dataset.busy = "1";
  const previousText = link.textContent;
  link.textContent = "Preparing download...";
  try {
    const response = await fetch("/api/admin/backup/download", { cache: "no-store" });
    if (!(await expectAuthorized(response, "Could not download the backup."))) {
      return;
    }
    if (!response.ok) {
      adminBackupStatus.textContent = await readAPIErrorMessage(response, "Could not download the backup.");
      showToast("Could not download the backup.", "error", { title: "Backup & Restore" });
      return;
    }
    const blob = await response.blob();
    const filename = state.admin.backupStatus?.filename || "memeindex-backup.tar.gz";
    const objectURL = URL.createObjectURL(blob);
    const temp = document.createElement("a");
    temp.href = objectURL;
    temp.download = filename;
    document.body.appendChild(temp);
    temp.click();
    temp.remove();
    window.setTimeout(() => URL.revokeObjectURL(objectURL), 10000);
  } catch (error) {
    console.error(error);
    adminBackupStatus.textContent = "Could not download the backup.";
    showToast("Could not download the backup.", "error", { title: "Backup & Restore" });
  } finally {
    link.textContent = previousText;
    delete link.dataset.busy;
  }
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

modalVolumeToggle?.addEventListener("click", (event) => {
  showMemeModalUI();
  const media = getModalMediaControlTarget();
  if (!media) return;

  if (mobileSearchHeaderMediaQuery.matches) {
    event.preventDefault();
    modalVolumeWrap.classList.toggle("is-expanded");
    syncModalMediaControls();
    return;
  }

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

randomReelVolumeToggle?.addEventListener("click", (event) => {
  showRandomReelUI();
  const media = getRandomReelMediaControlTarget();
  if (!media) return;

  if (mobileSearchHeaderMediaQuery.matches) {
    event.preventDefault();
    randomReelVolumeWrap.classList.toggle("is-expanded");
    syncRandomReelMediaControls();
    return;
  }

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

randomReelProgress?.addEventListener("input", () => {
  showRandomReelUI();
  const media = getRandomReelMediaControlTarget();
  if (!(media instanceof HTMLVideoElement)) return;

  const duration = Number.isFinite(media.duration) ? media.duration : 0;
  if (duration <= 0) return;

  const progressRatio = Number(randomReelProgress.value) / MODAL_PROGRESS_SCALE_MAX;
  media.currentTime = Math.max(0, Math.min(duration, duration * progressRatio));
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

function noteMemeGridUserScroll() {
  if (document.body.classList.contains("library-mode")) {
    memeGridScrollRevision += 1;
  }
}

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
