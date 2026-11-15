// The random reel: its session, preloading, swiping between memes, and
// auto-hiding controls.

let randomReelSessionID = null;
let randomReelCanGoPrev = false;
let randomReelUITimeout = null;
let randomReelStepLock = false;
let randomReelPreloadCache = new Map();

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
  icon.innerHTML = `<strong>${pickIcon(meme.contentType)}</strong><span>${escapeHTML(meme.originalName)}</span><span>Use Share to create a one-year access link.</span>`;
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
  randomReelMediaControlBar.sync();
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
