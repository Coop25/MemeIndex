// The home dashboard: the spotlight player and queue, recent memes, and
// random picks.

// keepSpotlight leaves the featured random pick alone, for refreshes the user
// did not ask for (a favorite toggled, a new upload arriving in the background).
async function fetchVaultDashboard({ onlyIfNewestChanged = false, keepSpotlight = false } = {}) {
	if (onlyIfNewestChanged) {
		// The background poll only needs to know whether something new arrived.
		// Ask for the newest id alone and skip the full dashboard when it is the
		// one already on screen.
		const probe = await fetch("/api/dashboard?probe=newest", { cache: "no-store" });
		if ((probe.status === 401 || probe.status === 403) && !(await expectAuthorized(probe, "Failed to load dashboard."))) return;
		// A background check that fails for any other reason just tries again
		// on the next tick; it must not interrupt the user with an error.
		if (!probe.ok) return false;
		const probed = await probe.json();
		const renderedNewestID = String(dashboardRecent?.querySelector(".meme-card")?.dataset.memeId || "");
		if (String(probed?.newest_id || "") === renderedNewestID) return false;
	}
	const response = await fetch("/api/dashboard", { cache: "no-store" });
	if (!(await expectAuthorized(response, "Failed to load dashboard."))) return;
	const dashboard = await response.json();
	if (onlyIfNewestChanged) {
		const incomingNewestID = String(dashboard.recent_items?.[0]?.id || "");
		const renderedNewestID = String(dashboardRecent?.querySelector(".meme-card")?.dataset.memeId || "");
		if (incomingNewestID === renderedNewestID) return false;
	}
	state.library.counts = dashboard.counts || state.library.counts;
	renderSidebarCounts();
	const totalItems = Number(dashboard.total_items || 0);
	const favoriteCount = Number(dashboard.favorites || 0);
	const topTags = Array.isArray(dashboard.top_tags) ? dashboard.top_tags : [];
	const tiles = [
		["items", "Total memes", totalItems.toLocaleString(), () => navigateToView("library")],
		["tags", "Tags", Number(dashboard.tag_count || 0).toLocaleString(), topTags.length ? () => dashboardTagsSection?.scrollIntoView({ behavior: "smooth", block: "start" }) : null],
		["storage", "Storage used", formatSize(Number(dashboard.storage_bytes || 0)), null],
		["favorites", "Favorites", favoriteCount.toLocaleString(), () => navigateToView("favorites")],
	];
	// The slim strip across the top of Home; the ones with somewhere to go
	// are buttons.
	dashboardStats.replaceChildren(...tiles.map(([kind, label, value, action]) => {
		const tile = document.createElement(action ? "button" : "div");
		if (action) {
			tile.type = "button";
			tile.addEventListener("click", action);
		}
		tile.className = "dashboard-stat-card";
		tile.dataset.icon = kind;
		tile.innerHTML = `<span></span><strong></strong>`;
		tile.querySelector("span").textContent = label;
		tile.querySelector("strong").textContent = value;
		return tile;
	}));
	if (dashboardRecentAll) dashboardRecentAll.textContent = totalItems > 1 ? `View all ${totalItems.toLocaleString()} items` : "View all items";
	if (dashboardFavoritesAll) dashboardFavoritesAll.textContent = favoriteCount > 1 ? `View all ${favoriteCount.toLocaleString()} favorites` : "View favorites";
	renderDashboardMemeCollection(
		dashboardRecent,
		dashboard.recent_items,
		"Your newest items will appear here.",
	);
	renderDashboardMemeCollection(
		dashboardFavorites,
		dashboard.favorite_items,
		"Favorite a meme and it will appear here.",
	);
	// A kept spotlight is left exactly as it is, so a video playing in it is
	// not restarted; edits to its item reach it through syncMemeCardElements.
	if (!keepSpotlight || !hasSpotlightItems()) setSpotlightItems(dashboard.random_items);
	dashboardStorageLabel.textContent = formatSize(Number(dashboard.storage_bytes || 0));
	dashboardStorageBar.style.width = dashboard.storage_bytes > 0 ? "100%" : "0%";
	// Tag size follows how often the tag is used, on a log scale so one very
	// popular tag does not flatten all the others.
	const tagCounts = topTags.map((tag) => Math.log(Math.max(1, Number(tag.count) || 1)));
	const minWeight = Math.min(...tagCounts);
	const weightSpan = Math.max(...tagCounts) - minWeight;
	dashboardTags?.replaceChildren(...topTags.map((tag, index) => {
		const button = document.createElement("button");
		button.type = "button";
		button.className = "home-tag";
		const weight = weightSpan > 0 ? (tagCounts[index] - minWeight) / weightSpan : 0.5;
		button.style.setProperty("--tag-weight", weight.toFixed(3));
		button.innerHTML = `<span></span><small></small>`;
		button.querySelector("span").textContent = tag.name;
		button.querySelector("small").textContent = Number(tag.count || 0).toLocaleString();
		button.title = `${tag.count} item${tag.count === 1 ? "" : "s"} tagged ${tag.name}`;
		button.addEventListener("click", () => navigateToTag(tag.name));
		return button;
	}));
	dashboardTagsSection?.classList.toggle("hidden", topTags.length === 0);
	return true;
}

