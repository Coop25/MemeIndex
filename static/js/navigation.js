// Browser history and URL state for views and modals, the admin tab rail,
// the sidebar, and switching between views.

const ADMIN_TAB_GROUPS = Object.freeze(["Overview", "Tags", "Imports", "Operations", "Audiobooks"]);

function isValidAdminTab(tab) {
  return Object.prototype.hasOwnProperty.call(ADMIN_TABS, tab);
}

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
  return computeAdminTabBadgeCounts(state.admin);
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

// Counts last drawn on the rail, so pollers only touch the DOM when one changes.
let adminAppliedBadgeCounts = null;

// refreshAdminTabBadges patches the badge spans in place. Unlike
// renderAdminTabRail it never rebuilds the rail, so keyboard focus is untouched.
function refreshAdminTabBadges() {
  if (!adminTabList) return;
  const next = adminTabBadgeCounts();
  if (!adminBadgeCountsChanged(adminAppliedBadgeCounts, next)) return;
  adminAppliedBadgeCounts = next;
  adminTabButtons().forEach((button) => {
    const config = ADMIN_TABS[button.dataset.adminTab];
    if (!config?.badge) return;
    const count = next[config.badge] || 0;
    let badge = button.querySelector(".admin-tab-badge");
    if (count > 0) {
      if (!badge) {
        badge = document.createElement("span");
        badge.className = "admin-tab-badge";
        badge.setAttribute("aria-hidden", "true");
        button.append(badge);
      }
      badge.textContent = count > 99 ? "99+" : String(count);
      button.setAttribute("aria-label", `${config.label}, ${count} need attention`);
    } else {
      badge?.remove();
      button.setAttribute("aria-label", config.label);
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
  adminAppliedBadgeCounts = badges;
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
  if (sub === "stats") {
    return { view: "stats", seriesId: "", filter: "", author: "", tag: "", bookId };
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
    } else if (state.audiobooks.view === "stats") {
      qs.set("sub", "stats");
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

function navigateToView(view) {
	state.filters.tag = "";
	state.filters.view = view || "library";
	loadInitialMemes().catch((error) => console.error(error));
	if (drawerMediaQuery.matches) closeSidebarDrawer();
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

function renderContentMode() {
  const adminMode = isAdminView();
	const homeMode = state.filters.view === "home";
	const audiobooksMode = isAudiobooksView();
	const manageSeriesBooksMode = audiobooksMode && state.audiobooks.view === "manage-books";
	const statsMode = audiobooksMode && state.audiobooks.view === "stats";
	const libraryMode = !adminMode && !homeMode && !audiobooksMode;
	audiobooksView?.classList.toggle("hidden", !audiobooksMode || manageSeriesBooksMode || statsMode);
	audiobookSeriesManageView?.classList.toggle("hidden", !manageSeriesBooksMode);
	audiobookStatsView?.classList.toggle("hidden", !statsMode);
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
  window.renderAdminStorage?.();
  window.renderAdminAudiobookLibrary?.();
  window.renderAdminUserActivity?.();
  window.renderAdminAudiobookSeries?.();
  syncAdminTagQueuePolling();
  syncAdminLinkQueuePolling();
  syncAdminBackupPolling();
  adminRemuxPoller.sync();
}
