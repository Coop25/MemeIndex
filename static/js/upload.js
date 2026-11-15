// Uploading files and importing media links, with their tag editors.

let uploadTagSuggestionAbortController = null;
let linkUploadTagSuggestionAbortController = null;
let uploadPreviewURL = null;

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