// The featured random pick. `history` holds every item shown so far, with
// `pos` the one on show; `queue` is the Up next strip. Next walks forward
// through history and then pulls from the queue, which is topped up with fresh
// random items so it never runs dry; Previous walks back through history.
const SPOTLIGHT_QUEUE_SIZE = 5;
const SPOTLIGHT_HISTORY_LIMIT = 60;
// `batch` goes up on every Shuffle so a top-up still on its way for the old
// batch does not land in the new one.
const spotlight = { history: [], pos: -1, queue: [], fetching: null, exhausted: false, batch: 0 };

function loadSpotlightToggle(key) {
	try {
		return window.localStorage.getItem(key) !== "false";
	} catch {
		return true;
	}
}

function saveSpotlightToggle(key, value) {
	try {
		window.localStorage.setItem(key, value ? "true" : "false");
	} catch {
		// Not remembered in a private window; the checkbox still works.
	}
}

function spotlightAutoplayOn() {
	return Boolean(homeSpotlightAutoplay?.checked);
}

function spotlightLoopOn() {
	return Boolean(homeSpotlightLoop?.checked);
}

function currentSpotlightMeme() {
	const meme = spotlight.history[spotlight.pos];
	return meme ? getMemeById(meme.id) || meme : null;
}

function hasSpotlightItems() {
	return spotlight.history.length > 0;
}

// Items still ahead of the one on show: any forward history first (after
// stepping back), then the queue.
function upcomingSpotlightItems() {
	return spotlight.history.slice(spotlight.pos + 1).concat(spotlight.queue);
}

function setSpotlightItems(items) {
	const memes = (Array.isArray(items) ? items : []).map((meme) => upsertMemeInState(meme) || meme);
	spotlight.history = memes.slice(0, 1);
	spotlight.pos = memes.length ? 0 : -1;
	spotlight.queue = memes.slice(1);
	spotlight.exhausted = false;
	spotlight.batch += 1;
	spotlight.fetching = null;
	renderSpotlight();
	topUpSpotlightQueue();
}

// Fetches another batch of random items and appends the ones not already in
// the history or queue. A small vault runs out of unseen items, so once a
// fetch brings nothing new, repeats are let back in (just never the item on
// show or the one about to play).
async function topUpSpotlightQueue() {
	// A fetch already on its way is returned, so a caller can wait for it.
	if (spotlight.fetching) return spotlight.fetching;
	if (upcomingSpotlightItems().length >= SPOTLIGHT_QUEUE_SIZE || !hasSpotlightItems()) return;
	const batch = spotlight.batch;
	let fetching = null;
	fetching = (async () => {
		try {
			for (let attempt = 0; attempt < 3 && upcomingSpotlightItems().length < SPOTLIGHT_QUEUE_SIZE; attempt += 1) {
				const response = await fetch("/api/dashboard", { cache: "no-store" });
				if (!response.ok || batch !== spotlight.batch) return;
				const dashboard = await response.json();
				if (batch !== spotlight.batch) return;
				const fresh = Array.isArray(dashboard.random_items) ? dashboard.random_items : [];
				const known = new Set(spotlight.history.concat(spotlight.queue).map((meme) => meme.id));
				let added = fresh.filter((meme) => meme?.id && !known.has(meme.id));
				if (!added.length && spotlight.exhausted) {
					const avoid = new Set([currentSpotlightMeme()?.id, upcomingSpotlightItems().at(-1)?.id]);
					added = fresh.filter((meme) => meme?.id && !avoid.has(meme.id));
				}
				if (!added.length) {
					spotlight.exhausted = true;
					continue;
				}
				const room = SPOTLIGHT_QUEUE_SIZE - upcomingSpotlightItems().length;
				spotlight.queue.push(...added.slice(0, Math.max(0, room)).map((meme) => upsertMemeInState(meme) || meme));
			}
		} catch (error) {
			console.warn("Could not top up the random picks", error);
		} finally {
			if (spotlight.fetching === fetching) spotlight.fetching = null;
			renderSpotlightQueue();
		}
	})();
	spotlight.fetching = fetching;
	return fetching;
}

