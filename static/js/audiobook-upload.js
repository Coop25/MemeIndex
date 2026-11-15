// Uploading audiobooks in resumable chunks.

const AUDIOBOOK_CHUNK_SIZE = 8 * 1024 * 1024; // 8 MiB per chunk request.

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

const AUDIOBOOK_UPLOAD_STORAGE_KEY = "memevault.audiobookUpload";

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
