// The audiobook player: playback, stream recovery, playback on other
// devices, Media Session, chapters, bookmarks, the sleep timer, the mini
// player, and saving progress.

const AUDIOBOOK_PROGRESS_SAVE_INTERVAL_MS = 15000;
// How often this device reports its playback state while actually playing,
// and how often it polls for another device doing the same. The heartbeat is
// also how a playing device hears it was asked to stop, so it's what bounds
// how long two devices overlap after "play here" - kept short for that, not a
// bandwidth concern at this scale.
const AUDIOBOOK_PLAYBACK_HEARTBEAT_INTERVAL_MS = 2000;
const AUDIOBOOK_PLAYBACK_ELSEWHERE_POLL_INTERVAL_MS = 5000;
// After asking another device to stop, how long to keep checking that it
// actually did before telling the listener it didn't respond.
const AUDIOBOOK_TAKEOVER_CONFIRM_TIMEOUT_MS = 10000;
const AUDIOBOOK_TAKEOVER_CONFIRM_POLL_MS = 1000;
// The server stops counting a device as playing after 20s without a
// heartbeat, so a reported position is never older than that.
const AUDIOBOOK_PLAYBACK_MAX_AGE_SECONDS = 20;
const AUDIOBOOK_ELSEWHERE_STATUS = "Playing on another device — tap play to pause it there and play here.";

// Progress save, heartbeat, and "playing elsewhere" poll are all driven off
// one shared Worker tick (see getAudiobookSyncWorker) rather than their own
// window.setInterval, so they stay on schedule even while this tab is
// backgrounded. audiobookSyncActive is just "is a player open right now" -
// the worker itself runs for the page's whole lifetime once created, same as
// audiobookAudioEl, since a tick with nothing open to act on is a no-op.
let audiobookSyncWorker = null;
let audiobookSyncTickCount = 0;
let audiobookSyncActive = false;
const AUDIOBOOK_SYNC_TICK_MS = 1000;

function getAudiobookSyncWorker() {
  if (audiobookSyncWorker) return audiobookSyncWorker;
  const worker = new Worker("/static/audiobook-sync-worker.js");
  worker.onmessage = () => {
    audiobookSyncTickCount++;
    if (!audiobookSyncActive || !state.audiobooks.player) return;
    if (audiobookSyncTickCount % (AUDIOBOOK_PLAYBACK_ELSEWHERE_POLL_INTERVAL_MS / AUDIOBOOK_SYNC_TICK_MS) === 0) {
      pollAudiobookPlaybackElsewhere();
    }
    tickAudiobookStreamRecovery();
    const audio = audiobookAudioEl;
    if (!audio || audio.paused) return;
    if (audiobookSyncTickCount % (AUDIOBOOK_PLAYBACK_HEARTBEAT_INTERVAL_MS / AUDIOBOOK_SYNC_TICK_MS) === 0) {
      sendAudiobookPlaybackHeartbeat();
    }
    if (audiobookSyncTickCount % (AUDIOBOOK_PROGRESS_SAVE_INTERVAL_MS / AUDIOBOOK_SYNC_TICK_MS) === 0) {
      saveAudiobookProgressNow();
    }
  };
  audiobookSyncWorker = worker;
  return worker;
}

let audiobookVolumeAutoHideTimer = null;
const AUDIOBOOK_VOLUME_AUTO_HIDE_MS = 30000;

// The volume popover works the same way as the progress toggle above - tucked
// behind a button until tapped, and folding itself away after
// AUDIOBOOK_VOLUME_AUTO_HIDE_MS of no interaction.
function setAudiobookVolumeExpanded(expanded) {
  audiobookPlayerVolumeWrap?.classList.toggle("is-expanded", expanded);
  audiobookPlayerVolumeToggle?.setAttribute("aria-expanded", String(expanded));
  window.clearTimeout(audiobookVolumeAutoHideTimer);
  audiobookVolumeAutoHideTimer = null;
  if (expanded) {
    audiobookVolumeAutoHideTimer = window.setTimeout(() => setAudiobookVolumeExpanded(false), AUDIOBOOK_VOLUME_AUTO_HIDE_MS);
  }
}

// scheduleAudiobookVolumeAutoHide restarts the auto-hide countdown without
// forcing the popover open - used while the slider is actively being dragged.
function scheduleAudiobookVolumeAutoHide() {
  if (!audiobookPlayerVolumeWrap?.classList.contains("is-expanded")) return;
  window.clearTimeout(audiobookVolumeAutoHideTimer);
  audiobookVolumeAutoHideTimer = window.setTimeout(() => setAudiobookVolumeExpanded(false), AUDIOBOOK_VOLUME_AUTO_HIDE_MS);
}

// renderAudiobookPlaybackElsewhere shows or hides the "playing on another
// device" banner. info is {positionSeconds, ageSeconds, fetchedAt} from
// fetchAudiobookPlaybackStatus, or null once nothing is playing elsewhere
// (either we never found one, or the listener just took over).
function renderAudiobookPlaybackElsewhere(info) {
  audiobookPlaybackElsewhereState = info;
  if (!info) clearAudiobookPlayerStatus(AUDIOBOOK_ELSEWHERE_STATUS);
  renderAudiobookMiniPlayerInfo();
  if (!audiobookPlayerElsewhereBanner) return;
  if (!info) {
    audiobookPlayerElsewhereBanner.classList.add("hidden");
    return;
  }
  if (audiobookPlayerElsewhereText) {
    audiobookPlayerElsewhereText.textContent = `Playing on another device — ${formatMediaTime(estimateAudiobookElsewherePosition(info))}`;
  }
  audiobookPlayerElsewhereBanner.classList.remove("hidden");
}

// estimateAudiobookElsewherePosition works out where the other device has
// gotten to by now, not where it was at its last heartbeat: that report is
// already ageSeconds old when the server hands it over, plus however long ago
// this browser received it. Assumes 1x speed - the server doesn't track rate.
function estimateAudiobookElsewherePosition(info) {
  const sinceFetch = (performance.now() - info.fetchedAt) / 1000;
  const age = Math.min(info.ageSeconds + sinceFetch, AUDIOBOOK_PLAYBACK_MAX_AGE_SECONDS);
  return info.positionSeconds + Math.max(age, 0);
}

// fetchAudiobookPlaybackStatus returns the raw "other devices" picture for an
// audiobook: elsewhere is {positionSeconds, ageSeconds, fetchedAt} for another
// device still playing it (or null), and stopPending says a stop request this
// device sent hasn't been picked up yet. null on a failed request.
async function fetchAudiobookPlaybackStatus(audiobookId, { timeoutMs = 0 } = {}) {
  const payload = await fetchAudiobookJSON(
    `/api/audiobooks/${encodeURIComponent(audiobookId)}/playback-elsewhere?deviceId=${encodeURIComponent(getAudiobookDeviceId())}`,
    timeoutMs,
  );
  if (!payload) return null;
  return {
    elsewhere: payload.active
      ? { positionSeconds: payload.position_seconds || 0, ageSeconds: payload.age_seconds || 0, fetchedAt: performance.now() }
      : null,
    stopPending: !!payload.stop_pending,
  };
}

async function fetchAudiobookPlaybackElsewhere(audiobookId, options) {
  const status = await fetchAudiobookPlaybackStatus(audiobookId, options);
  return status?.elsewhere || null;
}

// fetchAudiobookSavedPosition reads the server's latest saved position for
// a book, or null if it can't be fetched (the caller falls back to the list's
// copy).
async function fetchAudiobookSavedPosition(audiobookId, { timeoutMs = 0 } = {}) {
  const payload = await fetchAudiobookJSON(`/api/audiobooks/${encodeURIComponent(audiobookId)}/progress`, timeoutMs);
  if (!payload) return null;
  return payload.found ? Number(payload.position_seconds) || 0 : null;
}

// fetchAudiobookJSON GETs a small player endpoint and returns its JSON, or
// null on any failure. With a timeout, a slow or dead connection gives up
// (as null) instead of holding up whatever is waiting on it - pressing play,
// or opening a book, shouldn't hang on a bad signal just to double-check
// something the player mostly already knows. The timer covers reading the
// body too, not just the headers.
async function fetchAudiobookJSON(url, timeoutMs = 0) {
  const controller = timeoutMs > 0 ? new AbortController() : null;
  const timer = controller ? setTimeout(() => controller.abort(), timeoutMs) : 0;
  try {
    const response = await fetch(url, { credentials: "same-origin", signal: controller?.signal });
    if (!response.ok) return null;
    return await response.json();
  } catch (error) {
    if (error?.name !== "AbortError") console.error(error);
    return null;
  } finally {
    clearTimeout(timer);
  }
}

// pollAudiobookPlaybackElsewhere re-checks for another active device while
// the player is open, so the banner can appear (or disappear) mid-session,
// not just at open time.
async function pollAudiobookPlaybackElsewhere() {
  const player = state.audiobooks.player;
  if (!player) return;
  // While a takeover is still being confirmed, the device being stopped can
  // report itself as playing one last time before it hears the request -
  // showing the banner for that would just flash it back up for a moment.
  if (audiobookTakeoverConfirming) return;
  const info = await fetchAudiobookPlaybackElsewhere(player.id);
  if (state.audiobooks.player?.id !== player.id || audiobookTakeoverConfirming) return;
  renderAudiobookPlaybackElsewhere(info);
}

// The takeover still waiting on its pause request, if any. Another press
// meanwhile (a lock-screen or headset play, or the button again after
// cancelling) joins it rather than sending a second request and running a
// second seek-and-play on top of the first.
let audiobookTakeover = null;