// Moves the spotlight `steps` items forward (1 for Next, more when an Up next
// thumbnail further along is picked).
function advanceSpotlight(steps = 1) {
	let moved = false;
	for (let step = 0; step < steps; step += 1) {
		if (spotlight.pos < spotlight.history.length - 1) {
			spotlight.pos += 1;
		} else if (spotlight.queue.length) {
			spotlight.history.push(spotlight.queue.shift());
			spotlight.pos += 1;
		} else {
			break;
		}
		moved = true;
	}
	// Forget the oldest items once the history gets long.
	const overflow = spotlight.history.length - SPOTLIGHT_HISTORY_LIMIT;
	if (overflow > 0 && spotlight.pos >= overflow) {
		spotlight.history.splice(0, overflow);
		spotlight.pos -= overflow;
	}
	return moved;
}

async function showNextSpotlight() {
	if (!advanceSpotlight(1)) {
		// The queue was empty (a fetch still on its way); wait for it once.
		await topUpSpotlightQueue();
		if (!advanceSpotlight(1)) return;
	}
	renderSpotlight();
	topUpSpotlightQueue();
}

function showPreviousSpotlight() {
	if (spotlight.pos <= 0) return;
	spotlight.pos -= 1;
	renderSpotlight();
}

function spotlightKindLabel(meme) {
	if (isVideoMeme(meme)) return "Video";
	if (isImageMeme(meme)) return "Image";
	if (isMP3Meme(meme)) return "Audio";
	return "File";
}

// The spotlight's headline: the first line of the item's notes when it has
// any, else its file name when that reads like a name. Machine-made names
// (ids, long digit runs) get a stock line instead; the full name stays in the
// tooltip.
function spotlightHeadline(meme) {
	const note = String(meme.notes || "").split("\n").map((line) => line.trim()).find(Boolean);
	if (note) return note;
	const name = String(meme.originalName || "").replace(/\.[a-z0-9]{1,5}$/i, "").trim();
	const digits = (name.match(/\d/g) || []).length;
	const readable = name && name.length <= 60 && digits / name.length < 0.4 && !/^[0-9a-f_-]{16,}$/i.test(name);
	return readable ? name : "Remember this one?";
}

function spotlightMediaElements() {
	return homeSpotlightStage ? [...homeSpotlightStage.querySelectorAll("video, audio")] : [];
}

function pauseSpotlightMedia() {
	spotlightMediaElements().forEach((media) => {
		if (!media.paused) media.pause();
	});
}

// The ResizeObserver watching the current spotlight control bar, if any.
let spotlightControlsObserver = null;

// Stops the spotlight's current player before renderSpotlight replaces it:
// pauses it, drops its source so the browser stops downloading a file nobody
// will watch, and stops watching its control bar's size. The old player keeps
// its listeners, so its controls also ignore events once it is off the stage.
function retireSpotlightMedia() {
	spotlightControlsObserver?.disconnect();
	spotlightControlsObserver = null;
	for (const media of spotlightMediaElements()) {
		media.pause();
		media.removeAttribute("src");
		media.load();
	}
	homeSpotlightStage?.classList.remove("is-playing");
}

