// Audiobook series: the series filter, the series manager, and series
// suggestions.

function renderAudiobookSeriesFilter() {
  audiobooksSubnav?.classList.toggle("hidden", !canViewAudiobooks());
  const series = state.audiobooks.series;
  const current = state.audiobooks.seriesId;
  const activeFilter = state.audiobooks.filter;
  const activeAuthor = state.audiobooks.author;
  const activeTag = state.audiobooks.tag;
  const inSeriesGallery = state.audiobooks.view === "series";
  const inStatsView = state.audiobooks.view === "stats";

  audiobooksSubnavFilterItems.forEach((item) => {
    item.classList.toggle("is-active", !inSeriesGallery && !inStatsView && !current && !activeAuthor && !activeTag && item.dataset.audiobookFilter === activeFilter);
  });
  audiobooksSubnavSeriesButton?.classList.toggle("is-active", inSeriesGallery);
  audiobooksSubnavStatsButton?.classList.toggle("is-active", inStatsView);

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

function wireAudiobookSeriesCard(entry) {
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
// grouped by series, then ordered series_sequence, then created_at (the order
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
  stopAudiobookSeriesSuggestScan();
  audiobookSeriesSuggest.open = false;
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
  // Picking a series means looking at it, not at the suggestions; a scan
  // still running carries on and is there when the panel is reopened.
  audiobookSeriesSuggest.open = false;
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
  // The suggestions panel takes over the detail side while it's open.
  const suggesting = audiobookSeriesSuggest.open;
  audiobookSeriesSuggestions?.classList.toggle("hidden", !suggesting);
  audiobookSeriesManagerEmpty?.classList.toggle("hidden", !!series || suggesting);
  audiobookSeriesManagerSelected?.classList.toggle("hidden", !series || suggesting);
  if (suggesting) {
    renderAudiobookSeriesSuggestions();
    return;
  }
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

let audiobookSeriesAddListKey = "";

function renderAudiobookSeriesAddList() {
  if (!audiobookSeriesAddList) return;
  const seriesId = state.audiobooks.manageSeriesId;
  const candidates = seriesId ? audiobookSeriesAddCandidates(seriesId, audiobookSeriesAddQuery) : [];
  const listKey = JSON.stringify([seriesId, audiobookSeriesAddQuery.trim()]);
  const keepShown = listKey === audiobookSeriesAddListKey ? audiobookSeriesAddShown : 0;
  audiobookSeriesAddListKey = listKey;
  audiobookSeriesAddRows = candidates;
  audiobookSeriesAddShown = 0;
  if (!candidates.length) {
    audiobookSeriesAddList.innerHTML = `<p class="status-text audiobook-series-add-list-empty">${
      audiobookSeriesAddQuery.trim() ? "No matching books." : "Every book is already in this series."
    }</p>`;
    return;
  }
  audiobookSeriesAddList.innerHTML = "";
  appendAudiobookSeriesAddRows(Math.max(keepShown, AUDIOBOOK_RENDER_CHUNK));
}

function audiobookSeriesAddRowHTML(book) {
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
}

function appendAudiobookSeriesAddRows(count) {
  const seriesId = state.audiobooks.manageSeriesId;
  const next = audiobookSeriesAddRows.slice(audiobookSeriesAddShown, audiobookSeriesAddShown + count);
  if (!next.length) return;
  audiobookSeriesAddList.insertAdjacentHTML("beforeend", next.map(audiobookSeriesAddRowHTML).join(""));
  audiobookSeriesAddShown += next.length;
  audiobookSeriesAddList.querySelectorAll("[data-add-book]:not([data-wired])").forEach((button) => {
    button.dataset.wired = "";
    button.addEventListener("click", () => {
      const bookId = button.closest("[data-book-id]")?.dataset.bookId;
      if (bookId && seriesId) addAudiobookToSeries(seriesId, bookId).catch((error) => console.error(error));
    });
  });
}

// ---------------------------------------------------------------------------
// Series suggestions: the server looks each book up by title and author in
// the Audible catalog (audiobook_series_lookup.go) and groups the library by
// series, with each book's number. A scan goes a batch per request, and every
// response covers everything checked so far, so it simply replaces the last
// one. Nothing changes until a suggestion is applied, through the same series
// endpoints the rest of the manager uses.
// ---------------------------------------------------------------------------

const audiobookSeriesSuggest = {
  open: false,
  scanning: false,
  scanToken: 0,
  result: null,
  error: "",
  // What the user changed on a card, by suggestion key, so it survives the
  // re-render each scan batch brings: { name, skipped: Set, sequences: {} }.
  edits: {},
  applying: new Set(),
  dismissed: new Set(),
};

function openAudiobookSeriesSuggestions() {
  audiobookSeriesSuggest.open = true;
  renderAudiobookSeriesManagerView();
  if (!audiobookSeriesSuggest.result && !audiobookSeriesSuggest.scanning) {
    scanAudiobookSeriesSuggestions().catch((error) => console.error(error));
  }
}

function closeAudiobookSeriesSuggestions() {
  stopAudiobookSeriesSuggestScan();
  audiobookSeriesSuggest.open = false;
  renderAudiobookSeriesManagerView();
}

function stopAudiobookSeriesSuggestScan() {
  audiobookSeriesSuggest.scanToken++;
  audiobookSeriesSuggest.scanning = false;
}

async function scanAudiobookSeriesSuggestions() {
  const token = ++audiobookSeriesSuggest.scanToken;
  audiobookSeriesSuggest.scanning = true;
  audiobookSeriesSuggest.error = "";
  // A fresh scan starts from clean cards: what was unticked or renamed last
  // time belonged to suggestions that may have changed since.
  audiobookSeriesSuggest.dismissed.clear();
  audiobookSeriesSuggest.edits = {};
  if (audiobookSeriesSuggestList) audiobookSeriesSuggestList.innerHTML = "";
  renderAudiobookSeriesSuggestions();
  const query = audiobookSeriesSuggestAllInput?.checked ? "?scope=all" : "";
  let lastChecked = -1;
  try {
    while (token === audiobookSeriesSuggest.scanToken) {
      const response = await fetch(`/api/audiobook-series/suggestions${query}`, { credentials: "same-origin" });
      if (token !== audiobookSeriesSuggest.scanToken) return;
      if (response.status === 401 || response.status === 403) {
        await expectAuthorized(response, "");
        return;
      }
      if (!response.ok) {
        audiobookSeriesSuggest.error = response.status === 429
          ? "Too many lookups at once - wait a moment, then scan again."
          : "Could not look up series right now.";
        break;
      }
      const payload = await response.json();
      if (token !== audiobookSeriesSuggest.scanToken) return;
      audiobookSeriesSuggest.result = payload;
      renderAudiobookSeriesSuggestions();
      // Keep going while books are left and the last batch got somewhere - a
      // batch that checked nothing new means the catalog isn't answering.
      if (!payload.remaining || payload.checked <= lastChecked) break;
      lastChecked = payload.checked;
    }
  } catch (error) {
    console.error(error);
    if (token === audiobookSeriesSuggest.scanToken) audiobookSeriesSuggest.error = "Could not look up series right now.";
  } finally {
    if (token === audiobookSeriesSuggest.scanToken) {
      audiobookSeriesSuggest.scanning = false;
      renderAudiobookSeriesSuggestions();
    }
  }
}

function audiobookSeriesSuggestStatusText() {
  const { scanning, result, error } = audiobookSeriesSuggest;
  if (!result) return scanning ? "Looking up your books..." : error;
  const { checked = 0, remaining = 0, failed = 0, not_found: notFound = 0 } = result;
  if (scanning) return `Looking up your books... ${checked} checked, ${remaining} to go.`;
  const parts = [`Checked ${checked} book${checked === 1 ? "" : "s"}.`];
  if (notFound) parts.push(`${notFound} had no series in the catalog (standalones, or not listed there).`);
  if (failed) parts.push(`${failed} couldn't be looked up right now.`);
  if (remaining) parts.push(`${remaining} not checked yet - scan again to continue.`);
  if (error) parts.push(error);
  return parts.join(" ");
}

function visibleAudiobookSeriesSuggestions() {
  return (audiobookSeriesSuggest.result?.suggestions || []).filter((s) => !audiobookSeriesSuggest.dismissed.has(s.key));
}

function audiobookSeriesSuggestionEdit(key) {
  audiobookSeriesSuggest.edits[key] ??= { name: null, skipped: new Set(), sequences: {} };
  return audiobookSeriesSuggest.edits[key];
}

function formatAudiobookSeriesNumber(value) {
  return value === null || value === undefined ? "" : String(value);
}

function audiobookSeriesSuggestionHTML(suggestion) {
  const edit = audiobookSeriesSuggestionEdit(suggestion.key);
  // Apply adds to existing_series_id whenever the server sent one, so the
  // card must say so even if the local series list hasn't caught up with it.
  const existing = suggestion.existing_series_id
    ? state.audiobooks.series.find((s) => s.id === suggestion.existing_series_id) || { name: suggestion.name }
    : null;
  const applying = audiobookSeriesSuggest.applying.has(suggestion.key);
  const rows = suggestion.books.map((book) => {
    const included = !edit.skipped.has(book.id);
    const number = edit.sequences[book.id] ?? formatAudiobookSeriesNumber(book.sequence);
    const cover = book.cover_url
      ? `<img class="audiobook-series-add-row-cover" src="${escapeHTML(book.cover_url)}" alt="" loading="lazy" />`
      : `<div class="audiobook-series-add-row-cover audiobook-series-add-row-cover-empty" aria-hidden="true"></div>`;
    const current = book.current_series_id && book.current_series_id !== suggestion.existing_series_id
      ? `<span class="audiobook-manage-item-badge">Now in ${escapeHTML(book.current_series_name || "another series")}${book.current_sequence !== null && book.current_sequence !== undefined ? ` #${escapeHTML(formatAudiobookSeriesNumber(book.current_sequence))}` : ""}</span>`
      : "";
    return `
      <div class="audiobook-series-add-row audiobook-series-suggestion-book${included ? "" : " is-skipped"}" data-book-id="${escapeHTML(book.id)}">
        <input type="checkbox" data-suggestion-include ${included ? "checked" : ""} aria-label="Include ${escapeHTML(audiobookDisplayTitle(book))}" />
        ${cover}
        <div class="audiobook-series-add-row-info">
          <span class="audiobook-series-add-row-title">${escapeHTML(audiobookDisplayTitle(book))}</span>
          ${book.author ? `<span class="audiobook-series-add-row-author">${escapeHTML(book.author)}</span>` : ""}
          ${current}
        </div>
        <input type="number" step="any" min="0" class="audiobook-series-sequence-input" data-suggestion-sequence value="${escapeHTML(number)}" placeholder="#" title="Number in the series - left blank, it goes after the last one" aria-label="Number in series" />
      </div>
    `;
  }).join("");
  const name = edit.name ?? suggestion.name;
  return `
    <article class="audiobook-series-suggestion" data-suggestion-key="${escapeHTML(suggestion.key)}">
      <div class="audiobook-series-suggestion-header">
        <input type="text" class="audiobook-series-suggestion-name" data-suggestion-name maxlength="200" value="${escapeHTML(existing ? existing.name : name)}" ${existing ? "disabled" : ""} aria-label="Series name" />
        <span class="audiobook-manage-item-badge">${existing ? "Your existing series" : "New series"}</span>
      </div>
      <div class="audiobook-series-suggestion-books">${rows}</div>
      <div class="audiobook-series-suggestion-actions">
        <button type="button" class="primary-button" data-suggestion-apply ${applying ? "disabled" : ""}>${applying ? "Applying..." : existing ? "Add to series" : "Create series"}</button>
        <button type="button" class="ghost-button" data-suggestion-dismiss ${applying ? "disabled" : ""}>Dismiss</button>
      </div>
    </article>
  `;
}

// Cards are kept by key across re-renders and only redrawn when their content
// changed, so a scan batch landing doesn't throw away focus or a half-typed
// number on the card being edited. The signature leaves the user's edits out
// on purpose: the inputs already show those, and they're kept in .edits for
// whenever the card does get redrawn.
const audiobookSeriesSuggestCardSignatures = new WeakMap();

function audiobookSeriesSuggestionSignature(suggestion) {
  const existingName = state.audiobooks.series.find((s) => s.id === suggestion.existing_series_id)?.name || "";
  return JSON.stringify([suggestion, existingName, audiobookSeriesSuggest.applying.has(suggestion.key)]);
}

function renderAudiobookSeriesSuggestions() {
  if (!audiobookSeriesSuggestList) return;
  const { scanning } = audiobookSeriesSuggest;
  audiobookSeriesSuggestRunButton?.classList.toggle("hidden", scanning);
  audiobookSeriesSuggestStopButton?.classList.toggle("hidden", !scanning);
  if (audiobookSeriesSuggestRunButton) {
    audiobookSeriesSuggestRunButton.textContent = audiobookSeriesSuggest.result?.remaining ? "Continue scan" : audiobookSeriesSuggest.result ? "Scan again" : "Scan library";
  }
  if (audiobookSeriesSuggestStatus) audiobookSeriesSuggestStatus.textContent = audiobookSeriesSuggestStatusText();

  const suggestions = visibleAudiobookSeriesSuggestions();
  const existingCards = new Map();
  audiobookSeriesSuggestList.querySelectorAll("[data-suggestion-key]").forEach((card) => existingCards.set(card.dataset.suggestionKey, card));
  audiobookSeriesSuggestList.querySelector("[data-suggestion-empty]")?.remove();

  let previous = null;
  for (const suggestion of suggestions) {
    const signature = audiobookSeriesSuggestionSignature(suggestion);
    let card = existingCards.get(suggestion.key);
    existingCards.delete(suggestion.key);
    if (!card || audiobookSeriesSuggestCardSignatures.get(card) !== signature) {
      const template = document.createElement("template");
      template.innerHTML = audiobookSeriesSuggestionHTML(suggestion).trim();
      const fresh = template.content.firstElementChild;
      audiobookSeriesSuggestCardSignatures.set(fresh, signature);
      wireAudiobookSeriesSuggestionCard(fresh, suggestion);
      if (card) card.replaceWith(fresh);
      card = fresh;
    }
    const expectedNext = previous ? previous.nextElementSibling : audiobookSeriesSuggestList.firstElementChild;
    if (card !== expectedNext) {
      audiobookSeriesSuggestList.insertBefore(card, expectedNext);
    }
    previous = card;
  }
  existingCards.forEach((card) => card.remove());

  if (!suggestions.length && audiobookSeriesSuggest.result && !scanning) {
    audiobookSeriesSuggestList.insertAdjacentHTML("beforeend", `<p class="status-text audiobook-series-add-list-empty" data-suggestion-empty>No series to suggest${audiobookSeriesSuggestAllInput?.checked ? "" : " for books that aren't in one yet"}.</p>`);
  }
}

function wireAudiobookSeriesSuggestionCard(card, suggestion) {
  const edit = audiobookSeriesSuggestionEdit(suggestion.key);
  card.querySelector("[data-suggestion-name]")?.addEventListener("input", (event) => {
    edit.name = event.target.value;
  });
  card.querySelectorAll("[data-suggestion-include]").forEach((input) => {
    input.addEventListener("change", () => {
      const row = input.closest("[data-book-id]");
      const bookId = row?.dataset.bookId;
      if (!bookId) return;
      if (input.checked) edit.skipped.delete(bookId); else edit.skipped.add(bookId);
      row.classList.toggle("is-skipped", !input.checked);
    });
  });
  card.querySelectorAll("[data-suggestion-sequence]").forEach((input) => {
    input.addEventListener("input", () => {
      const bookId = input.closest("[data-book-id]")?.dataset.bookId;
      if (bookId) edit.sequences[bookId] = input.value;
    });
  });
  card.querySelector("[data-suggestion-apply]")?.addEventListener("click", () => {
    applyAudiobookSeriesSuggestion(suggestion.key).catch((error) => console.error(error));
  });
  card.querySelector("[data-suggestion-dismiss]")?.addEventListener("click", () => {
    audiobookSeriesSuggest.dismissed.add(suggestion.key);
    delete audiobookSeriesSuggest.edits[suggestion.key];
    renderAudiobookSeriesSuggestions();
  });
}

// findOrCreateAudiobookSeriesByName reuses a series of the same name (the
// server treats names case-insensitively) rather than failing on a duplicate.
async function findOrCreateAudiobookSeriesByName(name) {
  const same = (series) => series.name.trim().toLowerCase() === name.trim().toLowerCase();
  const found = state.audiobooks.series.find(same);
  if (found) return found.id;
  const response = await fetch("/api/audiobook-series", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  });
  if (response.status === 409) {
    await fetchAudiobookSeries();
    const again = state.audiobooks.series.find(same);
    if (again) return again.id;
  }
  if (!(await expectAuthorized(response, "Could not create series."))) {
    throw new Error("create audiobook series failed");
  }
  return (await response.json()).id;
}

async function applyAudiobookSeriesSuggestion(key) {
  const suggestion = visibleAudiobookSeriesSuggestions().find((s) => s.key === key);
  if (!suggestion || audiobookSeriesSuggest.applying.has(key)) return;
  const edit = audiobookSeriesSuggestionEdit(key);
  const books = suggestion.books.filter((book) => !edit.skipped.has(book.id));
  if (!books.length) {
    showToast("Tick at least one book to add.", "error", { title: "Audiobook Series" });
    return;
  }
  const name = (edit.name ?? suggestion.name).trim();
  if (!suggestion.existing_series_id && !name) {
    showToast("Give the series a name first.", "error", { title: "Audiobook Series" });
    return;
  }

  audiobookSeriesSuggest.applying.add(key);
  renderAudiobookSeriesSuggestions();
  try {
    const seriesId = suggestion.existing_series_id || await findOrCreateAudiobookSeriesByName(name);
    const numbers = books.map((book) => parseFloat(edit.sequences[book.id] ?? formatAudiobookSeriesNumber(book.sequence)));
    // A book with no number goes after everything else in the series.
    let next = [
      ...seriesManagerBooksFor(seriesId).map((book) => Number(book.series_sequence) || 0),
      ...numbers.filter(Number.isFinite),
    ].reduce((max, value) => Math.max(max, value), 0) + 1;
    for (let i = 0; i < books.length; i++) {
      const sequence = Number.isFinite(numbers[i]) ? numbers[i] : next++;
      await setAudiobookSeriesRaw(books[i].id, seriesId, sequence);
    }
    audiobookSeriesSuggest.dismissed.add(key);
    delete audiobookSeriesSuggest.edits[key];
    const seriesName = state.audiobooks.series.find((s) => s.id === seriesId)?.name || name;
    showToast(`Added ${books.length} book${books.length === 1 ? "" : "s"} to "${seriesName}".`, "success", { title: "Audiobook Series" });
  } catch (error) {
    console.error(error);
    showToast("Could not apply that suggestion - some books may already have been added.", "error", { title: "Audiobook Series" });
  } finally {
    audiobookSeriesSuggest.applying.delete(key);
    await refreshAudiobookSeriesManagePageData();
    await fetchAudiobooks();
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
