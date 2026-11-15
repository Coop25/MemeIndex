// The audiobook library: listing, filters, cards, stats, favorites, and
// editing an audiobook's details.

// The books grid is fetched a page at a time; scrolling near the end asks for
// the next one (fetchAudiobooks({ append: true })). The server's page cap
// matches maxAudiobookPageSize in audiobook_handlers.go.
const AUDIOBOOK_PAGE_SIZE = 48;
const AUDIOBOOK_MAX_PAGE_SIZE = 1000;
// Bumped by every fetch so a response for a list the user has already moved
// away from (a slow page of "Favorites" landing after they picked a tag)
// is dropped rather than overwriting the newer one.
let audiobooksFetchSeq = 0;
// Which list state.audiobooks.items currently holds, so a refresh knows
// whether it's reloading the same list (keep what's scrolled in) or a new one.
let audiobooksLoadedListKey = "";

function audiobooksListKey() {
  const { filter, seriesId, author, tag } = state.audiobooks;
  return JSON.stringify([filter, seriesId, author, tag, state.filters.query]);
}

// fetchAudiobooks loads the first page of the current list, or with append
// the next page after what's already loaded. A plain refresh of the list
// already on screen (after an edit, upload, or delete) re-requests as many
// books as were scrolled in, so it doesn't drop you back to page one.
// Resolves true once the books are in state.audiobooks.items.
async function fetchAudiobooks({ append = false } = {}) {
  const listKey = audiobooksListKey();
  if (append && (listKey !== audiobooksLoadedListKey || state.audiobooks.loading || !state.audiobooks.hasMore)) {
    return false;
  }
  const seq = ++audiobooksFetchSeq;
  const loadedCount = listKey === audiobooksLoadedListKey ? state.audiobooks.items.length : 0;
  // Without IntersectionObserver nothing would ever ask for page two, so
  // fall back to the whole list in one go.
  const limit = !audiobookIncrementalRender
    ? 0
    : append
      ? AUDIOBOOK_PAGE_SIZE
      : Math.min(Math.max(loadedCount, AUDIOBOOK_PAGE_SIZE), AUDIOBOOK_MAX_PAGE_SIZE);
  state.audiobooks.loading = true;
  state.audiobooks.appending = append;
  if (append) {
    syncAudiobooksGridSentinel();
  } else if (audiobooksStatus) {
    audiobooksStatus.textContent = "Loading audiobooks...";
  }
  let loaded = false;
  try {
    const params = new URLSearchParams();
    if (state.audiobooks.filter) params.set("filter", state.audiobooks.filter);
    if (state.audiobooks.seriesId) params.set("series_id", state.audiobooks.seriesId);
    if (state.audiobooks.author) params.set("author", state.audiobooks.author);
    if (state.audiobooks.tag) params.set("tag", state.audiobooks.tag);
    if (state.filters.query) params.set("q", state.filters.query);
    if (limit > 0) params.set("limit", `${limit}`);
    if (append) params.set("offset", `${state.audiobooks.items.length}`);
    const query = params.toString();
    const response = await fetch(`/api/audiobooks${query ? `?${query}` : ""}`, { credentials: "same-origin" });
    if (!(await expectAuthorized(response, "Could not load audiobooks."))) {
      return false;
    }
    const payload = await response.json();
    if (seq !== audiobooksFetchSeq) return false;
    const page = payload.audiobooks || [];
    if (append) {
      // Offsets shift if a book is added between page requests, which can
      // repeat one that's already loaded - skip it rather than show it twice.
      const seen = new Set(state.audiobooks.items.map((item) => item.id));
      state.audiobooks.items.push(...page.filter((item) => !seen.has(item.id)));
    } else {
      state.audiobooks.items = page;
    }
    state.audiobooks.hasMore = !!payload.has_more;
    audiobooksLoadedListKey = listKey;
    if (audiobooksStatus) audiobooksStatus.textContent = "";
    loaded = true;
  } catch (error) {
    console.error(error);
    if (seq === audiobooksFetchSeq && !append && audiobooksStatus) {
      audiobooksStatus.textContent = "Could not load audiobooks.";
    }
  } finally {
    if (seq === audiobooksFetchSeq) {
      state.audiobooks.loading = false;
      state.audiobooks.appending = false;
      if (!append) {
        renderAudiobooksGrid();
      } else if (state.audiobooks.view !== "series") {
        appendAudiobooksGridEntries(Infinity);
        if (!loaded && audiobooksGridSentinel) {
          audiobooksGridSentinel.textContent = "Could not load more audiobooks. Scroll away and back to retry.";
        }
      }
    }
  }
  return loaded;
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

async function fetchAudiobookStatsOverview() {
  try {
    const response = await fetch("/api/audiobooks/stats/overview", { credentials: "same-origin" });
    if (!(await expectAuthorized(response, "Could not load listening stats."))) {
      return;
    }
    state.audiobooks.stats = await response.json();
    audiobookStatsFinishedLimit = AUDIOBOOK_RENDER_CHUNK;
  } catch (error) {
    console.error(error);
    state.audiobooks.stats = null;
  } finally {
    renderAudiobookStatsView();
  }
}

async function toggleAudiobookStatsHistory(bookId) {
  if (state.audiobooks.statsHistoryOpenID === bookId) {
    state.audiobooks.statsHistoryOpenID = "";
    renderAudiobookStatsView();
    return;
  }
  state.audiobooks.statsHistoryOpenID = bookId;
  if (!state.audiobooks.statsHistoryByID[bookId]) {
    state.audiobooks.statsHistoryFailedIDs.delete(bookId);
    try {
      const response = await fetch(`/api/audiobooks/${encodeURIComponent(bookId)}/listening-history`, { credentials: "same-origin" });
      if (response.ok) {
        const payload = await response.json();
        state.audiobooks.statsHistoryByID[bookId] = payload.history || [];
      } else {
        state.audiobooks.statsHistoryFailedIDs.add(bookId);
      }
    } catch (error) {
      console.error(error);
      state.audiobooks.statsHistoryFailedIDs.add(bookId);
    }
  }
  renderAudiobookStatsView();
}

// createAudiobookScrollLoader returns { recheck } (or null without
// IntersectionObserver, in which case callers just render everything).
// An observer only reports *changes* in intersection, so when a chunk is
// appended and the sentinel is still in range (a tall window, a short list)
// nothing would fire again - recheck re-observes to force a fresh report.
function createAudiobookScrollLoader(sentinel, loadMore) {
  if (!sentinel || !audiobookIncrementalRender) return null;
  const observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting)) loadMore();
  }, { root: null, rootMargin: "0px 0px 600px 0px", threshold: 0 });
  observer.observe(sentinel);
  return {
    recheck() {
      observer.unobserve(sentinel);
      observer.observe(sentinel);
    },
  };
}