// Gives a spotlight video or audio element the shared volume preference and
// keeps that preference in step when the viewer changes it.
function prepareSpotlightMedia(media) {
	applyDefaultMediaVolume(media);
	media.loop = spotlightLoopOn();
	// With Loop off and Autoplay on, a finished item hands over to the next.
	media.addEventListener("ended", () => {
		if (!spotlightAutoplayOn() || media.loop || !homeSpotlightStage.contains(media)) return;
		showNextSpotlight().catch((error) => console.warn("Could not show the next random pick", error));
	});
	media.addEventListener("volumechange", () => {
		// Muting for a page-load autoplay is the browser's rule, not the
		// viewer's choice, so it is not saved as their volume.
		if (media.dataset.autoMuted === "true") {
			if (media.muted) return;
			delete media.dataset.autoMuted;
		}
		syncStoredMediaVolumeFromElement(media);
	});
	// Only one spotlight item plays at a time, and nothing plays behind the
	// editor or reel.
	media.addEventListener("play", () => {
		if (media.dataset.autoMuted !== "true") applyDefaultMediaVolume(media);
		spotlightMediaElements().forEach((other) => {
			if (other !== media && !other.paused) other.pause();
		});
	});
}

// The spotlight's own control bar in place of the browser's built-in controls:
// play/pause, progress with times, a speaker button with a volume slider and
// percentage, and fullscreen. It runs on createMediaControls, like the meme
// modal and the reel, so it behaves the same and shares the saved volume.
function buildSpotlightControls(player) {
	const isVideo = player instanceof HTMLVideoElement;
	const bar = document.createElement("div");
	bar.className = "spot-controls";
	bar.innerHTML = `
		<button class="spot-control-button spot-play" type="button" aria-label="Play media">
			<svg viewBox="0 0 24 24" aria-hidden="true"><path class="spot-icon-play" d="M8 5.5v13l10.5-6.5z" /><path class="spot-icon-pause" d="M7 5h3.5v14H7zM13.5 5H17v14h-3.5z" /></svg>
		</button>
		<span class="spot-time spot-time-current">0:00</span>
		<input class="spot-progress" type="range" min="0" max="${MODAL_PROGRESS_SCALE_MAX}" value="0" step="1" aria-label="Playback progress">
		<span class="spot-time spot-time-duration">0:00</span>
		<div class="spot-volume">
			<button class="spot-control-button spot-mute" type="button" aria-label="Mute media" aria-controls="home-spotlight-volume" aria-expanded="false">
				<svg viewBox="0 0 24 24" aria-hidden="true"><path class="spot-icon-speaker" d="M4 9.5h3.5L12 5.5v13l-4.5-4H4z" /><path class="spot-icon-waves" d="M15.5 9a4.5 4.5 0 0 1 0 6M18 6.5a8 8 0 0 1 0 11" /><path class="spot-icon-off" d="M16 9.5l5 5M21 9.5l-5 5" /></svg>
			</button>
			<div class="spot-volume-pop" id="home-spotlight-volume">
				<input class="spot-volume-slider" type="range" min="0" max="100" step="1" aria-label="Volume">
				<output class="spot-volume-value">10%</output>
			</div>
		</div>
		${isVideo ? `<button class="spot-control-button spot-fullscreen" type="button" aria-label="Full screen">
			<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 9V4h5M15 4h5v5M20 15v5h-5M9 20H4v-5" /></svg>
		</button>` : ""}`;
	const volumeWrap = bar.querySelector(".spot-volume");
	const controls = createMediaControls({
		play: bar.querySelector(".spot-play"),
		progress: bar.querySelector(".spot-progress"),
		current: bar.querySelector(".spot-time-current"),
		duration: bar.querySelector(".spot-time-duration"),
		volumeWrap,
		volumeToggle: bar.querySelector(".spot-mute"),
		volume: bar.querySelector(".spot-volume-slider"),
		volumeValue: bar.querySelector(".spot-volume-value"),
	}, {
		getMedia: () => player,
		// A narrow bar (a phone, or a tall video on a wide screen) tucks the
		// volume slider into a popover so the progress bar keeps its room.
		isCompact: () => bar.classList.contains("is-compact"),
		seekAudio: true,
		root: bar,
		onSync() {
			// A replaced player's late events (its metadata arriving after
			// the viewer skipped ahead) must not change the new item's stage.
			if (!homeSpotlightStage.contains(player)) return;
			homeSpotlightStage.classList.toggle("is-playing", !player.paused);
		},
	});
	if (typeof ResizeObserver === "function") {
		// retireSpotlightMedia disconnects this when the bar is replaced, even
		// if the bar was never laid out (built while the home view was hidden).
		spotlightControlsObserver?.disconnect();
		spotlightControlsObserver = new ResizeObserver(() => {
			const compact = bar.clientWidth > 0 && bar.clientWidth < 420;
			bar.classList.toggle("is-compact", compact);
			if (!compact) volumeWrap.classList.remove("is-expanded");
			controls.sync();
		});
		spotlightControlsObserver.observe(bar);
	}

	bar.querySelector(".spot-fullscreen")?.addEventListener("click", () => {
		if (document.fullscreenElement) {
			document.exitFullscreen?.().catch(() => {});
		} else if (homeSpotlightStage.requestFullscreen) {
			homeSpotlightStage.requestFullscreen().catch(() => {});
		} else if (typeof player.webkitEnterFullscreen === "function") {
			player.webkitEnterFullscreen();
		}
	});
	for (const type of ["play", "pause", "timeupdate", "loadedmetadata", "durationchange", "volumechange"]) {
		player.addEventListener(type, controls.sync);
	}
	controls.sync();
	return bar;
}