// pauseAudiobookElsewhereAndPlayHere asks every other device playing this
// book to stop, then takes over playback here from wherever that device had
// gotten to. Used both by the "playing elsewhere" banner's explicit button
// and by the main play button, which does the same thing implicitly - this
// is the one place that actually requests the other device stop, since just
// calling audio.play() locally (e.g. the plain play() in the button handler
// below) only starts a second simultaneous playback.
async function pauseAudiobookElsewhereAndPlayHere() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  const elsewhere = audiobookPlaybackElsewhereState;
  if (!player || !audio || !elsewhere) return;
  const seekToElsewherePosition = () => {
    const targetPosition = estimateAudiobookElsewherePosition(elsewhere);
    const knownDuration = Number.isFinite(audio.duration) && audio.duration > 0 ? audio.duration : player.durationSeconds || 0;
    if (Number.isFinite(targetPosition) && targetPosition > 0 && (!knownDuration || targetPosition < knownDuration - 1)) {
      audio.currentTime = targetPosition;
    }
  };
  // Same constraint as resumeAudiobookPlaybackFromSavedPosition: an element
  // that has never played can only be started from inside the press, so it
  // starts now rather than after the other device has been asked to stop.
  const startNow = !audiobookPlaybackUnlocked && !audio.error && !audiobookStreamRecovery;
  const begin = (takeover) => {
    if (startNow) {
      takeover.startedEarly = true;
      seekToElsewherePosition();
      startAudiobookPlayback(audio);
    } else {
      setAudiobookPlayPending("takeover");
    }
  };
  if (audiobookTakeover?.playerId === player.id) {
    begin(audiobookTakeover);
    return;
  }
  const takeover = { playerId: player.id, startedEarly: false };
  audiobookTakeover = takeover;
  begin(takeover);
  // The other device is still playing, so this one shouldn't be either.
  const abandonTakeover = () => {
    setAudiobookPlayPending(null);
    if (takeover.startedEarly) audio.pause();
  };
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/playback-elsewhere/pause`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ deviceId: getAudiobookDeviceId() }),
    });
    if (!(await expectAuthorized(response, "Could not pause the other device."))) {
      abandonTakeover();
      return;
    }
  } catch (error) {
    console.error(error);
    abandonTakeover();
    showToast("Could not pause the other device.", "error", { title: "Audiobooks" });
    return;
  } finally {
    if (audiobookTakeover === takeover) audiobookTakeover = null;
  }
  // The player may have closed or moved to another book meanwhile - its
  // banner isn't this one's to clear.
  if (state.audiobooks.player?.id !== player.id) return;
  renderAudiobookPlaybackElsewhere(null);
  // Still waiting to play (not cancelled, and not already started early).
  if (audiobookPlayPending === "takeover") {
    seekToElsewherePosition();
    startAudiobookPlayback(audio);
  }
  confirmAudiobookTakeover(player.id).catch((error) => console.error(error));
}

// confirmAudiobookTakeover waits for the other device to actually pick up the
// stop request (the server reports it pending until that device's next
// heartbeat) instead of announcing success the moment the request is sent.
// A device that never answers - a phone whose browser froze the tab in the
// background, say - gets an honest "may still be playing" instead.
let audiobookTakeoverConfirmToken = 0;
let audiobookTakeoverConfirming = false;
const AUDIOBOOK_TAKEOVER_STATUS = "Pausing the other device...";

async function confirmAudiobookTakeover(audiobookId) {
  const token = ++audiobookTakeoverConfirmToken;
  const isCurrent = () => token === audiobookTakeoverConfirmToken && state.audiobooks.player?.id === audiobookId;
  audiobookTakeoverConfirming = true;
  setAudiobookPlayerStatus(AUDIOBOOK_TAKEOVER_STATUS);
  try {
    const deadline = performance.now() + AUDIOBOOK_TAKEOVER_CONFIRM_TIMEOUT_MS;
    while (performance.now() < deadline) {
      await new Promise((resolve) => window.setTimeout(resolve, AUDIOBOOK_TAKEOVER_CONFIRM_POLL_MS));
      if (!isCurrent()) return;
      const status = await fetchAudiobookPlaybackStatus(audiobookId);
      if (!isCurrent()) return;
      if (status && !status.stopPending) {
        showToast("Paused on the other device.", "success", { title: "Audiobooks" });
        return;
      }
    }
    showToast("The other device didn't respond — it may still be playing.", "info", { title: "Audiobooks" });
  } finally {
    if (token === audiobookTakeoverConfirmToken) {
      audiobookTakeoverConfirming = false;
      clearAudiobookPlayerStatus(AUDIOBOOK_TAKEOVER_STATUS);
    }
  }
}

// Set once this device pauses itself because another device took over, and
// held until the listener plays or seeks here again. While set, progress saves
// from this device are skipped - its position is a few seconds ahead of where
// the other one resumed, and the pause event, closing the player, or the tab
// going to the background would otherwise save it over the newer position
// the other device is now writing.
let audiobookYieldedToOtherDevice = false;
let audiobookLastHeartbeatAt = 0;

// sendAudiobookPlaybackHeartbeat reports this device's actual play state and
// position, and self-pauses if another device just forced this one to stop
// (the "stopRequested" flag the server clears as it reports it). resumed is
// set for the heartbeat sent the moment the listener presses play here, which
// tells the server to drop any older stop request instead of acting on it.
async function sendAudiobookPlaybackHeartbeat({ resumed = false } = {}) {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio || !isAudiobookAudioForPlayer(audio, player)) return;
  audiobookLastHeartbeatAt = performance.now();
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/playback-heartbeat`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ deviceId: getAudiobookDeviceId(), positionSeconds: audio.currentTime || 0, isPlaying: !audio.paused, resumed }),
      keepalive: true,
    });
    if (!response.ok) return;
    const payload = await response.json();
    if (payload.stopRequested && state.audiobooks.player?.id === player.id && !audio.paused) {
      audiobookYieldedToOtherDevice = true;
      audio.pause();
      setAudiobookPlayerStatus("Paused — now playing on another device.");
      showToast("Paused here — resumed on another device.", "info", { title: "Audiobooks" });
      pollAudiobookPlaybackElsewhere();
    }
  } catch (error) {
    console.error(error);
  }
}

