// Admin tag hygiene, and the background queues: tag suggestions, link
// downloads, and audiobook remuxing.

const ADMIN_TAG_HYGIENE_DISMISSED_KEY = "memeindex.adminTagHygieneDismissed";
const ADMIN_TAG_HYGIENE_PAGE_SIZE = 50;
// Timing for re-polling the tag-hygiene report after a change: the server
// recomputes it on a debounced background worker, so the first fetch right after
// a merge can still be a step behind.
const ADMIN_TAG_HYGIENE_RETRY_MS = 1500;
const ADMIN_TAG_HYGIENE_MAX_RETRIES = 20;
const ADMIN_TAG_HYGIENE_SETTLE_MS = [2500, 7000];
let adminTagHygieneFetchToken = 0;

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

function renderAdminTagQueueStatus() {
  if (!adminTagQueuePanel || !adminTagQueueGrid || !adminTagQueueSummary || !adminTagQueueList) {
    return;
  }

  const tab = activeAdminTab();
  const isQueueTab = tab === "tag-queue";
  const isReviewTab = tab === "tag-review";
  const visible = isAdminView() && canManageUsers() && (isQueueTab || isReviewTab);
  adminTagQueuePanel.classList.toggle("hidden", !visible);
  adminTagQueueRetryFailed?.classList.toggle("hidden", !isQueueTab);
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
  const mergedMemes = Number(payload.affected_memes || 0);
  const mergedAudiobooks = Number(payload.audiobooks_affected || 0);
  const movedRestrictions = Number(payload.restrictions_moved || 0);
  let mergeMessage = `Merged ${payload.source_tag || normalizedSource} into ${payload.target_tag || normalizedTarget} across ${mergedMemes} meme${mergedMemes === 1 ? "" : "s"}`;
  if (mergedAudiobooks > 0) {
    mergeMessage += ` and ${mergedAudiobooks} audiobook${mergedAudiobooks === 1 ? "" : "s"}`;
  }
  if (movedRestrictions > 0) {
    mergeMessage += ` (${movedRestrictions} account restriction${movedRestrictions === 1 ? "" : "s"} moved)`;
  }
  showToast(`${mergeMessage}.`, "success", { title: "Tag Hygiene", duration: 3400 });
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
  state.admin.tagReview.loaded = true;
  syncAdminWorkQueue(state.admin, "suggested_tags", reviewTotal);
  refreshAdminTabBadges();
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
  if (payload) {
    syncAdminWorkQueue(state.admin, "link_retries", Array.isArray(payload.queued) ? payload.queued.length : 0);
    syncAdminWorkQueue(state.admin, "rejected_links", Array.isArray(payload.rejected) ? payload.rejected.length : 0);
  }
  refreshAdminTabBadges();
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