function renderSpotlight() {
	if (!homeSpotlight) return;
	retireSpotlightMedia();
	const meme = currentSpotlightMeme();
	homeSpotlight.classList.toggle("is-empty", !meme);
	if (!meme) {
		homeSpotlightStage.replaceChildren();
		homeSpotlightBackdrop.removeAttribute("src");
		renderSpotlightQueue();
		return;
	}
	const stillSrc = meme.previewPath || (isImageMeme(meme) ? meme.filePath : "");
	if (stillSrc) homeSpotlightBackdrop.src = stillSrc;
	else homeSpotlightBackdrop.removeAttribute("src");

	// Videos get a real player with sound and controls; images show the
	// original rather than the small card thumbnail; audio gets its cover (or
	// icon) with a player along the bottom.
	const playable = isVideoMeme(meme) || isMP3Meme(meme);
	let media;
	let player = null;
	if (isVideoMeme(meme)) {
		media = document.createElement("video");
		media.className = "home-spotlight-player-video";
		media.src = meme.filePath;
		if (meme.previewPath) media.poster = meme.previewPath;
		media.playsInline = true;
		media.preload = "metadata";
		player = media;
	} else if (isImageMeme(meme)) {
		media = document.createElement("img");
		media.src = meme.filePath || meme.previewPath;
		media.alt = meme.originalName || "";
		media.decoding = "async";
	} else {
		media = buildPreview(meme);
		if (media instanceof HTMLImageElement) media.loading = "eager";
	}
	const children = [media];
	if (isMP3Meme(meme)) {
		player = document.createElement("audio");
		player.className = "home-spotlight-audio";
		player.src = meme.filePath;
		player.preload = "metadata";
		children.push(player);
	}
	if (player) {
		prepareSpotlightMedia(player);
		children.push(buildSpotlightControls(player));
	}
	homeSpotlightStage.replaceChildren(...children);
	homeSpotlightStage.classList.toggle("is-playable", playable);
	// A playable stage is a player, not a button: its clicks belong to the
	// controls, and Open details is in the actions below.
	if (playable) {
		homeSpotlightStage.removeAttribute("role");
		homeSpotlightStage.removeAttribute("tabindex");
		homeSpotlightStage.removeAttribute("aria-label");
	} else {
		homeSpotlightStage.setAttribute("role", "button");
		homeSpotlightStage.tabIndex = 0;
		homeSpotlightStage.setAttribute("aria-label", `Open ${meme.originalName || "item"} details`);
	}

	// Shape the stage to the picture: from the card's measured shape straight
	// away when there is one, then from the picture itself once it has loaded.
	const setStageRatio = (width, height) => {
		if (!width || !height || media.parentElement !== homeSpotlightStage) return;
		homeSpotlightStage.style.setProperty("--spot-ratio", Math.min(2.4, Math.max(0.45, width / height)).toFixed(4));
	};
	const knownRatio = previewRatioCache.get(meme.previewPath || meme.filePath);
	if (knownRatio) homeSpotlightStage.style.setProperty("--spot-ratio", String(knownRatio));
	else homeSpotlightStage.style.removeProperty("--spot-ratio");
	if (media instanceof HTMLImageElement) {
		if (media.complete && media.naturalWidth) setStageRatio(media.naturalWidth, media.naturalHeight);
		else media.addEventListener("load", () => setStageRatio(media.naturalWidth, media.naturalHeight), { once: true });
	} else if (media instanceof HTMLVideoElement) {
		media.addEventListener("loadedmetadata", () => setStageRatio(media.videoWidth, media.videoHeight), { once: true });
	}
	homeSpotlightStage.dataset.kind = spotlightKindLabel(meme).toLowerCase();

	// With Autoplay on, a new item starts by itself, with sound when the
	// browser allows it. On page load, or when one item hands over to the next
	// without a click, the browser may only allow it muted, so fall back to that.
	if (player && spotlightAutoplayOn() && document.body.classList.contains("home-mode")) {
		player.play().catch(() => {
			if (!player.isConnected) return;
			player.dataset.autoMuted = "true";
			player.muted = true;
			player.play().catch(() => {});
		});
	}

	renderSpotlightDetails();
	renderSpotlightQueue();
}