// sendAudiobookPlaybackStoppedSignal tells the server this device stopped
// regardless of the audio element's actual paused state at the moment - used
// on page unload, where the tab is closing out from under an still-playing
// <audio> and there's no "pause" event to piggyback a heartbeat on.
function sendAudiobookPlaybackStoppedSignal() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/playback-heartbeat`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ deviceId: getAudiobookDeviceId(), positionSeconds: audio.currentTime || 0, isPlaying: false }),
    keepalive: true,
  }).catch((error) => console.error(error));
}

function getAudiobookAudioElement() {
  if (audiobookAudioEl) return audiobookAudioEl;
  const audio = new Audio();
  audio.addEventListener("timeupdate", () => {
    updateAudiobookPlayerProgressUI();
    updateAudiobookBufferingUI();
    // Backstop for the worker-driven heartbeat: some mobile browsers suspend
    // workers in a backgrounded tab while still delivering timeupdate to the
    // page as long as audio is playing. Without this a locked phone can keep
    // playing but go quiet to the server, so another device neither sees it
    // as playing nor can tell it to stop.
    if (!audio.paused && performance.now() - audiobookLastHeartbeatAt > AUDIOBOOK_PLAYBACK_HEARTBEAT_INTERVAL_MS * 1.5) {
      sendAudiobookPlaybackHeartbeat();
    }
  });
  audio.addEventListener("loadedmetadata", updateAudiobookPlayerProgressUI);
  // progress fires as more of the file downloads - only the buffered band on
  // the seek bar depends on it.
  audio.addEventListener("progress", updateAudiobookPlayerProgressUI);
  // Buffering state is derived from the element itself (see
  // updateAudiobookBufferingUI), so every event that can change readyState,
  // paused, or seeking just re-derives it rather than each one guessing.
  for (const type of ["waiting", "playing", "canplay", "canplaythrough", "seeking", "seeked", "stalled", "loadstart", "emptied"]) {
    audio.addEventListener(type, updateAudiobookBufferingUI);
  }
  // Any sign of life keeps the stall check quiet - progress in particular, so
  // a slow connection that's still delivering isn't mistaken for a dead one.
  for (const type of ["play", "playing", "timeupdate", "progress", "seeked", "loadstart", "canplay"]) {
    audio.addEventListener(type, noteAudiobookStreamActivity);
  }
  audio.addEventListener("playing", () => {
    audiobookPlaybackUnlocked = true;
    if (audiobookStreamRecovery) finishAudiobookStreamRecovery();
  });
  // durationchange covers files whose real length isn't known yet at
  // loadedmetadata (e.g. an M4B with its moov atom at the end of the file,
  // needing an extra range request the browser hasn't made yet) - once it
  // resolves, the progress bar and time labels (which depend on duration)
  // need to be recomputed instead of staying wrong for the rest of playback.
  audio.addEventListener("durationchange", updateAudiobookPlayerProgressUI);
  audio.addEventListener("ratechange", () => setAudiobookMediaSessionPositionState(audio));
  audio.addEventListener("play", () => {
    setAudiobookPlayIcon(true);
    // Save the position the instant playback starts, not just on the 15s
    // timer - this is what clears a "Finished"/"not started" card badge as
    // soon as the listener presses play again, instead of leaving the old
    // marked-as state showing until the next periodic save.
    audiobookYieldedToOtherDevice = false;
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat({ resumed: true });
    setAudiobookMediaSessionPlaybackState(audio);
    clearAudiobookPlayerStatus("Paused — now playing on another device.");
    clearAudiobookPlayerStatus(AUDIOBOOK_TAP_TO_PLAY_STATUS);
    clearAudiobookPlayerStatus(AUDIOBOOK_TAP_TO_KEEP_LISTENING_STATUS);
    updateAudiobookBufferingUI();
  });
  audio.addEventListener("pause", () => {
    setAudiobookPlayIcon(false);
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat();
    setAudiobookMediaSessionPlaybackState(audio);
    updateAudiobookBufferingUI();
  });
  audio.addEventListener("ended", () => {
    setAudiobookPlayIcon(false);
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat();
    setAudiobookMediaSessionPlaybackState(audio);
  });
  // seeked fires for every way currentTime can change - the scrub bar,
  // chapter/bookmark jumps, the +/-30s buttons - so hooking it here instead
  // of each seek source is what makes a seek immediately persist and show up
  // on another open instance, rather than waiting on the next 15s save or 5s
  // heartbeat (or being lost entirely if the tab closes before either fires).
  audio.addEventListener("seeked", () => {
    // Moving the playhead here means the listener is back on this device, so
    // its position is worth saving again.
    audiobookYieldedToOtherDevice = false;
    saveAudiobookProgressNow();
    sendAudiobookPlaybackHeartbeat();
    setAudiobookMediaSessionPositionState(audio);
  });
  audio.addEventListener("error", () => {
    const wasPlaying = !audio.paused;
    setAudiobookPlayIcon(false);
    updateAudiobookBufferingUI();
    // An emptied src (closing the player) also raises "error" on some
    // browsers - only a real load failure is worth telling anyone about.
    if (!audio.getAttribute("src")) return;
    handleAudiobookStreamError(audio, wasPlaying);
  });
  setupAudiobookMediaSessionHandlers(audio);
  audiobookAudioEl = audio;
  return audio;
}

// The server can replace a book's file mid-listen - an MP3 is swapped for its
// lossless .m4b remux at the same URL - after which the element's next range
// request is answered from a different, differently sized file and fails.
// Reloading the source, back at the same position, recovers without the
// listener noticing more than a brief stall.
//
// A phone left playing overnight hits the same failure for a duller reason:
// the connection drops (Wi-Fi power saving, a proxy closing a long-lived
// request) and the first reload lands while the network is still gone. So the
// reload keeps trying with a growing delay until the window runs out. The
// browser doesn't always report this as an error either - it can sit in
// "waiting" forever - so a stall with no data arriving counts as a failure too.
// Everything runs off the shared worker tick (see getAudiobookSyncWorker)
// because window timers are throttled hard once a locked phone goes quiet.
const AUDIOBOOK_STREAM_RETRY_DELAYS_MS = [2000, 5000, 10000, 15000, 30000];
const AUDIOBOOK_STREAM_RETRY_WINDOW_MS = 15 * 60 * 1000;
const AUDIOBOOK_STREAM_ATTEMPT_TIMEOUT_MS = 30000;
const AUDIOBOOK_STREAM_STALL_MS = 20000;
const AUDIOBOOK_RECONNECTING_STATUS = "Connection lost - reconnecting...";
const AUDIOBOOK_PLAYBACK_ERROR_STATUS = "Playback error - check your connection, then press play to retry.";
const AUDIOBOOK_TAP_TO_KEEP_LISTENING_STATUS = "Tap play to keep listening.";
let audiobookStreamActivityAt = 0;
// Where playback was when recovery gave up: a failed load resets currentTime,
// so pressing play the next morning has nothing else to go on if the saved
// position can't be fetched either.
let audiobookStreamFailedAt = null;

function noteAudiobookStreamActivity() {
  audiobookStreamActivityAt = Date.now();
}

function cancelAudiobookStreamRecovery() {
  audiobookStreamRecovery?.reload?.abort();
  audiobookStreamRecovery = null;
  clearAudiobookPlayerStatus(AUDIOBOOK_RECONNECTING_STATUS);
}

function resetAudiobookStreamRecovery() {
  cancelAudiobookStreamRecovery();
  audiobookStreamFailedAt = null;
}

function finishAudiobookStreamRecovery() {
  audiobookStreamFailedAt = null;
  cancelAudiobookStreamRecovery();
}

function beginAudiobookStreamRecovery(audio, wasPlaying, resumeAt = audio.currentTime) {
  const player = state.audiobooks.player;
  if (!player || !isAudiobookAudioForPlayer(audio, player)) return false;
  audiobookStreamRecovery = {
    playerId: player.id,
    resumeAt,
    wasPlaying,
    startedAt: Date.now(),
    attempt: 0,
    nextAttemptAt: Date.now(),
    reload: null,
  };
  setAudiobookPlayerStatus(AUDIOBOOK_RECONNECTING_STATUS);
  tickAudiobookStreamRecovery();
  return true;
}

function handleAudiobookStreamError(audio, wasPlaying) {
  const recovery = audiobookStreamRecovery;
  if (recovery) {
    const delays = AUDIOBOOK_STREAM_RETRY_DELAYS_MS;
    recovery.nextAttemptAt = Date.now() + delays[Math.min(recovery.attempt - 1, delays.length - 1)];
    return;
  }
  if (!beginAudiobookStreamRecovery(audio, wasPlaying)) setAudiobookPlayerStatus(AUDIOBOOK_PLAYBACK_ERROR_STATUS);
}

// retryAudiobookStreamNow is for a good moment to try right away: the listener
// pressed play, or the network or the page just came back.
function retryAudiobookStreamNow(audio, resumeAt = audiobookStreamFailedAt ?? audio.currentTime) {
  const recovery = audiobookStreamRecovery;
  if (!recovery) {
    beginAudiobookStreamRecovery(audio, true, resumeAt);
    return;
  }
  recovery.wasPlaying = true;
  recovery.nextAttemptAt = Date.now();
  tickAudiobookStreamRecovery();
}

function isAudiobookStreamStalled(audio) {
  return !audio.paused && !audio.ended && !audio.error && !!audio.getAttribute("src")
    && audio.readyState < HTMLMediaElement.HAVE_FUTURE_DATA
    && Date.now() - audiobookStreamActivityAt >= AUDIOBOOK_STREAM_STALL_MS;
}

function tickAudiobookStreamRecovery() {
  const audio = audiobookAudioEl;
  if (!audio) return;
  const recovery = audiobookStreamRecovery;
  if (!recovery) {
    if (isAudiobookStreamStalled(audio)) beginAudiobookStreamRecovery(audio, true);
    return;
  }
  const player = state.audiobooks.player;
  // Picking the book up on another device mid-outage means this one should
  // stay quiet, not start playing over it once the network returns.
  if (player?.id !== recovery.playerId || !isAudiobookAudioForPlayer(audio, player) || audiobookPlaybackElsewhereState) {
    cancelAudiobookStreamRecovery();
    return;
  }
  if (Date.now() < recovery.nextAttemptAt) return;
  if (Date.now() - recovery.startedAt >= AUDIOBOOK_STREAM_RETRY_WINDOW_MS) {
    audiobookStreamFailedAt = recovery.resumeAt;
    cancelAudiobookStreamRecovery();
    setAudiobookPlayerStatus(AUDIOBOOK_PLAYBACK_ERROR_STATUS);
    return;
  }
  reloadAudiobookStream(audio, player, recovery);
}

function reloadAudiobookStream(audio, player, recovery) {
  recovery.attempt += 1;
  // Replaced with the backoff delay if this load fails with an error; left
  // alone, it catches a load that never answers at all.
  recovery.nextAttemptAt = Date.now() + AUDIOBOOK_STREAM_ATTEMPT_TIMEOUT_MS;
  // Only the newest reload counts: a listener left behind by an earlier one
  // could otherwise fire on a later load (reopening the book), seeking and
  // autoplaying ahead of the open flow's own resume/other-device checks.
  recovery.reload?.abort();
  const reload = new AbortController();
  recovery.reload = reload;
  audio.addEventListener("loadedmetadata", () => {
    reload.abort();
    audio.currentTime = recovery.resumeAt;
    if (!recovery.wasPlaying) {
      finishAudiobookStreamRecovery();
      return;
    }
    // Recovery ends on "playing"; a failure here comes back through "error"
    // or the attempt timeout, except a browser refusing to autoplay, which
    // retrying can't fix.
    audio.play().catch((error) => {
      if (error?.name !== "NotAllowedError" || audiobookStreamRecovery !== recovery) return;
      finishAudiobookStreamRecovery();
      setAudiobookPlayerStatus(AUDIOBOOK_TAP_TO_KEEP_LISTENING_STATUS);
    });
  }, { signal: reload.signal });
  audio.src = player.streamUrl;
  setAudiobookPlayIcon(false);
  updateAudiobookBufferingUI();
}

// --- Media Session (lock screen / notification drawer controls) -----------
//
// Without this, a backgrounded tab's notification shows only a generic
// "playing audio" card with just a mute/stop action - Android and iOS both
// drive their now-playing UI off the Media Session API rather than the
// <audio> element itself, so title/author/cover and play/pause/seek/chapter
// controls have to be set up explicitly.

function setAudiobookMediaSessionPlaybackState(audio) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.playbackState = audio.paused ? "paused" : "playing";
}

function setAudiobookMediaSessionPositionState(audio) {
  if (!("mediaSession" in navigator) || !navigator.mediaSession.setPositionState) return;
  const duration = Number.isFinite(audio.duration) ? audio.duration : 0;
  if (!(duration > 0)) return;
  try {
    navigator.mediaSession.setPositionState({
      duration,
      playbackRate: audio.playbackRate || 1,
      position: Math.min(Math.max(audio.currentTime || 0, 0), duration),
    });
  } catch (error) {
    console.error(error);
  }
}

// setAudiobookMediaSessionMetadata fills in the lock-screen/notification
// now-playing card (title, author, cover) so background playback shows real
// book details instead of a generic tab name. Artwork is listed at several
// sizes pointing at the same cover image - the OS picks whichever it needs,
// and we don't know the source image's real dimensions.
function setAudiobookMediaSessionMetadata(item) {
  if (!("mediaSession" in navigator) || typeof MediaMetadata === "undefined") return;
  navigator.mediaSession.metadata = new MediaMetadata({
    title: audiobookDisplayTitle(item),
    artist: item.author || "",
    album: "Audiobook",
    artwork: item.cover_url
      ? [96, 128, 192, 256, 384, 512].map((size) => ({ src: item.cover_url, sizes: `${size}x${size}` }))
      : [],
  });
}

// setupAudiobookMediaSessionHandlers wires the OS-level notification/lock-
// screen controls to the same audio element and helpers the in-page player
// buttons use, so background playback stays controllable from the
// notification drawer. Called once, when the shared <audio> element is
// created.
function setupAudiobookMediaSessionHandlers(audio) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.setActionHandler("play", () => {
    if (audiobookPlaybackElsewhereState) {
      pauseAudiobookElsewhereAndPlayHere().catch((error) => console.error(error));
    } else {
      resumeAudiobookPlaybackFromSavedPosition().catch((error) => console.error(error));
    }
  });
  navigator.mediaSession.setActionHandler("pause", () => {
    cancelAudiobookPlayRequest();
    audio.pause();
  });
  navigator.mediaSession.setActionHandler("stop", () => {
    cancelAudiobookPlayRequest();
    audio.pause();
  });
  navigator.mediaSession.setActionHandler("seekbackward", (details) => {
    audio.currentTime = Math.max(0, audio.currentTime - (details?.seekOffset || 30));
  });
  navigator.mediaSession.setActionHandler("seekforward", (details) => {
    audio.currentTime = Math.min(audio.duration || audio.currentTime, audio.currentTime + (details?.seekOffset || 30));
  });
  try {
    navigator.mediaSession.setActionHandler("seekto", (details) => {
      if (!details || !Number.isFinite(details.seekTime)) return;
      if (details.fastSeek && "fastSeek" in audio) {
        audio.fastSeek(details.seekTime);
      } else {
        audio.currentTime = details.seekTime;
      }
    });
  } catch (error) {
    // Some browsers (Safari) don't support the "seekto" action - safe to skip.
  }
  updateAudiobookMediaSessionChapterHandlers(false);
}

// updateAudiobookMediaSessionChapterHandlers enables/disables the
// previous/next-track notification buttons for chapter navigation - left
// unset (rather than wired to a no-op) when the open book has no chapters, so
// the OS doesn't show controls that do nothing.
function updateAudiobookMediaSessionChapterHandlers(hasChapters) {
  if (!("mediaSession" in navigator)) return;
  navigator.mediaSession.setActionHandler("previoustrack", hasChapters ? () => jumpAudiobookChapter(-1) : null);
  navigator.mediaSession.setActionHandler("nexttrack", hasChapters ? () => jumpAudiobookChapter(1) : null);
}

const AUDIOBOOK_PLAY_ICON_SVG = '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M8 5.5v13a1 1 0 0 0 1.5.86l10.5-6.5a1 1 0 0 0 0-1.72L9.5 4.64A1 1 0 0 0 8 5.5Z" /></svg>';
const AUDIOBOOK_PAUSE_ICON_SVG = '<svg viewBox="0 0 24 24" aria-hidden="true"><rect x="6" y="5" width="4" height="14" rx="1.2" /><rect x="14" y="5" width="4" height="14" rx="1.2" /></svg>';

function setAudiobookPlayIcon(playing) {
  if (audiobookPlayerPlayIcon) audiobookPlayerPlayIcon.innerHTML = playing ? AUDIOBOOK_PAUSE_ICON_SVG : AUDIOBOOK_PLAY_ICON_SVG;
  audiobookPlayerPlayButton?.setAttribute("aria-label", playing ? "Pause" : "Play");
}

// The player's one-line status (top bar of the full player, subtitle of the
// mini bar). setAudiobookPlayerStatus replaces it; clearAudiobookPlayerStatus
// only clears it if it still says `text`, so a transient message ("Pausing
// the other device...") finishing doesn't wipe out a newer one that replaced
// it in the meantime.
function setAudiobookPlayerStatus(text) {
  if (audiobookPlayerStatus) audiobookPlayerStatus.textContent = text;
  renderAudiobookMiniPlayerInfo();
}

function clearAudiobookPlayerStatus(text) {
  if (audiobookPlayerStatus?.textContent === text) setAudiobookPlayerStatus("");
}

const AUDIOBOOK_BUFFERING_STATUS = "Buffering...";
const AUDIOBOOK_TAP_TO_PLAY_STATUS = "Tap play to start listening.";
let audiobookBuffering = false;

// Set once this element has actually played. Until then a phone's browser
// only lets play() start from inside the tap itself, not after an awaited
// fetch, and refuses it outright otherwise.
let audiobookPlaybackUnlocked = false;

function setAudiobookPlayPending(reason) {
  if (audiobookPlayPending === reason) return;
  audiobookPlayPending = reason;
  updateAudiobookBufferingUI();
}

// startAudiobookPlayback is every play() the player makes on the listener's
// behalf: it hands the busy state over to the element (now unpaused, so the
// readyState check takes over) and makes a refusal visible instead of
// leaving the button looking like it ignored the press.
function startAudiobookPlayback(audio) {
  const attempt = audio.play();
  audiobookPlayPending = null;
  updateAudiobookBufferingUI();
  return attempt.catch((error) => {
    updateAudiobookBufferingUI();
    // AbortError: a pause or a new source got there first, which is fine.
    // Load failures come back through the "error" event and its recovery.
    if (error?.name === "AbortError") return;
    console.error(error);
    if (error?.name === "NotAllowedError") setAudiobookPlayerStatus(AUDIOBOOK_TAP_TO_PLAY_STATUS);
  });
}

// cancelAudiobookPlayRequest drops a press of play still waiting on its
// position fetch - pressing the (spinning) button again, or pausing from the
// lock screen, means "never mind".
function cancelAudiobookPlayRequest() {
  audiobookResumeRequestToken++;
  setAudiobookPlayPending(null);
}

// updateAudiobookBufferingUI derives "is this stuck waiting on data" from the
// audio element's own state - it's meant to be playing (not paused/ended) but
// doesn't have enough data to move forward, or is mid-seek - rather than
// tracking waiting/playing events, which miss cases like a seek while paused
// that then never fires "playing" to clear it. Shows as a spinning ring on
// the play button, which is the same node in both the full and mini player.
function updateAudiobookBufferingUI() {
  const audio = audiobookAudioEl;
  const buffering = !!audio && !!audio.getAttribute("src") && !audio.error && (!!audiobookPlayPending
    || (!audio.paused && !audio.ended && (audio.readyState < HTMLMediaElement.HAVE_FUTURE_DATA || audio.seeking)));
  if (buffering === audiobookBuffering) return;
  audiobookBuffering = buffering;
  audiobookPlayerPlayButton?.classList.toggle("is-buffering", buffering);
  audiobookPlayerPlayButton?.setAttribute("aria-busy", String(buffering));
  if (buffering) {
    if (!audiobookPlayerStatus?.textContent) setAudiobookPlayerStatus(AUDIOBOOK_BUFFERING_STATUS);
    else renderAudiobookMiniPlayerInfo();
  } else {
    clearAudiobookPlayerStatus(AUDIOBOOK_BUFFERING_STATUS);
    renderAudiobookMiniPlayerInfo();
  }
}

// renderAudiobookMiniPlayerInfo fills the mini bar's title line and the
// subtitle under it, which carries whatever is most worth knowing while the
// full player is hidden: another device playing, buffering, a status
// message, or else just the author.
function renderAudiobookMiniPlayerInfo() {
  if (!audiobookMiniPlayerSubtitle) return;
  const item = audiobookPlayerOpenItem;
  let subtitle = audiobookActiveChapterLabel || item?.author || "";
  let tone = "";
  const statusText = audiobookPlayerStatus?.textContent || "";
  if (audiobookPlaybackElsewhereState) {
    subtitle = "Playing on another device — press play to take over";
    tone = "warning";
  } else if (audiobookBuffering) {
    subtitle = AUDIOBOOK_BUFFERING_STATUS;
    tone = "busy";
  } else if (statusText) {
    subtitle = statusText;
  }
  audiobookMiniPlayerSubtitle.textContent = subtitle;
  audiobookMiniPlayerSubtitle.dataset.tone = tone;
}

function renderAudiobookMiniPlayerItem(item) {
  if (audiobookMiniPlayerTitle) audiobookMiniPlayerTitle.textContent = audiobookDisplayTitle(item);
  if (audiobookMiniPlayerCover) {
    if (item.cover_url) {
      audiobookMiniPlayerCover.src = item.cover_url;
      audiobookMiniPlayerCover.classList.remove("hidden");
      audiobookMiniPlayerCoverFallback?.classList.add("hidden");
    } else {
      audiobookMiniPlayerCover.removeAttribute("src");
      audiobookMiniPlayerCover.classList.add("hidden");
      audiobookMiniPlayerCoverFallback?.classList.remove("hidden");
    }
  }
  renderAudiobookMiniPlayerInfo();
}

// setAudiobookMiniPlayerOpen shows/hides the mini bar and keeps a body class
// in sync so other mobile chrome (the raised "+" nav button, which would
// otherwise poke up through the bar) can shrink out of its way via CSS.
function setAudiobookMiniPlayerOpen(open) {
  audiobookMiniPlayer?.classList.toggle("is-open", open);
  document.body.classList.toggle("audiobook-mini-player-open", open);
}

// setAudiobookMiniPlayerCollapsed toggles between the full mini bar (progress
// + controls visible) and just its handle peeking above the nav - unlike the
// bar's own close button, playback, the audio element, and every background
// timer are left completely alone either way. The handle is the one thing
// that's always on screen while a book is docked, so there's always
// something to tap to bring the rest back.
function setAudiobookMiniPlayerCollapsed(collapsed) {
  audiobookMiniPlayer?.classList.toggle("is-collapsed", collapsed);
  audiobookMiniPlayerHandle?.setAttribute("aria-label", collapsed ? "Show player" : "Collapse player");
  audiobookMiniPlayerHandle?.setAttribute("aria-expanded", String(!collapsed));
}

// restoreAudiobookBottombarNodes moves the seek slider and transport controls
// back into the full player's bottombar (ahead of the speed/timer/bookmark
// row they started before) and hides the mini player. Safe to call whether or
// not they are currently out in the mini player.
function restoreAudiobookBottombarNodes() {
  if (audiobookPlayerBottombar && audiobookPlayerBottombarRow) {
    if (audiobookPlayerProgressWrap) audiobookPlayerBottombar.insertBefore(audiobookPlayerProgressWrap, audiobookPlayerBottombarRow);
    if (audiobookPlayerControls) audiobookPlayerBottombar.insertBefore(audiobookPlayerControls, audiobookPlayerBottombarRow);
  }
  setAudiobookMiniPlayerOpen(false);
  setAudiobookMiniPlayerCollapsed(false);
}

// minimizeAudiobookPlayer hides the full player dialog but keeps the audio
// element, its state, and all the background timers (heartbeat, progress
// save, elsewhere-poll) running exactly as they were - only the seek slider
// and transport controls move, into the mini bar docked at the bottom of the
// screen, so playback is uninterrupted.
function minimizeAudiobookPlayer() {
  if (!state.audiobooks.player) return;
  setAudiobookVolumeExpanded(false);
  if (audiobookMiniPlayerProgressSlot && audiobookPlayerProgressWrap) audiobookMiniPlayerProgressSlot.appendChild(audiobookPlayerProgressWrap);
  if (audiobookMiniPlayerControlsSlot && audiobookPlayerControls) audiobookMiniPlayerControlsSlot.appendChild(audiobookPlayerControls);
  setAudiobookMiniPlayerCollapsed(false);
  setAudiobookMiniPlayerOpen(true);
  audiobookPlayerModal?.close();
}

// expandAudiobookPlayer reopens the full player from the mini bar, picking up
// the same in-progress playback rather than reloading the audiobook.
function expandAudiobookPlayer() {
  if (!state.audiobooks.player) return;
  restoreAudiobookBottombarNodes();
  audiobookPlayerModal?.showModal();
}

async function openAudiobookPlayer(item) {
  audiobookPlayerOpenItem = item;
  const canEditThisBook = canManageUsers() || canDeleteAudiobooks() || (!!state.auth.user?.id && state.auth.user.id === item.uploaded_by);
  audiobookPlayerEditButton?.classList.toggle("hidden", !canEditThisBook);
  // Re-opening the book that's already loaded (just minimized) should pick up
  // where it is, not reload the audiobook and restart playback from scratch.
  if (state.audiobooks.player?.id === item.id && audiobookAudioEl && !audiobookPlayerModal?.open) {
    expandAudiobookPlayer();
    return;
  }
  // A different book than the one minimized (or none at all) - make sure the
  // slider/controls are back in the dialog before it shows, instead of stuck
  // out in a mini player that's about to be hidden.
  restoreAudiobookBottombarNodes();

  const audio = getAudiobookAudioElement();
  // Switching straight from another book: save its position and tell other
  // devices it stopped now, while player state still points at it. The
  // pause event this queues lands after state (and src) have moved to the
  // new book, and is ignored by saveAudiobookProgressNow's loaded check.
  if (state.audiobooks.player && state.audiobooks.player.id !== item.id) {
    saveAudiobookProgressNow();
    sendAudiobookPlaybackStoppedSignal();
  }
  audio.pause();
  cancelSleepTimer({ silent: true });
  resetAudiobookStreamRecovery();
  const chapters = normalizeAudiobookChapters(item.chapters || [], item.duration_seconds || 0);
  state.audiobooks.player = { id: item.id, streamUrl: item.stream_url, chapters, durationSeconds: item.duration_seconds || 0, bookmarks: [] };

  audiobookPlayerFlip?.classList.remove("is-flipped");
  if (audiobookPlayerTitle) audiobookPlayerTitle.textContent = audiobookDisplayTitle(item);
  if (audiobookPlayerAuthor) {
    audiobookPlayerAuthor.textContent = item.author || "";
    audiobookPlayerAuthor.classList.toggle("hidden", !item.author);
  }
  if (audiobookPlayerCover) {
    if (item.cover_url) {
      audiobookPlayerCover.src = item.cover_url;
      audiobookPlayerCover.classList.remove("hidden");
      audiobookPlayerCoverFallback?.classList.add("hidden");
    } else {
      audiobookPlayerCover.removeAttribute("src");
      audiobookPlayerCover.classList.add("hidden");
      audiobookPlayerCoverFallback?.classList.remove("hidden");
    }
  }
  renderAudiobookPlayerMeta(item);
  renderAudiobookPlayerChapters(chapters);
  setAudiobookMediaSessionMetadata(item);
  updateAudiobookMediaSessionChapterHandlers(chapters.length > 0);
  audiobookTakeoverConfirmToken++;
  audiobookTakeoverConfirming = false;
  audiobookYieldedToOtherDevice = false;
  setAudiobookPlayerStatus("");
  // Clear the previous book's chapter label before rendering the mini
  // player's info line below - renderAudiobookMiniPlayerItem reads
  // audiobookActiveChapterLabel as its subtitle's first choice, so clearing
  // it after would leave the old book's chapter name showing (as the new
  // book's subtitle) until the next progress tick caught up.
  audiobookActiveChapterLabel = "";
  if (audiobookPlayerChapterLabel) audiobookPlayerChapterLabel.classList.add("hidden");
  renderAudiobookMiniPlayerItem(item);
  if (audiobookPlayerProgress) audiobookPlayerProgress.value = "0";
  if (audiobookPlayerCurrentTime) audiobookPlayerCurrentTime.textContent = "0:00";
  if (audiobookPlayerDuration) audiobookPlayerDuration.textContent = "0:00";
  if (audiobookPlayerRemaining) audiobookPlayerRemaining.textContent = "";
  setAudiobookVolumeExpanded(false);
  if (audiobookPlayerSpeed) audiobookPlayerSpeed.value = "1";
  audio.playbackRate = 1;
  applyDefaultMediaVolume(audio);
  if (audiobookPlayerVolume) audiobookPlayerVolume.value = `${Math.round(loadPreferredMediaVolume() * 100)}`;
  updateSleepTimerButtonUI();

  audiobookPlayerModal?.showModal();
  audiobookResumeRequestToken++;
  audio.src = item.stream_url;
  setAudiobookPlayPending("open");
  renderAudiobookPlaybackElsewhere(null);

  // Autoplay-on-open has to wait on the audio metadata, the elsewhere-check,
  // and a fresh read of the saved position, whichever lands last - jumping
  // straight to play() the instant metadata is ready would start a second
  // simultaneous playback before we've had a chance to find out this book is
  // already playing on another device. The position is re-read rather than
  // taken from the library list, which is only as fresh as when it loaded
  // and misses anything listened to on another device since.
  let resumeAt = item.position_seconds || 0;
  let metadataReady = false;
  let elsewhereChecked = false;
  let positionChecked = false;
  const tryAutoplayDecision = () => {
    if (!metadataReady || !elsewhereChecked || !positionChecked) return;
    // A press of play (or of play then pause) while this was still waiting
    // already decided what happens - autoplaying on top would override it.
    if (audiobookPlayPending !== "open" || state.audiobooks.player?.id !== item.id) return;
    // audio.duration can still be NaN here for some files even after
    // loadedmetadata (e.g. an M4B whose real length needs another range
    // request the browser hasn't made yet) - falling back to the server's
    // own stored duration keeps a resume from silently being skipped just
    // because the upper-bound check had nothing finite to compare against.
    const knownDuration = Number.isFinite(audio.duration) && audio.duration > 0
      ? audio.duration
      : state.audiobooks.player?.durationSeconds || 0;
    if (resumeAt > 1 && (!knownDuration || resumeAt < knownDuration - 2)) {
      audio.currentTime = resumeAt;
    }
    if (audiobookPlaybackElsewhereState) {
      setAudiobookPlayPending(null);
      setAudiobookPlayerStatus(AUDIOBOOK_ELSEWHERE_STATUS);
      return;
    }
    startAudiobookPlayback(audio);
  };
  const onReady = () => {
    metadataReady = true;
    tryAutoplayDecision();
    audio.removeEventListener("loadedmetadata", onReady);
  };
  audio.addEventListener("loadedmetadata", onReady);

  // Both checks give up after a while: the play button spins until they
  // land, and a request stuck on a bad signal shouldn't hold it there
  // forever. A timed-out check counts the same as a failed one.
  fetchAudiobookPlaybackElsewhere(item.id, { timeoutMs: AUDIOBOOK_OPEN_CHECK_TIMEOUT_MS }).then((info) => {
    elsewhereChecked = true;
    if (state.audiobooks.player?.id !== item.id) return;
    renderAudiobookPlaybackElsewhere(info);
    tryAutoplayDecision();
  }).catch((error) => {
    console.error(error);
    elsewhereChecked = true;
    tryAutoplayDecision();
  });

  fetchAudiobookSavedPosition(item.id, { timeoutMs: AUDIOBOOK_OPEN_CHECK_TIMEOUT_MS }).then((position) => {
    if (state.audiobooks.player?.id !== item.id) return;
    // This device's own save from just before (closing the player, then
    // reopening the same book) can still be in flight, so the server may not
    // have it yet - a save that recent is newer than anything it could hold.
    const localSave = audiobookLastLocalSave;
    const localSaveIsFresh = localSave?.id === item.id && performance.now() - localSave.at < AUDIOBOOK_LOCAL_SAVE_TRUST_MS;
    if (position !== null && !localSaveIsFresh) {
      resumeAt = position;
      item.position_seconds = position;
      // A metadata edit while opening swaps in a copy of the item; a press
      // of play reads its starting point from that copy.
      if (audiobookPlayerOpenItem?.id === item.id) audiobookPlayerOpenItem.position_seconds = position;
    }
    positionChecked = true;
    tryAutoplayDecision();
  });

  audiobookSyncTickCount = 0;
  audiobookSyncActive = true;
  getAudiobookSyncWorker();

  fetchAudiobookBookmarks(item.id).catch((error) => console.error(error));
}

function renderAudiobookPlayerMeta(item) {
  if (!audiobookPlayerMeta) return;
  const rows = [
    ["Title", audiobookDisplayTitle(item)],
    ["Author", item.author || "Unknown"],
    ["Narrator", item.narrator || ""],
    ["File", item.filename || ""],
    ["Size", formatSize(item.size_bytes || 0)],
    ["Duration", item.duration_seconds > 0 ? formatMediaTime(item.duration_seconds) : "Unknown"],
  ];
  if (item.chapters?.length) rows.push(["Chapters", String(item.chapters.length)]);
  if (item.tags?.length) rows.push(["Tags", item.tags.join(", ")]);
  audiobookPlayerMeta.innerHTML = rows
    .filter(([, value]) => value)
    .map(([label, value]) => `<div class="audiobook-player-meta-row"><dt>${escapeHTML(label)}</dt><dd>${escapeHTML(value)}</dd></div>`)
    .join("");
}

// A seek to a chapter's start can land a few milliseconds short of it - the
// browser snaps to an audio frame boundary - which would otherwise count as
// still being at the very end of the previous chapter: the wrong chapter
// highlighted, a "-0:00" seek bar, and "next chapter" re-jumping to the same
// spot. Positions this close before a chapter start count as inside it.
const AUDIOBOOK_CHAPTER_BOUNDARY_TOLERANCE_SECONDS = 0.5;

// normalizeAudiobookChapters sorts chapters into playback order and repairs
// missing/zero-length/overlapping end times (each chapter runs until the
// next starts, the last until the end of the book) - the same repair the
// server now applies at upload, repeated here for books probed before that.
function normalizeAudiobookChapters(chapters, durationSeconds) {
  const sorted = chapters
    .map((chapter) => ({ ...chapter, start_seconds: Math.max(0, Number(chapter.start_seconds) || 0), end_seconds: Number(chapter.end_seconds) || 0 }))
    .sort((a, b) => a.start_seconds - b.start_seconds);
  sorted.forEach((chapter, index) => {
    const limit = index + 1 < sorted.length ? sorted[index + 1].start_seconds : durationSeconds;
    if (chapter.end_seconds <= chapter.start_seconds || (limit > chapter.start_seconds && chapter.end_seconds > limit)) {
      chapter.end_seconds = limit;
    }
  });
  return sorted;
}

// getCurrentAudiobookChapterBounds returns the {start, end} of the chapter
// containing currentTime, or null when there is no chapter data - callers
// fall back to book-wide progress in that case.
function getCurrentAudiobookChapterBounds(currentTime) {
  const chapters = state.audiobooks.player?.chapters;
  if (!chapters || chapters.length === 0) return null;
  const index = findActiveAudiobookChapterIndex(currentTime, chapters);
  if (index < 0) return null;
  const chapter = chapters[index];
  if (!(chapter.end_seconds > chapter.start_seconds)) return null;
  return { start: chapter.start_seconds, end: chapter.end_seconds };
}

function updateAudiobookPlayerProgressUI() {
  const audio = audiobookAudioEl;
  const player = state.audiobooks.player;
  if (!audio || !player) return;
  const duration = Number.isFinite(audio.duration) ? audio.duration : 0;
  const bookRemaining = (duration || player.durationSeconds) - audio.currentTime;

  // The current-time/duration labels and the slider are chapter-relative
  // when chapter data is available - "how far into this chapter, how much of
  // it is left" - matching how most audiobook apps scrub. The remaining-time
  // line underneath always tracks the whole book regardless.
  const chapterBounds = getCurrentAudiobookChapterBounds(audio.currentTime);
  if (chapterBounds) {
    const chapterLength = chapterBounds.end - chapterBounds.start;
    const elapsedInChapter = Math.min(Math.max(audio.currentTime - chapterBounds.start, 0), chapterLength);
    if (audiobookPlayerCurrentTime) audiobookPlayerCurrentTime.textContent = formatMediaTime(elapsedInChapter);
    if (audiobookPlayerDuration) audiobookPlayerDuration.textContent = `-${formatMediaTime(chapterLength - elapsedInChapter)}`;
    if (audiobookPlayerProgress && !audiobookPlayerProgressDragging) {
      audiobookPlayerProgress.value = String(Math.round((elapsedInChapter / chapterLength) * 1000));
    }
  } else {
    if (audiobookPlayerCurrentTime) audiobookPlayerCurrentTime.textContent = formatMediaTime(audio.currentTime);
    if (audiobookPlayerDuration) audiobookPlayerDuration.textContent = formatMediaTime(duration);
    if (duration > 0 && audiobookPlayerProgress && !audiobookPlayerProgressDragging) {
      audiobookPlayerProgress.value = String(Math.round((audio.currentTime / duration) * 1000));
    }
  }

  if (audiobookPlayerRemaining) {
    audiobookPlayerRemaining.textContent = bookRemaining > 1 ? formatAudiobookRemaining(bookRemaining) : "";
  }
  updateAudiobookProgressTrack(audio, chapterBounds, duration);
  highlightActiveAudiobookChapter(audio.currentTime);
  updateAudiobookChapterNavState(audio.currentTime);
  updateAudiobookActiveChapterLabel(audio.currentTime);
  setAudiobookMediaSessionPositionState(audio);
}

// updateAudiobookProgressTrack paints the seek bar's track: the played part,
// then a lighter band for what's already downloaded ahead of the playhead -
// so it's visible whether jumping forward will play right away or stall.
// Same chapter-relative scale as the slider itself. Only the buffered range
// containing the playhead counts; a range further ahead (from an earlier
// seek) doesn't help playback continue from here.
function updateAudiobookProgressTrack(audio, chapterBounds, duration) {
  if (!audiobookPlayerProgress) return;
  const start = chapterBounds ? chapterBounds.start : 0;
  const length = chapterBounds ? chapterBounds.end - chapterBounds.start : duration;
  let bufferedEnd = audio.currentTime;
  const ranges = audio.buffered;
  for (let i = 0; i < ranges.length; i++) {
    if (ranges.start(i) <= audio.currentTime + 0.5 && ranges.end(i) >= audio.currentTime) {
      bufferedEnd = ranges.end(i);
      break;
    }
  }
  const played = Number(audiobookPlayerProgress.value) / 10;
  const buffered = length > 0 ? Math.min(Math.max(((bufferedEnd - start) / length) * 100, 0), 100) : 0;
  audiobookPlayerProgress.style.setProperty("--played", `${played}%`);
  audiobookPlayerProgress.style.setProperty("--buffered", `${Math.max(buffered, played)}%`);
}

function renderAudiobookPlayerChapters(chapters) {
  if (!audiobookPlayerChapters) return;
  const hasChapters = chapters.length > 0;
  audiobookPlayerChaptersToggle?.classList.toggle("hidden", !hasChapters);
  if (audiobookPlayerPrevChapter) audiobookPlayerPrevChapter.disabled = !hasChapters;
  if (audiobookPlayerNextChapter) audiobookPlayerNextChapter.disabled = !hasChapters;
  // Always start collapsed - the panel overlays the cover art once opened, so
  // it should be an explicit choice rather than something that is already
  // covering part of the player when it opens.
  setAudiobookSidePanel(audiobookPlayerChaptersPanel, audiobookPlayerChaptersToggle, false);
  audiobookPlayerChapters.innerHTML = chapters.map((chapter, index) => `
    <button type="button" class="audiobook-player-chapter" data-chapter-index="${index}" data-chapter-start="${chapter.start_seconds}">
      <span class="audiobook-player-chapter-title">${escapeHTML(chapter.title || `Chapter ${index + 1}`)}</span>
      <span class="audiobook-player-chapter-time">${formatMediaTime(chapter.start_seconds)}</span>
    </button>
  `).join("");

  audiobookPlayerChapters.querySelectorAll(".audiobook-player-chapter").forEach((button) => {
    button.addEventListener("click", () => {
      const audio = getAudiobookAudioElement();
      audio.currentTime = Number(button.dataset.chapterStart) || 0;
      if (audio.paused) audio.play().catch((error) => console.error(error));
      // Below the chapters-panel-as-overlay breakpoint the panel covers the
      // whole player, so close it once a chapter is picked instead of
      // leaving it blocking the view.
      if (window.innerWidth < 900) setAudiobookSidePanel(audiobookPlayerChaptersPanel, audiobookPlayerChaptersToggle, false);
    });
  });
}

function highlightActiveAudiobookChapter(currentTime) {
  const chapters = state.audiobooks.player?.chapters;
  if (!chapters || chapters.length === 0 || !audiobookPlayerChapters) return;
  const activeIndex = findActiveAudiobookChapterIndex(currentTime, chapters);
  audiobookPlayerChapters.querySelectorAll(".audiobook-player-chapter").forEach((button, index) => {
    button.classList.toggle("is-active", index === activeIndex);
  });
}

// audiobookActiveChapterLabel is the current "Chapter N" / chapter title
// text, shared by the full player's label under the author line and the
// mini bar's subtitle (see renderAudiobookMiniPlayerInfo). Kept as module
// state, rather than recomputed inline wherever it's needed, so a change
// only triggers a mini-bar re-render when the chapter actually changes.
let audiobookActiveChapterLabel = "";

function updateAudiobookActiveChapterLabel(currentTime) {
  const chapters = state.audiobooks.player?.chapters || [];
  const activeIndex = findActiveAudiobookChapterIndex(currentTime, chapters);
  const chapter = activeIndex >= 0 ? chapters[activeIndex] : null;
  const label = chapter ? (chapter.title || `Chapter ${activeIndex + 1}`) : "";
  if (audiobookPlayerChapterLabel) {
    audiobookPlayerChapterLabel.textContent = label;
    audiobookPlayerChapterLabel.classList.toggle("hidden", !label);
  }
  if (label !== audiobookActiveChapterLabel) {
    audiobookActiveChapterLabel = label;
    renderAudiobookMiniPlayerInfo();
  }
}

// findActiveAudiobookChapterIndex returns the index of the chapter containing
// currentTime, or -1 if there are no chapters / none has started yet.
function findActiveAudiobookChapterIndex(currentTime, chapters) {
  let activeIndex = -1;
  for (let i = 0; i < chapters.length; i += 1) {
    if (currentTime + AUDIOBOOK_CHAPTER_BOUNDARY_TOLERANCE_SECONDS >= chapters[i].start_seconds) activeIndex = i;
  }
  return activeIndex;
}

function updateAudiobookChapterNavState(currentTime) {
  const chapters = state.audiobooks.player?.chapters || [];
  if (!audiobookPlayerPrevChapter || !audiobookPlayerNextChapter) return;
  if (chapters.length === 0) {
    audiobookPlayerPrevChapter.disabled = true;
    audiobookPlayerNextChapter.disabled = true;
    return;
  }
  const activeIndex = findActiveAudiobookChapterIndex(currentTime, chapters);
  audiobookPlayerPrevChapter.disabled = activeIndex <= 0 && currentTime < 3;
  audiobookPlayerNextChapter.disabled = activeIndex >= chapters.length - 1;
}

// jumpAudiobookChapter moves to the previous/next chapter boundary. Jumping
// "back" while more than 3s into the current chapter restarts that chapter
// instead of skipping to the one before it, matching how most audiobook/
// podcast players treat the previous-track button.
function jumpAudiobookChapter(direction) {
  const audio = getAudiobookAudioElement();
  const chapters = state.audiobooks.player?.chapters || [];
  if (chapters.length === 0) return;
  const activeIndex = findActiveAudiobookChapterIndex(audio.currentTime, chapters);

  if (direction < 0) {
    const current = chapters[activeIndex];
    if (current && audio.currentTime - current.start_seconds > 3) {
      audio.currentTime = current.start_seconds;
    } else {
      audio.currentTime = chapters[Math.max(0, activeIndex - 1)].start_seconds;
    }
  } else {
    const nextIndex = Math.min(chapters.length - 1, activeIndex + 1);
    audio.currentTime = chapters[nextIndex].start_seconds;
  }
  updateAudiobookPlayerProgressUI();
}

// --- Side panels (chapters / bookmarks) -----------------------------------

function setAudiobookSidePanel(panel, toggle, open) {
  panel?.classList.toggle("hidden", !open);
  toggle?.setAttribute("aria-expanded", String(open));
  toggle?.classList.toggle("is-active", open);
}

// toggleAudiobookSidePanel opens the given panel and closes the other one:
// chapters and bookmarks share the same overlay footprint, so showing both at
// once would just stack them on top of each other.
function toggleAudiobookSidePanel(panel, toggle, otherPanel, otherToggle) {
  const opening = panel?.classList.contains("hidden");
  setAudiobookSidePanel(panel, toggle, !!opening);
  if (opening) setAudiobookSidePanel(otherPanel, otherToggle, false);
}

// --- Bookmarks -------------------------------------------------------------

async function fetchAudiobookBookmarks(audiobookId) {
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(audiobookId)}/bookmarks`, { credentials: "same-origin" });
    if (!response.ok) return;
    const payload = await response.json();
    // The player may have moved on to a different book (or closed) while this
    // request was in flight.
    if (state.audiobooks.player?.id !== audiobookId) return;
    state.audiobooks.player.bookmarks = payload.bookmarks || [];
    renderAudiobookPlayerBookmarks();
  } catch (error) {
    console.error(error);
  }
}

