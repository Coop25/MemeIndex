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
    nextOffset: 0,
    hasMore: false,
    loading: false,
  },
  auth: {
    enabled: false,
    authenticated: true,
    user: null,
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
const authLogout = document.querySelector("#auth-logout");
const uploadModalClose = document.querySelector("#upload-modal-close");
const uploadPreview = document.querySelector("#upload-preview");
const uploadFileInput = document.querySelector("#upload-file-input");
const uploadTagChips = document.querySelector("#upload-tag-chips");
const uploadTagsInput = document.querySelector("#upload-tags-input");
const uploadTagSuggestions = document.querySelector("#upload-tag-suggestions");
const uploadTagsHidden = document.querySelector("#upload-tags-hidden");
const contentPanel = document.querySelector(".content-panel");
const memeGridTopSpacer = document.querySelector("#meme-grid-top-spacer");
const memeGridBottomSpacer = document.querySelector("#meme-grid-bottom-spacer");
const memeGrid = document.querySelector("#meme-grid");
const memeGridSentinel = document.querySelector("#meme-grid-sentinel");
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
const modalTitle = document.querySelector("#modal-title");
const modalMeta = document.querySelector("#modal-meta");
const modalCloseButton = document.querySelector("#modal-close");
const modalTagChips = document.querySelector("#modal-tag-chips");
const modalTagsInput = document.querySelector("#modal-tags-input");
const modalTagSuggestions = document.querySelector("#modal-tag-suggestions");
const modalNotesInput = document.querySelector("#modal-notes-input");
const modalOpenLink = document.querySelector("#modal-open-link");
const modalSave = document.querySelector("#modal-save");
const modalDelete = document.querySelector("#modal-delete");
const modalFavorite = document.querySelector("#modal-favorite");
const randomReelModal = document.querySelector("#random-reel-modal");
const randomReelStage = document.querySelector("#random-reel-stage");
const randomReelMedia = document.querySelector("#random-reel-media");
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
let randomReelTouchStartY = null;
let randomReelTouchDeltaY = 0;
let randomReelTouchActive = false;
let randomReelTouchBlocked = false;
let randomReelStepLock = false;
let memeGridObserver = null;
let memeGridRenderFrame = null;
let memeGridLastMountedRangeKey = "";
let memeLoadMorePromise = null;
const memeGridPageHeights = new Map();
const memeGridPageCache = new Map();
const drawerMediaQuery = window.matchMedia("(max-width: 1100px)");
const MEME_PAGE_SIZE = 72;
const MEME_GRID_MIN_WIDTH = 300;
const MEME_GRID_GAP = 3;
const MEME_GRID_MAX_MOUNTED_PAGES = 4;
const MEME_LOAD_AHEAD_VIEWPORTS = 1.5;

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

function canAdd() {
  return !!state.auth.permissions?.canAdd;
}

function canManage() {
  return !!state.auth.permissions?.canManage;
}

function permissionLabel() {
  if (canManage()) return "Manage";
  if (canAdd()) return "View + Add";
  if (canView()) return "View only";
  return "No access";
}

function renderAuthState() {
  const displayName = state.auth.user?.display_name || state.auth.user?.username || "Local access";
  authName.textContent = displayName;
  authRole.textContent = permissionLabel();
  authAvatarFallback.textContent = getAuthInitials();
  authTrigger.title = `${displayName} (${permissionLabel()})`;
  authLogout.href = state.auth.logoutURL || "/auth/logout";
  authLogout.classList.toggle("hidden", !state.auth.enabled || !state.auth.authenticated);
  openUploadModalButton.disabled = !canAdd();
  openUploadModalButton.setAttribute("aria-disabled", String(!canAdd()));
  openUploadModalButton.title = canAdd() ? "Add File" : "You do not have permission to upload";

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
    permissions: payload.permissions || {
      canView: false,
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

function syncMemeGridObserver() {
  if (!memeGridSentinel) {
    return;
  }

  memeGridSentinel.classList.add("hidden");

  if (state.library.hasMore) {
    setMemeGridStatus(state.library.loading ? "Loading more memes..." : "Scroll for more memes", false);
    return;
  }

  if (state.memes.length > 0) {
    setMemeGridStatus(`Loaded ${state.memes.length} meme${state.memes.length === 1 ? "" : "s"}.`, false);
  } else {
    setMemeGridStatus("", true);
  }
}

function shouldLoadMoreMemes() {
  if (!contentPanel || !state.library.hasMore || state.library.loading || memeLoadMorePromise) {
    return false;
  }

  const distanceToBottom = contentPanel.scrollHeight - (contentPanel.scrollTop + contentPanel.clientHeight);
  const loadAheadThreshold = contentPanel.clientHeight * MEME_LOAD_AHEAD_VIEWPORTS;
  return distanceToBottom <= loadAheadThreshold;
}

function maybeLoadMoreMemes() {
  if (!shouldLoadMoreMemes()) {
    return memeLoadMorePromise || Promise.resolve();
  }

  memeLoadMorePromise = fetchMemes({ reset: false })
    .catch((error) => {
      console.error(error);
    })
    .finally(() => {
      memeLoadMorePromise = null;
    });

  return memeLoadMorePromise;
}

async function fetchMemes({ reset = true } = {}) {
  if (state.library.loading) {
    return;
  }

  state.library.loading = true;
  syncMemeGridObserver();

  try {
    const params = new URLSearchParams();
    if (state.filters.tag) params.set("tag", state.filters.tag);
    if (state.filters.view && state.filters.view !== "library") params.set("view", state.filters.view);
    params.set("offset", reset ? "0" : `${state.library.nextOffset}`);
    params.set("limit", `${MEME_PAGE_SIZE}`);

    const response = await fetch(`/api/memes?${params.toString()}`);
    if (!(await expectAuthorized(response, "Failed to load memes."))) {
      throw new Error("Failed to load memes");
    }

    const payload = await response.json();
    state.library.counts = payload.counts || state.library.counts;
    state.library.hasMore = !!payload.has_more;
    state.library.nextOffset = Number(payload.next_offset || 0);
    state.memes = reset ? (payload.memes || []) : [...state.memes, ...(payload.memes || [])];
    renderMemes({ append: !reset });
  } finally {
    state.library.loading = false;
    syncMemeGridObserver();
  }
}

async function applyTagSearch(rawValue) {
  state.filters.tag = normalizeTagValue(rawValue);
  await fetchMemes({ reset: true });
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

function renderMemes({ append = false } = {}) {
  const visibleMemes = getVisibleMemes();
  renderSidebarCounts();
  renderSidebarViewState();
  emptyState.classList.toggle("hidden", visibleMemes.length !== 0);
  if (!append) {
    contentPanel.scrollTop = 0;
    queueRenderLoadedMemes({ force: true });
    return;
  }
  queueRenderLoadedMemes();
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

function getGridLayoutMetrics() {
  const gridWidth = Math.max(memeGrid.clientWidth || contentPanel.clientWidth || 0, MEME_GRID_MIN_WIDTH);
  const columns = Math.max(1, Math.floor((gridWidth + MEME_GRID_GAP) / (MEME_GRID_MIN_WIDTH + MEME_GRID_GAP)));
  const cardWidth = (gridWidth - ((columns - 1) * MEME_GRID_GAP)) / columns;
  const rowHeight = Math.max(220, (cardWidth / 1.1) + MEME_GRID_GAP);
  return { columns, rowHeight };
}

function estimatePageHeight(pageIndex, totalItems, metrics = getGridLayoutMetrics()) {
  const startIndex = pageIndex * MEME_PAGE_SIZE;
  const itemCount = Math.max(0, Math.min(MEME_PAGE_SIZE, totalItems - startIndex));
  if (itemCount === 0) {
    return 0;
  }

  const rows = Math.ceil(itemCount / metrics.columns);
  return Math.max(0, (rows * metrics.rowHeight) - MEME_GRID_GAP);
}

function sumPageHeights(startPage, endPage, totalItems, metrics) {
  let totalHeight = 0;
  for (let pageIndex = startPage; pageIndex < endPage; pageIndex += 1) {
    totalHeight += memeGridPageHeights.get(pageIndex) ?? estimatePageHeight(pageIndex, totalItems, metrics);
  }
  return totalHeight;
}

function getMountedPageRange(totalItems, metrics = getGridLayoutMetrics()) {
  const totalPages = Math.ceil(totalItems / MEME_PAGE_SIZE);
  if (totalPages <= MEME_GRID_MAX_MOUNTED_PAGES) {
    return { startPage: 0, endPage: totalPages };
  }

  let anchorPage = 0;
  let remainingScroll = contentPanel.scrollTop;
  for (let pageIndex = 0; pageIndex < totalPages; pageIndex += 1) {
    const pageHeight = memeGridPageHeights.get(pageIndex) ?? estimatePageHeight(pageIndex, totalItems, metrics);
    if (remainingScroll < pageHeight) {
      anchorPage = pageIndex;
      break;
    }
    remainingScroll -= pageHeight;
    anchorPage = pageIndex;
  }

  let startPage = Math.max(0, anchorPage - 1);
  startPage = Math.min(startPage, Math.max(0, totalPages - MEME_GRID_MAX_MOUNTED_PAGES));
  const endPage = Math.min(totalPages, startPage + MEME_GRID_MAX_MOUNTED_PAGES);
  return { startPage, endPage };
}

function buildMemePageElement(pageIndex, memes, metrics) {
  const page = document.createElement("div");
  page.className = "meme-grid-page";
  page.dataset.pageIndex = `${pageIndex}`;
  page.style.display = "contents";
  page._cards = [];

  const fragment = document.createDocumentFragment();
  memes.forEach((meme) => {
    const card = buildMemeCardElement(meme);
    page._cards.push(card);
    fragment.appendChild(card);
  });
  page.appendChild(fragment);

  page._cards.forEach((card) => {
    layoutCardTags(card._tagList, card._tags);
  });

  return page;
}

function renderLoadedMemes({ force = false } = {}) {
  const memes = getVisibleMemes();
  if (!memes.length) {
    memeGrid.replaceChildren();
    memeGridPageCache.clear();
    memeGridPageHeights.clear();
    memeGridLastMountedRangeKey = "";
    memeGridTopSpacer.classList.add("hidden");
    memeGridBottomSpacer.classList.add("hidden");
    return;
  }

  const metrics = getGridLayoutMetrics();
  const totalPages = Math.ceil(memes.length / MEME_PAGE_SIZE);
  const { startPage, endPage } = getMountedPageRange(memes.length, metrics);
  const rangeKey = `${metrics.columns}:${startPage}:${endPage}:${totalPages}`;

  if (!force && rangeKey === memeGridLastMountedRangeKey) {
    return;
  }
  memeGridLastMountedRangeKey = rangeKey;

  if (force) {
    Array.from(memeGrid.querySelectorAll(".meme-card")).forEach((card) => card.remove());
    memeGridPageCache.clear();
  }

  const desiredPages = [];
  for (let pageIndex = startPage; pageIndex < endPage; pageIndex += 1) {
    const pageMemes = memes.slice(pageIndex * MEME_PAGE_SIZE, Math.min(memes.length, (pageIndex + 1) * MEME_PAGE_SIZE));
    let page = memeGridPageCache.get(pageIndex);
    if (!page || force) {
      page = buildMemePageElement(pageIndex, pageMemes, metrics);
      memeGridPageCache.set(pageIndex, page);
    }
    desiredPages.push(page);
  }

  let insertBeforeNode = null;
  for (let index = desiredPages.length - 1; index >= 0; index -= 1) {
    const page = desiredPages[index];
    const firstCard = page._cards?.[0];
    if (!firstCard) {
      continue;
    }
    if (firstCard.parentNode !== memeGrid || firstCard.nextSibling !== insertBeforeNode) {
      for (let cardIndex = page._cards.length - 1; cardIndex >= 0; cardIndex -= 1) {
        memeGrid.insertBefore(page._cards[cardIndex], insertBeforeNode);
      }
    }
    insertBeforeNode = firstCard;
  }

  for (const [pageIndex, page] of memeGridPageCache) {
    if (pageIndex < startPage || pageIndex >= endPage) {
      page._cards?.forEach((card) => {
        if (card.parentNode === memeGrid) {
          card.remove();
        }
      });
      memeGridPageCache.delete(pageIndex);
    }
  }

  desiredPages.forEach((page) => {
    const pageIndex = Number(page.dataset.pageIndex);
    memeGridPageHeights.set(pageIndex, estimatePageHeight(pageIndex, memes.length, metrics));
  });

  const topHeight = sumPageHeights(0, startPage, memes.length, metrics);
  const bottomHeight = sumPageHeights(endPage, totalPages, memes.length, metrics);
  memeGridTopSpacer.style.height = `${topHeight}px`;
  memeGridBottomSpacer.style.height = `${bottomHeight}px`;
  memeGridTopSpacer.classList.toggle("hidden", topHeight <= 0);
  memeGridBottomSpacer.classList.toggle("hidden", bottomHeight <= 0);
}

function queueRenderLoadedMemes(options = {}) {
  if (options.force) {
    if (memeGridRenderFrame) {
      window.cancelAnimationFrame(memeGridRenderFrame);
      memeGridRenderFrame = null;
    }
    renderLoadedMemes({ force: true });
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

function applyDefaultMediaVolume(media) {
  media.volume = 0.10;
  media.muted = false;
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
    video.controls = true;
    video.preload = "metadata";
    applyDefaultMediaVolume(video);
    return video;
  }

  if (meme.contentType.startsWith("audio/")) {
    const audio = document.createElement("audio");
    audio.src = meme.filePath;
    audio.controls = true;
    audio.preload = "metadata";
    applyDefaultMediaVolume(audio);
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
    randomReelVolume.value = "10";
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
  randomReelStage.classList.add(direction > 0 ? "is-stepping-next" : "is-stepping-prev", "is-loading");
}

function endRandomReelTransition() {
  randomReelStage.classList.remove("is-stepping-next", "is-stepping-prev", "is-loading");
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
  await resetRandomReelSession();
  const payload = await fetchRandomReelStep("next");
  if (!payload?.meme) {
    window.alert("No memes available yet.");
    return;
  }

  randomReelCanGoPrev = !!payload.can_go_prev;
  renderRandomReelMeme(payload.meme);

  if (!randomReelModal.open) {
    randomReelModal.showModal();
  }
  setRandomReelScrollLock(true);
  showRandomReelUI();
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
  memeGridLastMountedRangeKey = "";

  return state.memes[index];
}

function openModal(id) {
  const meme = getMemeById(id);
  if (!meme) return;

  activeMemeId = id;
  modalPreview.innerHTML = "";
  modalPreview.appendChild(buildModalPreview(meme));
  modalTitle.textContent = meme.originalName;
  modalMeta.textContent = `${formatSize(meme.sizeBytes)} * ${meme.contentType}`;
  setModalTags(meme.tags || []);
  modalTagsInput.value = "";
  renderTagEditor();
  renderTagSuggestions();
  modalNotesInput.value = meme.notes || "";
  modalOpenLink.href = meme.filePath;
  applyFavoriteStateToButton(modalFavorite, meme.favorite);
  modalTagsInput.disabled = !canManage();
  modalNotesInput.readOnly = !canManage();
  modalFavorite.disabled = !canView();
  modalSave.disabled = !canManage();
  modalDelete.disabled = !canManage();
  modalFavorite.title = canView() ? "" : "You do not have permission to favorite memes";
  modalSave.title = canManage() ? "" : "You do not have permission to edit memes";
  modalDelete.title = canManage() ? "" : "You do not have permission to delete memes";
  modalSnapshot = {
    favorite: !!meme.favorite,
    notes: meme.notes || "",
    tags: getModalTagValues().sort().join(", "),
  };

  if (!memeModal.open) {
    memeModal.showModal();
  }
  overlayClose.classList.remove("hidden");
}

function hasUnsavedModalChanges() {
  if (!canManage()) return false;
  if (!activeMemeId || !modalSnapshot) return false;
  return (
    modalFavorite.classList.contains("is-active") !== modalSnapshot.favorite ||
    modalNotesInput.value !== modalSnapshot.notes ||
    getModalTagValues().sort().join(", ") !== modalSnapshot.tags
  );
}

function closeModal() {
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
  activeMemeId = null;
  modalSnapshot = null;
  overlayClose.classList.add("hidden");
  return true;
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
  if (!canManage()) return;
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
  if (!canManage()) return;
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
    removeButton.addEventListener("pointerdown", (event) => {
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
  if (!canManage()) return;
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
  if (!canManage()) {
    window.alert("You do not have permission to edit memes.");
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

  await fetchMemes();
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
  if (!canManage()) {
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

  closeModal();
  await fetchMemes();
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
  await fetchMemes();
  uploadModal.close();
});

openUploadModalButton.addEventListener("click", () => {
  if (!canAdd()) return;
  uploadStatus.textContent = "";
  uploadForm.reset();
  resetUploadTags();
  clearUploadPreview();
  uploadModal.showModal();
});

openRandomReelButton?.addEventListener("click", () => {
  openRandomReel().catch((error) => {
    console.error(error);
  });
});

uploadModalClose.addEventListener("click", () => {
  clearUploadPreview();
  uploadModal.close();
});

uploadModal.addEventListener("click", (event) => {
  if (event.target !== uploadModal) return;
  clearUploadPreview();
  uploadModal.close();
});

uploadModal.addEventListener("cancel", (event) => {
  event.preventDefault();
  clearUploadPreview();
  uploadModal.close();
});

uploadFileInput.addEventListener("change", () => {
  renderUploadPreview(uploadFileInput.files?.[0], uploadFileInput.files?.length ?? 0);
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
    fetchMemes({ reset: true }).catch((error) => {
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

document.addEventListener("click", (event) => {
  if (!authPanelContains(event.target)) {
    closeAuthMenu();
  }
});

overlayClose.addEventListener("click", () => {
  closeModal();
});

modalCloseButton?.addEventListener("click", () => {
  closeModal();
});

memeModal.addEventListener("close", () => {
  activeMemeId = null;
  modalSnapshot = null;
  overlayClose.classList.add("hidden");
});

memeModal.addEventListener("click", (event) => {
  if (event.target !== memeModal) return;
  closeModal();
});

memeModal.addEventListener("cancel", (event) => {
  event.preventDefault();
  closeModal();
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
  syncRandomReelMediaControls();
});

randomReelVolume?.addEventListener("input", () => {
  showRandomReelUI();
  const media = getRandomReelMediaControlTarget();
  if (!media) return;

  const volume = Number(randomReelVolume.value) / 100;
  media.volume = volume;
  media.muted = volume === 0;
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
  if (!canManage()) return;
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
  if (!canManage()) return;
  if (!activeMemeId) return;
  await deleteMeme(activeMemeId);
});

modalTagsInput.addEventListener("input", () => {
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
    event.preventDefault();
    if (activeSuggestionIndex >= 0 && modalSuggestionState[activeSuggestionIndex]) {
      addModalTag(modalSuggestionState[activeSuggestionIndex]);
    } else {
      addModalTag(modalTagsInput.value);
    }
    return;
  }

  if (event.key === "Backspace" && !modalTagsInput.value && modalTagState.length > 0) {
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
  .then(() => fetchMemes())
  .catch((error) => {
    console.error(error);
    uploadStatus.textContent = "Could not load existing memes.";
  });

window.addEventListener("resize", () => {
  syncResponsiveSidebar();
  memeGridLastMountedRangeKey = "";
  queueRenderLoadedMemes({ force: true });
  syncMemeGridObserver();
  maybeLoadMoreMemes();
});

contentPanel?.addEventListener("scroll", () => {
  queueRenderLoadedMemes();
  maybeLoadMoreMemes();
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