// Title, meta, tags and favorite: everything that can change while the item
// is on show without restarting its video.
function renderSpotlightDetails() {
	const meme = currentSpotlightMeme();
	if (!meme || !homeSpotlightTitle) return;
	homeSpotlightTitle.textContent = spotlightHeadline(meme);
	homeSpotlightTitle.title = meme.originalName || "";
	const added = meme.createdAt ? `added ${formatRelativeTime(meme.createdAt)}` : "";
	homeSpotlightMeta.textContent = [spotlightKindLabel(meme), formatSize(Number(meme.sizeBytes || 0)), added].filter(Boolean).join(" • ");
	const tags = Array.isArray(meme.tags) ? meme.tags : [];
	homeSpotlightTags.replaceChildren(...tags.slice(0, 8).map((tag) => {
		const chip = document.createElement("button");
		chip.type = "button";
		chip.className = "tag-chip";
		chip.textContent = tag;
		chip.addEventListener("click", () => navigateToTag(tag));
		return chip;
	}));
	if (!tags.length) {
		const none = document.createElement("span");
		none.className = "home-spotlight-untagged";
		none.textContent = "No tags yet";
		homeSpotlightTags.appendChild(none);
	}
	syncSpotlightFavorite();
}

function renderSpotlightQueue() {
	if (!homeSpotlightQueue) return;
	const upcoming = upcomingSpotlightItems().slice(0, SPOTLIGHT_QUEUE_SIZE);
	homeSpotlightQueue.replaceChildren(...upcoming.map((item, index) => {
		const queued = getMemeById(item.id) || item;
		const button = document.createElement("button");
		button.type = "button";
		button.className = "home-spotlight-queued";
		button.dataset.kind = spotlightKindLabel(queued).toLowerCase();
		button.title = queued.originalName || "";
		button.setAttribute("aria-label", `Show ${queued.originalName || "item"} in the spotlight`);
		const thumb = queued.previewPath || (isImageMeme(queued) ? queued.filePath : "");
		if (thumb) {
			const img = document.createElement("img");
			img.src = thumb;
			img.alt = "";
			img.loading = "lazy";
			button.appendChild(img);
		} else {
			button.textContent = pickIcon(queued.contentType);
		}
		button.addEventListener("click", () => {
			if (!advanceSpotlight(index + 1)) return;
			renderSpotlight();
			topUpSpotlightQueue();
		});
		return button;
	}));
	homeSpotlightNext.classList.toggle("hidden", upcoming.length === 0);
	if (homeSpotlightPrev) homeSpotlightPrev.disabled = spotlight.pos <= 0;
	if (homeSpotlightNextButton) homeSpotlightNextButton.disabled = upcoming.length === 0 && !spotlight.fetching;
	if (homeSpotlightPosition) {
		homeSpotlightPosition.textContent = hasSpotlightItems() ? `${spotlight.pos + 1} of ${spotlight.history.length}` : "";
	}
}

