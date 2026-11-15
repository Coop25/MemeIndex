// The meme modal: opening a meme, its preview, editing tags and notes, AI
// tag suggestions, and saving changes.

// Tracks whether opening the meme modal paused an in-progress audiobook, so
// closing it only resumes playback it actually interrupted - not an
// audiobook the listener had already paused themselves before or during.
let audiobookPausedForMemeModal = false;
let adminTagReviewAdvancing = false;
let modalLLMTagSuggestionLoading = false;
let tagSuggestionAbortController = null;
let modalTagSequence = 0;
let memeModalUITimeout = null;

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
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${escapeHTML(meme.contentType)}</span><span>Use Share to create a one-year access link.</span>`;
  return icon;
}

function clearMemeModalUIHideTimer() {
  if (memeModalUITimeout) {
    window.clearTimeout(memeModalUITimeout);
    memeModalUITimeout = null;
  }
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

function getModalMediaControlTarget() {
  return modalPreview.querySelector("video, audio");
}

function syncModalMediaControls() {
  modalMediaControlBar.sync();
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

// buildMemePatch sends only what the user changed since the modal opened:
// tags as a delta against the saved set (so a concurrent edit by someone else
// is merged instead of overwritten), and notes/favorite only when touched.
// Without a snapshot it falls back to sending the full state.
function buildMemePatch(snapshot, current) {
  if (!snapshot) {
    return { favorite: current.favorite, notes: current.notes, tags: current.tags };
  }
  const before = new Set(String(snapshot.tags || "").split(", ").map((value) => normalizeTagValue(value)).filter(Boolean));
  const after = new Set((current.tags || []).map((value) => normalizeTagValue(value)).filter(Boolean));
  const patch = {
    tags_added: [...after].filter((tag) => !before.has(tag)),
    tags_removed: [...before].filter((tag) => !after.has(tag)),
  };
  if (current.notes !== snapshot.notes) patch.notes = current.notes;
  if (current.favorite !== snapshot.favorite) patch.favorite = current.favorite;
  return patch;
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