function renderAudiobookPlayerBookmarks() {
  const bookmarks = state.audiobooks.player?.bookmarks || [];
  if (!audiobookPlayerBookmarks) return;
  audiobookPlayerBookmarksEmpty?.classList.toggle("hidden", bookmarks.length > 0);
  audiobookPlayerBookmarks.innerHTML = bookmarks.map((bookmark) => `
    <div class="audiobook-player-bookmark-row" data-bookmark-id="${escapeHTML(bookmark.id)}">
      <button type="button" class="audiobook-player-bookmark-jump" data-bookmark-start="${bookmark.position_seconds}">
        <span class="audiobook-player-bookmark-label">${escapeHTML(bookmark.label || "Bookmark")}</span>
        <span class="audiobook-player-bookmark-time">${formatMediaTime(bookmark.position_seconds)}</span>
        <span class="audiobook-player-bookmark-created" title="${escapeHTML(audiobookBookmarkCreatedTitle(bookmark))}">${escapeHTML(formatRelativeTime(bookmark.created_at))}</span>
        ${renderAudiobookBookmarkExpiry(bookmark)}
      </button>
      <button type="button" class="audiobook-player-bookmark-delete" aria-label="Delete bookmark">
        <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M5 7h14" /><path d="M8 7V5.5A1.5 1.5 0 0 1 9.5 4h5A1.5 1.5 0 0 1 16 5.5V7" /><path d="M7 7v11.5A1.5 1.5 0 0 0 8.5 20h7a1.5 1.5 0 0 0 1.5-1.5V7" /></svg>
      </button>
    </div>
  `).join("");

  audiobookPlayerBookmarks.querySelectorAll(".audiobook-player-bookmark-row").forEach((row) => {
    const id = row.dataset.bookmarkId;
    row.querySelector(".audiobook-player-bookmark-jump")?.addEventListener("click", (event) => {
      const audio = getAudiobookAudioElement();
      audio.currentTime = Number(event.currentTarget.dataset.bookmarkStart) || 0;
      if (audio.paused) {
        cancelAudiobookPlayRequest();
        startAudiobookPlayback(audio);
      }
      markAudiobookBookmarkUsed(id).catch((error) => console.error(error));
      if (window.innerWidth < 900) setAudiobookSidePanel(audiobookPlayerBookmarksPanel, audiobookPlayerBookmarksToggle, false);
    });
    row.querySelector(".audiobook-player-bookmark-delete")?.addEventListener("click", () => {
      deleteAudiobookBookmark(id).catch((error) => console.error(error));
    });
  });
}