function syncSpotlightFavorite() {
	const meme = currentSpotlightMeme();
	if (!homeSpotlightFavorite || !meme) return;
	const favorite = Boolean(meme.favorite);
	homeSpotlightFavorite.classList.toggle("is-favorite", favorite);
	homeSpotlightFavorite.setAttribute("aria-pressed", favorite ? "true" : "false");
	homeSpotlightFavorite.querySelector("span").textContent = favorite ? "Favorited" : "Favorite";
	homeSpotlightFavorite.disabled = !canView();
	if (homeSpotlightShare) homeSpotlightShare.disabled = !canView();
}

function openSpotlightMeme() {
	const meme = currentSpotlightMeme();
	if (!meme) return;
	pauseSpotlightMedia();
	openModal(meme.id);
}

let homeDashboardRefreshInFlight = false;

async function refreshHomeDashboardForNewMemes() {
	if (homeDashboardRefreshInFlight || document.hidden || state.filters.view !== "home" || !canView()) return;
	homeDashboardRefreshInFlight = true;
	try {
		await fetchVaultDashboard({ onlyIfNewestChanged: true, keepSpotlight: true });
	} finally {
		homeDashboardRefreshInFlight = false;
	}
}

function renderDashboardMemeCollection(container, items, emptyMessage) {
	if (!container) return;
	container.replaceChildren();
	const memes = Array.isArray(items) ? items : [];
	if (!memes.length) {
		const empty = document.createElement("p");
		empty.className = "dashboard-empty";
		empty.textContent = emptyMessage;
		container.appendChild(empty);
		return;
	}
	memes.forEach((meme) => {
		// Dashboard items share the library state and card component so favorites,
		// permissions, and the editor modal behave identically everywhere.
		upsertMemeInState(meme);
		const card = buildMemeCardElement(meme);
		card.classList.add("dashboard-meme-card");
		card.title = `Open ${meme.originalName || "meme"} in the editor`;
		if (dashboardRows.includes(container)) {
			// Only a row's worth of these is shown, but which ones depends on
			// every card's shape, so load them all up front.
			const media = card._previewFrame?.firstElementChild;
			if (media instanceof HTMLImageElement) media.loading = "eager";
		}
		container.appendChild(card);
		layoutCardTags(card._tagList, card._tags, 1);
	});
	if (dashboardRows.includes(container)) scheduleDashboardRecentLayout();
}

// Preview shapes already measured, keyed by source, so a re-render lays cards
// out at their final widths instead of reflowing as images load.
const previewRatioCache = new Map();
const PREVIEW_RATIO_MIN = 0.5;
const PREVIEW_RATIO_MAX = 2.2;

// Publishes the preview's width/height ratio as --preview-ratio on the card so
// the stylesheet can size the box to the picture. Extreme shapes are clamped
// (and letterboxed inside the box) to keep one item from dominating a row.
function fitCardToPreviewRatio(card) {
	const media = card?._previewFrame?.firstElementChild;
	if (!card) return;
	card._previewRatio = 1;
	if (!(media instanceof HTMLImageElement || media instanceof HTMLVideoElement)) return;
	const isVideo = media instanceof HTMLVideoElement;
	const key = media.getAttribute("src") || "";
	const apply = (ratio) => {
		card._previewRatio = ratio;
		card.style.setProperty("--preview-ratio", String(ratio));
		if (dashboardRows.includes(card.parentElement)) scheduleDashboardRecentLayout();
	};
	const measure = () => {
		const width = isVideo ? media.videoWidth : media.naturalWidth;
		const height = isVideo ? media.videoHeight : media.naturalHeight;
		if (!width || !height) return false;
		const ratio = Math.min(PREVIEW_RATIO_MAX, Math.max(PREVIEW_RATIO_MIN, width / height));
		previewRatioCache.set(key, ratio);
		apply(ratio);
		return true;
	};
	if (previewRatioCache.has(key)) apply(previewRatioCache.get(key));
	if (!measure()) {
		media.addEventListener(isVideo ? "loadedmetadata" : "load", measure, { once: true });
	}
}

