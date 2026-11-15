// Shared media behaviour: the saved volume preference and the control bar
// used by the meme modal, the random reel and the home spotlight.

const MEDIA_VOLUME_STORAGE_KEY = "memeindex.mediaVolume";
const DEFAULT_MEDIA_VOLUME = 0.10;

function clampMediaVolume(value) {
  if (!Number.isFinite(value)) {
    return DEFAULT_MEDIA_VOLUME;
  }
  return Math.min(1, Math.max(0, value));
}

function loadPreferredMediaVolume() {
  try {
    const rawValue = window.localStorage.getItem(MEDIA_VOLUME_STORAGE_KEY);
    if (rawValue == null || rawValue === "") {
      return DEFAULT_MEDIA_VOLUME;
    }

    return clampMediaVolume(Number(rawValue));
  } catch (error) {
    return DEFAULT_MEDIA_VOLUME;
  }
}

function persistPreferredMediaVolume(volume) {
  const normalizedVolume = clampMediaVolume(volume);
  try {
    window.localStorage.setItem(MEDIA_VOLUME_STORAGE_KEY, `${normalizedVolume}`);
  } catch (error) {
    console.warn("Could not persist preferred media volume", error);
  }
}

function syncStoredMediaVolumeFromElement(media) {
  if (!(media instanceof HTMLMediaElement)) {
    return;
  }

  const volume = media.muted ? 0 : media.volume;
  persistPreferredMediaVolume(volume);
}

function applyDefaultMediaVolume(media) {
  const preferredVolume = loadPreferredMediaVolume();
  media.volume = preferredVolume;
  media.muted = preferredVolume === 0;
  media.defaultMuted = false;
}