// Bookmarks expire after going unused for the server's configured window
// (MEMEINDEX_AUDIOBOOK_BOOKMARK_EXPIRY_DAYS, six months by default). The
// expiry date is always in the row's tooltip, but only spelled out once it
// is close enough to act on - otherwise every row would carry a "expires in
// 6 months" line that means nothing yet.
const AUDIOBOOK_BOOKMARK_EXPIRY_WARNING_DAYS = 14;

function renderAudiobookBookmarkExpiry(bookmark) {
  if (!bookmark.expires_at) return "";
  const expiresAt = new Date(bookmark.expires_at);
  if (Number.isNaN(expiresAt.getTime())) return "";
  const daysLeft = (expiresAt.getTime() - Date.now()) / 86400000;
  if (daysLeft > AUDIOBOOK_BOOKMARK_EXPIRY_WARNING_DAYS) return "";
  const title = `Removed ${formatDateTimeZoned(expiresAt)} unless used - jumping to it resets the clock`;
  const when = daysLeft < 1 ? "today" : `in ${Math.ceil(daysLeft)} day${Math.ceil(daysLeft) === 1 ? "" : "s"}`;
  return `<span class="audiobook-player-bookmark-expiry" title="${escapeHTML(title)}">Expires ${when} - tap to keep</span>`;
}