let audiobookStatsFinishedLoader = null;

function loadMoreAudiobookStatsFinished() {
  const finished = state.audiobooks.stats?.finished_books || [];
  if (audiobookStatsFinishedLimit >= finished.length) return;
  audiobookStatsFinishedLimit += AUDIOBOOK_RENDER_CHUNK;
  renderAudiobookStatsView();
  audiobookStatsFinishedLoader?.recheck();
}

// renderAudiobookStatsView draws the whole Stats page from state.audiobooks.stats:
// the total-time/streak tiles, a lightweight CSS bar chart of the last 30
// days, and the finished-books history (each row expandable in place to that
// book's own day-by-day listening log, fetched on first expand).
function renderAudiobookStatsView() {
  if (!audiobookStatsView) return;
  const overview = state.audiobooks.stats;
  if (audiobookStatsTotalTime) {
    audiobookStatsTotalTime.textContent = formatAudiobookStatsDuration(overview?.total_seconds_listened);
  }
  if (audiobookStatsStreak) {
    const days = Number(overview?.streak_days || 0);
    audiobookStatsStreak.textContent = `${days} day${days === 1 ? "" : "s"}`;
  }

  if (audiobookStatsChart) {
    const totals = new Map((overview?.daily_totals || []).map((day) => [day.date, Number(day.seconds_listened) || 0]));
    const maxSeconds = Math.max(1, ...totals.values());
    const days = [];
    const cursor = new Date();
    cursor.setUTCHours(0, 0, 0, 0);
    for (let i = 29; i >= 0; i--) {
      const date = new Date(cursor);
      date.setUTCDate(date.getUTCDate() - i);
      const key = date.toISOString().slice(0, 10);
      days.push({ key, seconds: totals.get(key) || 0 });
    }
    audiobookStatsChart.innerHTML = days.map((day) => {
      const heightPct = Math.max(2, Math.round((day.seconds / maxSeconds) * 100));
      const title = `${day.key}: ${formatAudiobookStatsDuration(day.seconds)}`;
      return `<div class="audiobook-stats-chart-bar" style="height:${heightPct}%" title="${escapeHTML(title)}"></div>`;
    }).join("");
  }

  const finished = overview?.finished_books || [];
  audiobookStatsFinishedEmpty?.classList.toggle("hidden", finished.length !== 0);
  if (audiobookStatsFinishedList) {
    audiobookStatsFinishedLoader ??= createAudiobookScrollLoader(audiobookStatsFinishedSentinel, loadMoreAudiobookStatsFinished);
    const shown = audiobookStatsFinishedLoader ? finished.slice(0, audiobookStatsFinishedLimit) : finished;
    audiobookStatsFinishedSentinel?.classList.toggle("hidden", shown.length >= finished.length);
    const openID = state.audiobooks.statsHistoryOpenID;
    audiobookStatsFinishedList.innerHTML = shown.map((book) => {
      const isOpen = book.id === openID;
      const history = state.audiobooks.statsHistoryByID[book.id];
      const failed = state.audiobooks.statsHistoryFailedIDs.has(book.id);
      const historyHTML = !isOpen
        ? ""
        : failed
          ? `<p class="status-text">Could not load listening history. Collapse and reopen to try again.</p>`
          : !history
          ? `<p class="status-text">Loading...</p>`
          : history.length === 0
            ? `<p class="status-text">No recorded listening sessions.</p>`
            : `<ul class="audiobook-stats-history-list">${history.map((day) =>
                `<li><span>${escapeHTML(day.date)}</span><span>${formatAudiobookStatsDuration(day.seconds_listened)}</span></li>`
              ).join("")}</ul>`;
      return `
        <article class="audiobook-stats-finished-row" data-finished-book-id="${escapeHTML(book.id)}">
          <button type="button" class="audiobook-stats-finished-summary" data-finished-toggle>
            ${book.cover_url ? `<img class="audiobook-stats-finished-cover" src="${escapeHTML(book.cover_url)}" alt="" loading="lazy" />` : `<div class="audiobook-stats-finished-cover audiobook-stats-finished-cover-empty" aria-hidden="true"></div>`}
            <span class="audiobook-stats-finished-info">
              <strong>${escapeHTML(book.title)}</strong>
              <span class="status-text">${escapeHTML(book.author || "")}</span>
            </span>
            <span class="status-text" title="${escapeHTML(formatDateTimeZoned(book.finished_at))}">${escapeHTML(formatRelativeTime(book.finished_at))}</span>
          </button>
          <div class="audiobook-stats-finished-history${isOpen ? "" : " hidden"}">${historyHTML}</div>
        </article>
      `;
    }).join("");

    audiobookStatsFinishedList.querySelectorAll("[data-finished-toggle]").forEach((button) => {
      const bookId = button.closest("[data-finished-book-id]")?.dataset.finishedBookId;
      button.addEventListener("click", () => {
        if (bookId) toggleAudiobookStatsHistory(bookId).catch((error) => console.error(error));
      });
    });
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

function audiobookDisplayTitle(item) {
  return item.title || item.filename || "Untitled";
}

function audiobookFileExtension(filename) {
  const match = /\.[^./\\]+$/.exec(filename || "");
  return match ? match[0] : "";
}

// formatAudiobookRemaining renders a seconds count as "Xh Ym<suffix>" (or
// zeroText for anything under a minute). The default suffix/zeroText match
// its original "time left on this book" use; the Stats view reuses it with
// both blanked out for a plain duration like "2h 15m".
function formatAudiobookRemaining(seconds, { suffix = " left", zeroText = "Less than a minute left" } = {}) {
  const total = Math.max(0, Math.round(seconds || 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours > 0) return `${hours}h ${minutes}m${suffix}`;
  if (minutes > 0) return `${minutes}m${suffix}`;
  return zeroText;
}

// formatAudiobookStatsDuration is formatAudiobookRemaining with the "time
// left" framing stripped out, for a plain total like "2h 15m" or "0m".
function formatAudiobookStatsDuration(totalSeconds) {
  return formatAudiobookRemaining(totalSeconds, { suffix: "", zeroText: "0m" });
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

let audiobooksGridShown = 0;
let audiobooksGridLoader = null;

function audiobooksGridEntries() {
  return state.audiobooks.view === "series" ? state.audiobooks.series : state.audiobooks.items;
}

function renderAudiobooksGrid() {
  if (!audiobooksGrid) return;
  audiobooksGridLoader ??= createAudiobookScrollLoader(audiobooksGridSentinel, loadMoreAudiobooksGrid);
  const seriesMode = state.audiobooks.view === "series";
  if (!seriesMode) audiobooksGridLimit = AUDIOBOOK_RENDER_CHUNK;
  const entries = audiobooksGridEntries();
  audiobooksGrid.classList.toggle("audiobooks-grid-series", seriesMode);
  audiobooksGrid.innerHTML = "";
  audiobooksGridShown = 0;
  appendAudiobooksGridEntries(seriesMode && audiobooksGridLoader ? audiobooksGridLimit : Infinity);
  audiobooksEmpty?.classList.toggle("hidden", entries.length !== 0 || state.audiobooks.loading);
}

// appendAudiobooksGridEntries adds entries [shown, upTo) to the end of the
// grid without touching the cards already there, so a new chunk or page
// doesn't reload covers or drop focus.
function appendAudiobooksGridEntries(upTo) {
  const seriesMode = state.audiobooks.view === "series";
  const entries = audiobooksGridEntries();
  const next = entries.slice(audiobooksGridShown, upTo);
  audiobooksGrid.insertAdjacentHTML("beforeend", next.map(seriesMode ? audiobookSeriesCardHTML : audiobookCardHTML).join(""));
  if (seriesMode) {
    next.forEach(wireAudiobookSeriesCard);
  } else {
    wireAudiobookCards(next);
  }
  audiobooksGridShown += next.length;
  syncAudiobooksGridSentinel();
}

function syncAudiobooksGridSentinel() {
  if (!audiobooksGridSentinel) return;
  const seriesMode = state.audiobooks.view === "series";
  const more = seriesMode
    ? audiobooksGridShown < state.audiobooks.series.length
    : state.audiobooks.hasMore || state.audiobooks.appending;
  const appending = !seriesMode && state.audiobooks.appending;
  audiobooksGridSentinel.classList.toggle("hidden", !more);
  audiobooksGridSentinel.classList.toggle("is-loading", appending);
  audiobooksGridSentinel.textContent = appending ? "Loading more audiobooks..." : "";
}

function loadMoreAudiobooksGrid() {
  if (!audiobooksGrid) return;
  if (state.audiobooks.view !== "series") {
    // Only re-check after a page actually landed - re-checking after a
    // failure would retry in a tight loop while the sentinel sits in view.
    fetchAudiobooks({ append: true }).then((loaded) => {
      if (loaded) audiobooksGridLoader?.recheck();
    });
    return;
  }
  if (audiobooksGridShown >= state.audiobooks.series.length) return;
  audiobooksGridLimit = audiobooksGridShown + AUDIOBOOK_RENDER_CHUNK;
  appendAudiobooksGridEntries(audiobooksGridLimit);
  audiobooksGridLoader?.recheck();
}

function wireAudiobookCards(items) {
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
let audiobookEditTagSequence = 0;
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
  let savedAny = false;
  try {
    const metadataResponse = await fetch(`/api/audiobooks/${encodeURIComponent(item.id)}`, {
      method: "PATCH",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ title, author, narrator }),
    });
    if (!(await expectAuthorized(metadataResponse, "Could not update audiobook."))) return;
    savedAny = true;

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
    // Metadata/cover may have changed even if a later step failed; keep the admin library table in step.
    if (savedAny) window.refreshAdminAudiobookLibrary?.();
  }
}

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