// createMediaControls drives one player's control bar: a play/pause button, a
// seek bar with times, and a speaker button with a volume slider. The meme
// modal, the random reel and the home spotlight all use it, so they behave
// alike and share the saved volume. The bar always controls whatever element
// getMedia() returns right now, so it survives the player swapping its media.
//
// els holds the bar's parts. play, volumeWrap, volumeToggle and volume are
// required; playIcon, playLabel, progressWrap, progress, current, duration,
// volumeIcon and volumeValue are optional.
//
// options:
//   getMedia()     the <video> or <audio> to control, or null for none
//   isCompact()    true while the slider sits in a popover (a narrow bar): the
//                  speaker then opens the slider instead of muting
//   onInteract()   runs on every use of the bar, e.g. to show auto-hidden UI
//   seekAudio      show and drive the seek bar for audio as well as video
//   root           element given is-playing / is-muted classes, for CSS icons
//   onSync(media)  extra state for this player, run at the end of every sync
//
// Returns { sync, togglePlay }. Call sync whenever the media changes state.
// If a required part is missing from the page, the bar does nothing rather
// than stopping the script that creates it.
function createMediaControls(els, options) {
  const {
    getMedia,
    isCompact = () => false,
    onInteract = () => {},
    seekAudio = false,
    root = null,
    onSync = () => {},
  } = options;
  if (!els.play || !els.volumeWrap || !els.volumeToggle || !els.volume) {
    console.warn("Media control bar is missing parts of its markup; its controls are inactive");
    return { sync() {}, async togglePlay() {} };
  }
  // The element whose seek bar is being dragged, so playback updates do not
  // pull the thumb back under the pointer. It is tied to one element: when the
  // bar moves on to other media mid-drag, the new media is not held up.
  let seekingMedia = null;
  const isSeeking = () => seekingMedia !== null && seekingMedia === getMedia();
  // A drag can end without a "change" event: Chromium fires change only when
  // the value differs from where the drag began, and a cancelled touch or a
  // lost focus fires none. Any of these ends the drag.
  const endSeek = () => {
    if (seekingMedia === null) return;
    seekingMedia = null;
    sync();
  };

  const seekable = (media) => media instanceof HTMLVideoElement
    || (seekAudio && media instanceof HTMLAudioElement);
  const mediaDuration = (media) => (media && Number.isFinite(media.duration) ? media.duration : 0);
  // --media-fill lets a stylesheet paint the played or set part of a slider.
  const setFill = (input) => {
    const max = Number(input.max) || 100;
    input.style.setProperty("--media-fill", `${(Number(input.value) / max) * 100}%`);
  };

  function sync() {
    const media = getMedia();
    // A drag left open when the bar moved on to other media is over: forget
    // it, so returning to that same element later does not find it frozen.
    if (seekingMedia !== null && seekingMedia !== media) seekingMedia = null;
    const hasMedia = !!media;
    const paused = !hasMedia || media.paused;
    const muted = hasMedia && (media.muted || media.volume === 0);
    const level = hasMedia
      ? Math.round((media.muted ? 0 : media.volume) * 100)
      : Math.round(loadPreferredMediaVolume() * 100);
    const expanded = hasMedia && els.volumeWrap.classList.contains("is-expanded");

    els.play.disabled = !hasMedia;
    els.volumeToggle.disabled = !hasMedia;
    els.volume.disabled = !hasMedia;
    els.play.classList.toggle("hidden", !hasMedia);
    els.volumeWrap.classList.toggle("hidden", !hasMedia);
    els.progressWrap?.classList.toggle("hidden", !seekable(media));

    els.play.setAttribute("aria-label", paused ? "Play media" : "Pause media");
    if (els.play.hasAttribute("data-tooltip")) els.play.setAttribute("data-tooltip", paused ? "Play" : "Pause");
    if (els.playIcon) els.playIcon.innerHTML = paused ? "&#9654;" : "&#10074;&#10074;";
    if (els.playLabel) els.playLabel.textContent = paused ? "Play" : "Pause";

    els.volumeToggle.setAttribute(
      "aria-label",
      isCompact()
        ? (expanded ? "Close volume control" : "Adjust volume")
        : (muted ? "Unmute media" : "Mute media"),
    );
    els.volumeToggle.setAttribute("aria-expanded", String(expanded));
    if (els.volumeIcon) els.volumeIcon.innerHTML = muted ? "&#128263;" : "&#128266;";
    els.volume.value = String(level);
    setFill(els.volume);
    if (els.volumeValue) els.volumeValue.textContent = `${level}%`;

    const duration = mediaDuration(media);
    const currentTime = hasMedia && Number.isFinite(media.currentTime) ? media.currentTime : 0;
    if (els.current) els.current.textContent = formatMediaTime(currentTime);
    if (els.duration) els.duration.textContent = formatMediaTime(duration);
    if (els.progress) {
      if (!isSeeking()) {
        const value = duration > 0 ? Math.round((currentTime / duration) * MODAL_PROGRESS_SCALE_MAX) : 0;
        els.progress.value = String(Math.max(0, Math.min(MODAL_PROGRESS_SCALE_MAX, value)));
      }
      els.progress.disabled = duration <= 0;
      setFill(els.progress);
    }

    root?.classList.toggle("is-playing", !paused);
    root?.classList.toggle("is-muted", muted);
    onSync(media);
  }

  async function togglePlay() {
    const media = getMedia();
    if (!media) return;
    try {
      if (media.paused) {
        await media.play();
      } else {
        media.pause();
      }
    } catch (error) {
      console.error(error);
    }
    sync();
  }

  els.play.addEventListener("click", () => {
    onInteract();
    togglePlay();
  });

  els.volumeToggle.addEventListener("click", (event) => {
    onInteract();
    const media = getMedia();
    if (!media) return;
    if (isCompact()) {
      event.preventDefault();
      els.volumeWrap.classList.toggle("is-expanded");
      sync();
      return;
    }
    // A deliberate mute or unmute replaces any automatic mute, and unmuting a
    // player whose volume was dragged to zero brings back the default level
    // rather than leaving it silent.
    delete media.dataset.autoMuted;
    media.muted = !media.muted;
    if (!media.muted && media.volume === 0) media.volume = DEFAULT_MEDIA_VOLUME;
    syncStoredMediaVolumeFromElement(media);
    sync();
  });

  els.volume.addEventListener("input", () => {
    onInteract();
    const media = getMedia();
    if (!media) return;
    const volume = Number(els.volume.value) / 100;
    delete media.dataset.autoMuted;
    media.volume = volume;
    media.muted = volume === 0;
    syncStoredMediaVolumeFromElement(media);
    sync();
  });

  els.progress?.addEventListener("input", () => {
    onInteract();
    const media = getMedia();
    if (!seekable(media)) return;
    const duration = mediaDuration(media);
    if (duration <= 0) return;
    seekingMedia = media;
    const ratio = Number(els.progress.value) / MODAL_PROGRESS_SCALE_MAX;
    media.currentTime = Math.max(0, Math.min(duration, duration * ratio));
    sync();
  });
  for (const type of ["change", "pointerup", "pointercancel", "lostpointercapture", "blur"]) {
    els.progress?.addEventListener(type, endSeek);
  }

  return { sync, togglePlay };
}

function formatMediaTime(seconds) {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return "0:00";
  }

  const totalSeconds = Math.floor(seconds);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const remainingSeconds = totalSeconds % 60;

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, "0")}:${String(remainingSeconds).padStart(2, "0")}`;
  }

  return `${minutes}:${String(remainingSeconds).padStart(2, "0")}`;
}