function audiobookBookmarkCreatedTitle(bookmark) {
  const created = formatDateTimeZoned(bookmark.created_at);
  const expiresAt = bookmark.expires_at ? new Date(bookmark.expires_at) : null;
  if (!expiresAt || Number.isNaN(expiresAt.getTime())) return created;
  return `${created} · expires ${formatDateTimeZoned(expiresAt)} unless used`;
}

// markAudiobookBookmarkUsed restarts a bookmark's expiry clock after a jump
// to it, and refreshes its row with the new expiry.
async function markAudiobookBookmarkUsed(bookmarkId) {
  const player = state.audiobooks.player;
  if (!player) return;
  const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/bookmarks/${encodeURIComponent(bookmarkId)}/use`, {
    method: "POST",
    credentials: "same-origin",
  });
  // 404: it expired, was deleted on another device, or was replaced by a
  // newer sleep-timer bookmark while this list was open - drop the row
  // rather than leave a bookmark that can't be kept.
  if (response.status === 404) {
    if (state.audiobooks.player?.id !== player.id) return;
    state.audiobooks.player.bookmarks = (state.audiobooks.player.bookmarks || []).filter((entry) => entry.id !== bookmarkId);
    renderAudiobookPlayerBookmarks();
    showToast("That bookmark no longer exists, so it was removed from the list.", "info", { title: "Audiobooks" });
    return;
  }
  if (!response.ok) return;
  const updated = await response.json();
  if (state.audiobooks.player?.id !== player.id) return;
  state.audiobooks.player.bookmarks = (state.audiobooks.player.bookmarks || []).map((entry) => (entry.id === updated.id ? updated : entry));
  renderAudiobookPlayerBookmarks();
}

// flashAudiobookActionButton briefly pulses a button green (and, if a label
// element/text is given, swaps its text) to confirm an action landed, on top
// of whatever toast is also shown - a toast is easy to miss while looking at
// the button you just tapped.
function flashAudiobookActionButton(button, labelEl, successText) {
  if (!button) return;
  const originalText = labelEl ? labelEl.textContent : null;
  if (labelEl && successText) labelEl.textContent = successText;
  button.classList.remove("is-success");
  // Force a reflow so re-adding the class restarts the CSS animation even if
  // it is still playing from a previous tap.
  void button.offsetWidth;
  button.classList.add("is-success");
  window.setTimeout(() => {
    button.classList.remove("is-success");
    if (labelEl && originalText !== null) labelEl.textContent = originalText;
  }, 1200);
}

async function createAudiobookBookmark(label, { sleepTimer = false } = {}) {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/bookmarks`, {
      method: "POST",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ positionSeconds: audio.currentTime || 0, label: label || "", sleepTimer }),
    });
    if (!(await expectAuthorized(response, "Could not add bookmark."))) return;
    const bookmark = await response.json();
    if (state.audiobooks.player?.id !== player.id) return;
    // A sleep-timer bookmark replaces earlier ones close by in the book.
    const replaced = new Set(bookmark.replaced_bookmark_ids || []);
    state.audiobooks.player.bookmarks = [...(state.audiobooks.player.bookmarks || []).filter((entry) => !replaced.has(entry.id)), bookmark]
      .sort((a, b) => new Date(b.created_at) - new Date(a.created_at));
    renderAudiobookPlayerBookmarks();
    return bookmark;
  } catch (error) {
    console.error(error);
    showToast("Could not add bookmark.", "error", { title: "Audiobooks" });
  }
}

