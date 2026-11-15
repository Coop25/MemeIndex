// The meme library: loading pages of memes, the grid and its cards, search
// and tag suggestions, and keeping meme state and favorites in step.

const LIBRARY_VIEW_TITLES = Object.freeze({
  library: "All Items",
  favorites: "Favorites",
  videos: "Videos",
  images: "Images",
  mp3s: "Audio",
  untagged: "Untagged",
  files: "Files",
});
let topTagSuggestionAbortController = null;
let memeGridObserver = null;
let memeGridLoadArmed = true;
let memeGridLastRequestedPage = 0;
let memeGridScrollRevision = 0;
let memeGridLastLoadScrollRevision = 0;
let memeGridRenderFrame = null;
let memePageFetchSequence = 0;
let memePendingPageIndex = 0;
const MEME_PAGE_SIZE = 100;

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
		if (state.audiobooks.view === "stats") {
			// The Stats page needs neither the full book list nor the series
			// list - skip both fetches rather than loading them only to throw
			// the result away. renderAudiobookSeriesFilter still runs (off
			// whatever series data is already cached) purely for the subnav's
			// active-button highlighting, which doesn't depend on it being fresh.
			renderAudiobookSeriesFilter();
			await fetchAudiobookStatsOverview();
			renderContentMode();
			return;
		}
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
    card.style.removeProperty("--preview-ratio");
    fitCardToPreviewRatio(card);
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
    // Decode off the main thread and let the browser hold off on cards that
    // are far outside the viewport.
    img.loading = "lazy";
    img.decoding = "async";
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
    syncSpotlightFavorite();
    fetchVaultDashboard({ keepSpotlight: true }).catch((error) => console.error(error));
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
  if (currentSpotlightMeme()?.id === updatedMeme.id) renderSpotlightDetails();
  if (upcomingSpotlightItems().some((item) => item.id === updatedMeme.id)) renderSpotlightQueue();
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

function setLibraryViewMode(mode) {
	state.library.viewMode = mode === "list" ? "list" : "grid";
	try { window.localStorage.setItem("memevault.viewMode", state.library.viewMode); } catch (error) { /* Optional preference. */ }
	renderContentMode();
	queueRenderLoadedMemes({ force: true });
}

function noteMemeGridUserScroll() {
  if (document.body.classList.contains("library-mode")) {
    memeGridScrollRevision += 1;
  }
}