// Picks how many leading items of a single justified row to show, and the row
// height that makes them span `width` exactly. It takes whichever of "one item
// fewer, slightly taller" and "one more, slightly shorter" lands closer to
// `targetHeight`. When there are too few items to span the row it shows them
// all and stops growing at `maxHeight`.
function pickJustifiedRow(ratios, width, gap, targetHeight, maxHeight) {
	const heightFor = (count, ratioSum) => (width - gap * (count - 1)) / ratioSum;
	let ratioSum = 0;
	for (let index = 0; index < ratios.length; index += 1) {
		const previousSum = ratioSum;
		ratioSum += ratios[index];
		const height = heightFor(index + 1, ratioSum);
		if (height > targetHeight) continue;
		if (index > 0) {
			const tallerHeight = heightFor(index, previousSum);
			if (tallerHeight <= maxHeight && tallerHeight / targetHeight < targetHeight / height) {
				return { count: index, height: tallerHeight };
			}
		}
		return { count: index + 1, height };
	}
	if (!ratios.length || ratioSum <= 0) return { count: 0, height: targetHeight };
	return { count: ratios.length, height: Math.min(maxHeight, heightFor(ratios.length, ratioSum)) };
}

const DASHBOARD_RECENT_MOBILE_COUNT = 6;
let dashboardRecentLayoutFrame = 0;

function scheduleDashboardRecentLayout() {
	if (!dashboardRows.length || dashboardRecentLayoutFrame) return;
	dashboardRecentLayoutFrame = requestAnimationFrame(() => {
		dashboardRecentLayoutFrame = 0;
		dashboardRows.forEach(layoutDashboardRow);
	});
}

// Shows as many of a row's leading items as fill it edge to edge at the
// current window width, and hides the rest. Favorites sit a little shorter
// than Recently added so the two rows do not read as one block.
function layoutDashboardRow(rowElement) {
	const cards = Array.from(rowElement?.querySelectorAll(".meme-card") || []);
	const width = rowElement?.clientWidth || 0;
	if (!cards.length || !width) return;
	if (window.matchMedia("(max-width: 760px)").matches) {
		// Phones keep a short sideways-scrolling row at the stylesheet's height.
		cards.forEach((card, index) => { card.hidden = index >= DASHBOARD_RECENT_MOBILE_COUNT; });
		rowElement.style.removeProperty("--recent-row-height");
		return;
	}
	const gap = parseFloat(getComputedStyle(rowElement).columnGap) || 0;
	const targetHeight = rowElement === dashboardFavorites
		? Math.min(230, Math.max(170, window.innerWidth * 0.135))
		: Math.min(290, Math.max(200, window.innerWidth * 0.17));
	// Each card is its preview plus its own borders, so fold that fixed extra
	// width into the gap (and the one border pair no gap accounts for).
	const cardStyle = getComputedStyle(cards[0]);
	const chrome = (parseFloat(cardStyle.borderLeftWidth) || 0) + (parseFloat(cardStyle.borderRightWidth) || 0);
	const row = pickJustifiedRow(cards.map((card) => card._previewRatio || 1), width - chrome, gap + chrome, targetHeight, targetHeight * 1.5);
	cards.forEach((card, index) => { card.hidden = index >= row.count; });
	// Shave a hair off so sub-pixel rounding can never push the last card out.
	rowElement.style.setProperty("--recent-row-height", `${Math.max(1, row.height - 0.25).toFixed(2)}px`);
}

async function refreshDashboardRandomItems() {
	if (!dashboardRandomRefresh || !homeSpotlight) return;
	dashboardRandomRefresh.disabled = true;
	dashboardRandomRefresh.classList.add("is-loading");
	try {
		const response = await fetch("/api/dashboard", { cache: "no-store" });
		if (!(await expectAuthorized(response, "Could not shuffle random picks."))) return;
		const dashboard = await response.json();
		setSpotlightItems(dashboard.random_items);
	} finally {
		dashboardRandomRefresh.disabled = false;
		dashboardRandomRefresh.classList.remove("is-loading");
	}
}

function checkHomeDashboardForNewMemes() {
  refreshHomeDashboardForNewMemes().catch((error) => console.error(error));
}