async function deleteAudiobookBookmark(bookmarkId) {
  const player = state.audiobooks.player;
  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player?.id || "")}/bookmarks/${encodeURIComponent(bookmarkId)}`, {
      method: "DELETE",
      credentials: "same-origin",
    });
    if (!(await expectAuthorized(response, "Could not delete bookmark."))) return;
    if (state.audiobooks.player) {
      state.audiobooks.player.bookmarks = (state.audiobooks.player.bookmarks || []).filter((entry) => entry.id !== bookmarkId);
      renderAudiobookPlayerBookmarks();
    }
  } catch (error) {
    console.error(error);
    showToast("Could not delete bookmark.", "error", { title: "Audiobooks" });
  }
}

// --- Sleep timer -------------------------------------------------------------

// A running timer is either a fixed number of minutes (wall clock, endsAt)
// or "end of chapter" (chapterEnd, in book seconds - it follows playback, so
// pausing or changing speed moves it with the audio).
let audiobookSleepTimer = null; // { mode: "minutes"|"chapter", endsAt?, chapterEnd?, minutes?, intervalId }
let sleepTimerRepeat = null; // hold-to-repeat timers for the +/- buttons

const SLEEP_TIMER_MIN_MINUTES = 5;
const SLEEP_TIMER_MAX_MINUTES = 24 * 60;
const SLEEP_TIMER_STORAGE_KEY = "memeindex.sleepTimerMinutes";

function openSleepTimerPicker() {
  if (!sleepTimerModal) return;
  const hasChapters = !!getCurrentAudiobookChapterBounds(audiobookAudioEl?.currentTime || 0);
  sleepTimerChapterPreset?.classList.toggle("hidden", !hasChapters);
  if (audiobookSleepTimer?.mode === "chapter" && hasChapters) {
    sleepTimerSelection = { mode: "chapter", minutes: sleepTimerSelection.minutes };
  } else {
    let minutes = audiobookSleepTimer?.minutes;
    if (!minutes) {
      try { minutes = Number(window.localStorage.getItem(SLEEP_TIMER_STORAGE_KEY)); } catch (error) { /* Optional preference. */ }
    }
    sleepTimerSelection = { mode: "minutes", minutes: clampSleepTimerMinutes(minutes || 30) };
  }
  renderSleepTimerPicker();
  sleepTimerModal.showModal();
}

function clampSleepTimerMinutes(minutes) {
  return Math.min(SLEEP_TIMER_MAX_MINUTES, Math.max(SLEEP_TIMER_MIN_MINUTES, Math.round(minutes)));
}

// Finer steps for short timers, coarser once it's past an hour, so neither
// end of the range takes forever to reach with the +/- buttons.
function stepSleepTimerSelection(direction) {
  if (sleepTimerSelection.mode === "chapter") {
    sleepTimerSelection = { mode: "minutes", minutes: sleepTimerSelection.minutes };
  } else {
    const current = sleepTimerSelection.minutes;
    const step = direction > 0 ? (current < 60 ? 5 : 15) : (current <= 60 ? 5 : 15);
    // Move to the next multiple of the step rather than adding it, so odd
    // values (a remembered 37) snap back onto round numbers.
    const snapped = direction > 0 ? Math.floor(current / step) * step + step : Math.ceil(current / step) * step - step;
    sleepTimerSelection.minutes = clampSleepTimerMinutes(snapped);
    // At a limit the button disables itself and may never see pointerup.
    if (sleepTimerSelection.minutes === current) stopSleepTimerRepeat();
  }
  renderSleepTimerPicker();
}

function formatSleepTimerDuration(minutes) {
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return rest ? `${hours}h ${rest}m` : `${hours}h`;
}

function renderSleepTimerPicker() {
  if (!sleepTimerValue) return;
  const { mode, minutes } = sleepTimerSelection;
  sleepTimerValue.replaceChildren();
  sleepTimerValue.classList.toggle("is-chapter", mode === "chapter");
  const addPart = (text, className) => {
    const span = document.createElement("span");
    span.className = className;
    span.textContent = text;
    sleepTimerValue.append(span);
  };
  if (mode === "chapter") {
    addPart("End of chapter", "sleep-timer-value-number");
  } else if (minutes < 60) {
    addPart(String(minutes), "sleep-timer-value-number");
    addPart("min", "sleep-timer-value-unit");
  } else {
    addPart(String(Math.floor(minutes / 60)), "sleep-timer-value-number");
    addPart("h", "sleep-timer-value-unit");
    if (minutes % 60) {
      addPart(String(minutes % 60), "sleep-timer-value-number");
      addPart("m", "sleep-timer-value-unit");
    }
  }
  if (sleepTimerMinusButton) sleepTimerMinusButton.disabled = mode === "minutes" && minutes <= SLEEP_TIMER_MIN_MINUTES;
  if (sleepTimerPlusButton) sleepTimerPlusButton.disabled = mode === "minutes" && minutes >= SLEEP_TIMER_MAX_MINUTES;
  sleepTimerPresets?.querySelectorAll(".sleep-timer-preset").forEach((button) => {
    const active = button.dataset.mode === "chapter" ? mode === "chapter" : mode === "minutes" && Number(button.dataset.minutes) === minutes;
    button.classList.toggle("is-active", active);
  });
  sleepTimerOffButton?.classList.toggle("hidden", !audiobookSleepTimer);
  if (sleepTimerStartButton) sleepTimerStartButton.textContent = audiobookSleepTimer ? "Restart timer" : "Start timer";
}

// Press-and-hold on +/- keeps stepping, since there's no upper limit and
// tapping out to several hours would be tedious.
function startSleepTimerRepeat(direction) {
  stopSleepTimerRepeat();
  stepSleepTimerSelection(direction);
  sleepTimerRepeat = {
    timeoutId: window.setTimeout(() => {
      sleepTimerRepeat.intervalId = window.setInterval(() => stepSleepTimerSelection(direction), 110);
    }, 420),
  };
}

function stopSleepTimerRepeat() {
  if (!sleepTimerRepeat) return;
  window.clearTimeout(sleepTimerRepeat.timeoutId);
  window.clearInterval(sleepTimerRepeat.intervalId);
  sleepTimerRepeat = null;
}

async function startSleepTimer(selection) {
  cancelSleepTimer({ silent: true });

  let chapterEnd = null;
  if (selection.mode === "chapter") {
    chapterEnd = getCurrentAudiobookChapterBounds(audiobookAudioEl?.currentTime || 0)?.end;
    if (!chapterEnd) {
      showToast("This audiobook has no chapter data.", "error", { title: "Audiobooks" });
      return;
    }
  } else {
    try { window.localStorage.setItem(SLEEP_TIMER_STORAGE_KEY, String(selection.minutes)); } catch (error) { /* Optional preference. */ }
  }

  // Drop a bookmark right where playback is now, before the timer starts
  // counting down - if the listener drifts off, this marks roughly where
  // they still were awake rather than wherever playback happened to stop.
  const bookmark = await createAudiobookBookmark("Sleep timer started", { sleepTimer: true });
  // Another start may have finished while the bookmark was saving - replace
  // it rather than leaving its interval running.
  cancelSleepTimer({ silent: true });

  // Chapter mode ticks faster so the pause lands close to the boundary
  // instead of up to a second into the next chapter.
  const intervalId = window.setInterval(updateSleepTimerButtonUI, selection.mode === "chapter" ? 250 : 1000);
  audiobookSleepTimer = selection.mode === "chapter"
    ? { mode: "chapter", chapterEnd, lastTime: audiobookAudioEl?.currentTime || 0, lastTick: Date.now(), intervalId }
    : { mode: "minutes", endsAt: Date.now() + selection.minutes * 60 * 1000, minutes: selection.minutes, intervalId };
  updateSleepTimerButtonUI();
  // The label already switches to a live countdown, which is its own
  // feedback - just pulse the button rather than swapping text that the
  // countdown tick would immediately overwrite anyway.
  flashAudiobookActionButton(audiobookPlayerSleepTimerButton, null, null);
  const what = selection.mode === "chapter" ? "the end of this chapter" : formatSleepTimerDuration(selection.minutes);
  showToast(
    bookmark ? `Sleep timer set for ${what}. Bookmarked your spot.` : `Sleep timer set for ${what}.`,
    "success",
    { title: "Audiobooks" },
  );
}

// getSleepTimerRemainingSeconds returns how long is left on the running
// timer, or 0 once it's due. In chapter mode only playing across the
// boundary fires: a seek (including the next-chapter button, which lands
// exactly on the boundary) retargets to the end of whichever chapter it
// landed in. A move counts as playback when it's no further than the
// elapsed wall time at the current speed, plus slack for timer jitter.
function getSleepTimerRemainingSeconds() {
  const timer = audiobookSleepTimer;
  if (timer.mode !== "chapter") return Math.max(0, (timer.endsAt - Date.now()) / 1000);
  const audio = audiobookAudioEl;
  if (!audio) return 0;
  const time = audio.currentTime;
  const now = Date.now();
  const rate = audio.playbackRate || 1;
  const moved = time - timer.lastTime;
  const played = moved >= 0 && moved <= ((now - timer.lastTick) / 1000 + 0.5) * rate;
  const crossed = timer.lastTime < timer.chapterEnd && time >= timer.chapterEnd;
  timer.lastTime = time;
  timer.lastTick = now;
  if (crossed && played) return 0;
  // Only retarget after a seek: chapter lookup treats the last moment
  // before a boundary as the next chapter, so doing it during normal
  // playback would hop the target forward just before it's reached.
  if (!played) {
    const bounds = getCurrentAudiobookChapterBounds(time);
    if (bounds) timer.chapterEnd = bounds.end;
  }
  return Math.max(0, (timer.chapterEnd - time) / rate);
}

function formatSleepCountdown(totalSeconds) {
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  const mmss = `${String(minutes).padStart(hours ? 2 : 1, "0")}:${String(seconds).padStart(2, "0")}`;
  return hours ? `${hours}:${mmss}` : mmss;
}

function cancelSleepTimer({ silent } = {}) {
  if (!audiobookSleepTimer) return;
  window.clearInterval(audiobookSleepTimer.intervalId);
  audiobookSleepTimer = null;
  updateSleepTimerButtonUI();
  if (!silent) showToast("Sleep timer turned off.", "info", { title: "Audiobooks" });
}

// Keeps every sleep timer surface in step: the action button label, the big
// countdown over the cover, and the status line in the picker.
function updateSleepTimerButtonUI() {
  const remainingSeconds = audiobookSleepTimer ? getSleepTimerRemainingSeconds() : 0;
  if (audiobookSleepTimer && remainingSeconds <= 0) {
    sleepTimerElapsed();
    return;
  }
  const active = !!audiobookSleepTimer;
  audiobookPlayerSleepTimerButton?.classList.toggle("is-active", active);
  audiobookPlayerSleepOverlay?.classList.toggle("hidden", !active);
  sleepTimerStatus?.classList.toggle("is-active", active);
  if (!active) {
    if (audiobookPlayerSleepTimerLabel) audiobookPlayerSleepTimerLabel.textContent = "Sleep";
    if (sleepTimerStatus) sleepTimerStatus.textContent = "Pauses playback and bookmarks your spot.";
    // The timer can end while the picker is open; drop its "Turn off" and
    // "Restart" state so the picker matches.
    if (sleepTimerModal?.open) renderSleepTimerPicker();
    return;
  }
  const totalSeconds = Math.ceil(remainingSeconds);
  const countdown = formatSleepCountdown(totalSeconds);
  if (audiobookPlayerSleepTimerLabel) audiobookPlayerSleepTimerLabel.textContent = countdown;
  if (audiobookPlayerSleepOverlayTime) audiobookPlayerSleepOverlayTime.textContent = countdown;
  audiobookPlayerSleepOverlay?.classList.toggle("is-ending", totalSeconds <= 60);
  audiobookPlayerSleepOverlay?.classList.toggle("has-hours", totalSeconds >= 3600);
  if (sleepTimerStatus) {
    sleepTimerStatus.textContent = audiobookSleepTimer.mode === "chapter" ? `Pausing at chapter end · ${countdown}` : `Pausing in ${countdown}`;
  }
}

function sleepTimerElapsed() {
  const chapterEnd = audiobookSleepTimer?.mode === "chapter" ? audiobookSleepTimer.chapterEnd : null;
  cancelSleepTimer({ silent: true });
  // Playback that's mid-reconnect counts as paused, but must not come back
  // after the listener asked it to stop.
  cancelAudiobookStreamRecovery();
  const audio = audiobookAudioEl;
  if (audio && !audio.paused) {
    audio.pause();
    // The tick can land a fraction of a second past the boundary - park
    // exactly on it so resuming starts cleanly at the next chapter.
    if (chapterEnd !== null && audio.currentTime > chapterEnd) audio.currentTime = chapterEnd;
    showToast("Sleep timer ended - playback paused.", "info", { title: "Audiobooks" });
  }
}

// isAudiobookAudioForPlayer reports whether the shared audio element is
// actually loaded with the open book's stream. getAttribute (not .src) so
// it compares the same relative URL that was assigned.
function isAudiobookAudioForPlayer(audio, player) {
  return !!player.streamUrl && audio.getAttribute("src") === player.streamUrl;
}

// hasAudiobookPositionToSave is isAudiobookAudioForPlayer plus "something is
// actually loaded": assigning a new src resets readyState to HAVE_NOTHING and
// currentTime to 0 synchronously, so an event queued by the previous book
// (its pause, when switching books mid-playback) would otherwise run after
// the new src is set and save 0 over the new book's real position.
function hasAudiobookPositionToSave(audio, player) {
  return isAudiobookAudioForPlayer(audio, player) && audio.readyState > HTMLMediaElement.HAVE_NOTHING;
}

// The last progress save this tab sent, so reopening a book right after
// closing it can tell its own just-sent save apart from what the server
// returns - see openAudiobookPlayer.
let audiobookLastLocalSave = null;
const AUDIOBOOK_LOCAL_SAVE_TRUST_MS = 5000;

function saveAudiobookProgressNow() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  if (audiobookYieldedToOtherDevice) return;
  // The audio element can still be holding (or just unloading) the previous
  // book for a moment after player state moves to a new one - its position
  // belongs to that book, not this one.
  if (!hasAudiobookPositionToSave(audio, player)) return;
  const position = audio.currentTime || 0;
  audiobookLastLocalSave = { id: player.id, at: performance.now() };
  fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/progress`, {
    method: "PUT",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ positionSeconds: position }),
    keepalive: true,
  }).catch((error) => console.error(error));

  const item = state.audiobooks.items.find((entry) => entry.id === player.id);
  if (item) item.position_seconds = position;
}

// Below this, a seek onto the saved position isn't worth doing - it's within
// normal drift (a heartbeat/save that landed a couple of seconds before the
// pause) rather than a real "someone else moved the bookmark" difference.
const AUDIOBOOK_RESUME_POSITION_TOLERANCE_SECONDS = 2;
const AUDIOBOOK_RESUME_POSITION_TIMEOUT_MS = 4000;
// Opening a book waits on these before autoplaying; longer than a press's
// timeout because nothing has started yet and the fresh position matters
// more than a quick start.
const AUDIOBOOK_OPEN_CHECK_TIMEOUT_MS = 8000;

// A press is only superseded by a later one while still paused (see the
// token check below) - once playback actually starts, audio.paused itself
// is enough to tell a stale resolution it's no longer relevant.
let audiobookResumeRequestToken = 0;

// resumeAudiobookPlaybackFromSavedPosition is what the play button actually
// calls (when no other device is actively playing - see the click handler):
// it re-fetches the server's saved position right before playing and jumps
// there first if it disagrees with whatever this tab already had loaded, so
// pressing play always reconciles with the database instead of trusting a
// position that might be stale (e.g. another device paused further along
// since this tab last opened or synced). Mirrors the "elsewhere" takeover's
// own re-check in pauseAudiobookElsewhereAndPlayHere, just without an active
// device to ask to stop first.
async function resumeAudiobookPlaybackFromSavedPosition() {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;
  // Mashing play (or play arriving from both a UI click and a media-session/
  // hardware key in close succession) while still paused would otherwise fire
  // this twice - each fetching the position independently and racing to seek
  // right before calling play(). Only the most recent press's result applies.
  const token = ++audiobookResumeRequestToken;
  // A browser that hasn't let this element play yet only allows play() from
  // inside the press itself - after the await below it is refused, silently
  // as far as the listener could tell. So the first play starts right away
  // and the fetched position is reconciled with a seek once it arrives.
  const startNow = !audiobookPlaybackUnlocked && !audio.error && !audiobookStreamRecovery;
  // Best position known without asking the server: what opening the book
  // fetched (or the library list had). Only used while the open flow still
  // hasn't seeked there itself - pressing play in that window would
  // otherwise start a long book from the top. Once it has, wherever the
  // element is (including a deliberate jump back to 0:00) is what counts.
  const openStillPending = audiobookPlayPending === "open";
  const knownPosition = openStillPending && audiobookPlayerOpenItem?.id === player.id ? Number(audiobookPlayerOpenItem.position_seconds) || 0 : 0;
  const knownDuration = () => (Number.isFinite(audio.duration) && audio.duration > 0 ? audio.duration : player.durationSeconds || 0);
  // Same bounds as the open flow's own resume, so a finished book (saved at
  // its very end) starts over instead of seeking there and ending at once.
  const seekToKnownPosition = () => {
    const duration = knownDuration();
    if (knownPosition > 1 && (!duration || knownPosition < duration - 2)) audio.currentTime = knownPosition;
  };
  if (startNow) {
    seekToKnownPosition();
    startAudiobookPlayback(audio);
  } else {
    setAudiobookPlayPending("resume");
  }
  const position = await fetchAudiobookSavedPosition(player.id, { timeoutMs: AUDIOBOOK_RESUME_POSITION_TIMEOUT_MS });
  // A newer press (or a cancel) owns the pending state from here.
  if (token !== audiobookResumeRequestToken) return;
  if (state.audiobooks.player?.id !== player.id) {
    setAudiobookPlayPending(null);
    return;
  }
  const seekToSavedPosition = () => {
    if (position === null || Math.abs(position - audio.currentTime) <= AUDIOBOOK_RESUME_POSITION_TOLERANCE_SECONDS) return;
    const duration = knownDuration();
    if (!duration || position < duration - 1) {
      audio.currentTime = position;
    }
  };
  if (startNow) {
    seekToSavedPosition();
    return;
  }
  // Already playing again (a media-session/hardware play key got there
  // first) - the fetched position is no longer relevant.
  if (!audio.paused) {
    setAudiobookPlayPending(null);
    return;
  }
  // A dead element won't start again from play() alone - the source has to be
  // reloaded, which also covers pressing play mid-reconnect.
  if (audio.error || audiobookStreamRecovery) {
    setAudiobookPlayPending(null);
    retryAudiobookStreamNow(audio, position ?? undefined);
    return;
  }
  seekToSavedPosition();
  if (position === null) seekToKnownPosition();
  startAudiobookPlayback(audio);
}

// setAudiobookPlayerPosition is the shared path for the "mark as not
// started"/"mark as finished" controls: it moves playback to an explicit
// position, saves it immediately (not on the usual 15s timer), and updates
// the card grid underneath so the progress badge reflects it right away.
async function setAudiobookPlayerPosition(positionSeconds) {
  const player = state.audiobooks.player;
  const audio = audiobookAudioEl;
  if (!player || !audio) return;

  audio.currentTime = positionSeconds;
  updateAudiobookPlayerProgressUI();

  try {
    const response = await fetch(`/api/audiobooks/${encodeURIComponent(player.id)}/progress`, {
      method: "PUT",
      credentials: "same-origin",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ positionSeconds }),
    });
    if (!(await expectAuthorized(response, "Could not update playback progress."))) {
      return;
    }
  } catch (error) {
    console.error(error);
    showToast("Could not update playback progress.", "error", { title: "Audiobooks" });
    return;
  }

  const item = state.audiobooks.items.find((entry) => entry.id === player.id);
  if (item) item.position_seconds = positionSeconds;
  renderAudiobooksGrid();
}

function closeAudiobookPlayer() {
  const audio = audiobookAudioEl;
  if (audio) {
    audio.pause();
    saveAudiobookProgressNow();
    // The "pause" event (and its heartbeat) fires as a queued task, which can
    // land after player state below is cleared - send the stopped signal
    // directly here instead of relying on that event's timing.
    sendAudiobookPlaybackStoppedSignal();
    audio.removeAttribute("src");
    audio.load();
  }
  audiobookSyncActive = false;
  resetAudiobookStreamRecovery();
  cancelAudiobookPlayRequest();
  audiobookTakeoverConfirmToken++;
  audiobookTakeoverConfirming = false;
  renderAudiobookPlaybackElsewhere(null);
  restoreAudiobookBottombarNodes();
  setAudiobookVolumeExpanded(false);
  cancelSleepTimer({ silent: true });
  state.audiobooks.player = null;
  audiobookPlayerModal?.close();
  if ("mediaSession" in navigator) {
    navigator.mediaSession.metadata = null;
    navigator.mediaSession.playbackState = "none";
  }
  renderAudiobooksGrid();
}

const AUDIOBOOK_DEVICE_ID_STORAGE_KEY = "memeindex.audiobookDeviceId";

let audiobookDeviceIdCache = null;

// getAudiobookDeviceId returns a per-browser random id, persisted in
// localStorage, used only to tell this browser's own playback heartbeats
// apart from another device's for the same account - not a real identity.
function getAudiobookDeviceId() {
  if (audiobookDeviceIdCache) return audiobookDeviceIdCache;
  const fallback = () => `dev-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  try {
    let id = window.localStorage.getItem(AUDIOBOOK_DEVICE_ID_STORAGE_KEY);
    if (!id) {
      id = window.crypto?.randomUUID ? window.crypto.randomUUID() : fallback();
      window.localStorage.setItem(AUDIOBOOK_DEVICE_ID_STORAGE_KEY, id);
    }
    audiobookDeviceIdCache = id;
  } catch (error) {
    audiobookDeviceIdCache = fallback();
  }
  return audiobookDeviceIdCache;
}
