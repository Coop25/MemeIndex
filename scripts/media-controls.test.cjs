// Run with: node --test scripts/media-controls.test.cjs
// createMediaControls is the control bar shared by the meme modal, the random
// reel and the home spotlight.
const assert = require("node:assert/strict");
const { test } = require("node:test");
const vm = require("node:vm");
const { readAppSource, extractFunction } = require("./app-source.cjs");

const source = readAppSource();

class FakeClassList {
  constructor() { this.names = new Set(); }
  toggle(name, force) {
    const on = force === undefined ? !this.names.has(name) : !!force;
    if (on) this.names.add(name); else this.names.delete(name);
    return on;
  }
  contains(name) { return this.names.has(name); }
  add(name) { this.names.add(name); }
  remove(name) { this.names.delete(name); }
}

class FakeElement {
  constructor(attributes = {}) {
    this.classList = new FakeClassList();
    this.attributes = new Map(Object.entries(attributes));
    this.listeners = {};
    this.style = { props: {}, setProperty(name, value) { this.props[name] = value; } };
    this.disabled = false;
    this.value = "";
    this.max = attributes.max || "";
    this.innerHTML = "";
    this.textContent = "";
  }
  setAttribute(name, value) { this.attributes.set(name, String(value)); }
  getAttribute(name) { return this.attributes.get(name) ?? null; }
  hasAttribute(name) { return this.attributes.has(name); }
  addEventListener(type, handler) { (this.listeners[type] ||= []).push(handler); }
  fire(type, event = {}) {
    return Promise.all((this.listeners[type] || []).map((handler) => handler({ preventDefault() {}, ...event })));
  }
}

class FakeMedia extends FakeElement {
  constructor() {
    super();
    this.paused = true;
    this.muted = false;
    this.volume = 0.5;
    this.duration = 100;
    this.currentTime = 0;
    this.dataset = {};
  }
  async play() { this.paused = false; }
  pause() { this.paused = true; }
}
class FakeVideo extends FakeMedia {}
class FakeAudio extends FakeMedia {}

function setup({ media = new FakeVideo(), compact = false, seekAudio = false, storedVolume = 0.25 } = {}) {
  const saved = [];
  const context = vm.createContext({
    console,
    HTMLMediaElement: FakeMedia,
    HTMLVideoElement: FakeVideo,
    HTMLAudioElement: FakeAudio,
    MODAL_PROGRESS_SCALE_MAX: 1000,
    DEFAULT_MEDIA_VOLUME: 0.1,
    loadPreferredMediaVolume: () => storedVolume,
    syncStoredMediaVolumeFromElement: (element) => saved.push(element.muted ? 0 : element.volume),
  });
  vm.runInContext(extractFunction(source, "formatMediaTime"), context);
  vm.runInContext(extractFunction(source, "createMediaControls"), context);
  const els = {
    play: new FakeElement({ "data-tooltip": "Pause" }),
    playIcon: new FakeElement(),
    playLabel: new FakeElement(),
    progressWrap: new FakeElement(),
    progress: new FakeElement({ max: "1000" }),
    current: new FakeElement(),
    duration: new FakeElement(),
    volumeWrap: new FakeElement(),
    volumeToggle: new FakeElement(),
    volumeIcon: new FakeElement(),
    volume: new FakeElement({ max: "100" }),
    volumeValue: new FakeElement(),
  };
  const state = { media, compact, interactions: 0, synced: [] };
  const root = new FakeElement();
  const controls = context.createMediaControls(els, {
    getMedia: () => state.media,
    isCompact: () => state.compact,
    onInteract: () => { state.interactions += 1; },
    seekAudio,
    root,
    onSync: (current) => state.synced.push(current),
  });
  return { els, controls, state, saved, root };
}

test("with no media the bar is disabled, hidden and shows the saved volume", () => {
  const { els, controls, state } = setup({ media: null });
  controls.sync();
  assert.equal(els.play.disabled, true);
  assert.equal(els.volume.disabled, true);
  assert.ok(els.play.classList.contains("hidden"));
  assert.ok(els.volumeWrap.classList.contains("hidden"));
  assert.ok(els.progressWrap.classList.contains("hidden"));
  assert.equal(els.volume.value, "25");
  assert.equal(els.volumeValue.textContent, "25%");
  assert.equal(els.current.textContent, "0:00");
  assert.deepEqual(state.synced, [null]);
});

test("sync reflects a playing video's time, volume and play state", () => {
  const media = new FakeVideo();
  media.paused = false;
  media.currentTime = 25;
  media.duration = 100;
  media.volume = 0.4;
  const { els, controls, root } = setup({ media });
  controls.sync();
  assert.equal(els.play.getAttribute("aria-label"), "Pause media");
  assert.equal(els.play.getAttribute("data-tooltip"), "Pause");
  assert.equal(els.playLabel.textContent, "Pause");
  assert.equal(els.progress.value, "250");
  assert.equal(els.progress.style.props["--media-fill"], "25%");
  assert.equal(els.current.textContent, "0:25");
  assert.equal(els.duration.textContent, "1:40");
  assert.equal(els.volume.value, "40");
  assert.equal(els.volumeToggle.getAttribute("aria-label"), "Mute media");
  assert.ok(root.classList.contains("is-playing"));
  assert.ok(!root.classList.contains("is-muted"));
  assert.ok(!els.progressWrap.classList.contains("hidden"));
});

test("the play button toggles playback and counts as an interaction", async () => {
  const { els, state } = setup();
  await els.play.fire("click");
  assert.equal(state.media.paused, false);
  assert.equal(state.interactions, 1);
  await els.play.fire("click");
  assert.equal(state.media.paused, true);
});

test("the volume slider sets, saves and mutes at zero", async () => {
  const { els, state, saved } = setup();
  els.volume.value = "60";
  await els.volume.fire("input");
  assert.equal(state.media.volume, 0.6);
  assert.equal(state.media.muted, false);
  els.volume.value = "0";
  await els.volume.fire("input");
  assert.equal(state.media.muted, true);
  assert.deepEqual(saved, [0.6, 0]);
});

test("unmuting a player dragged to zero brings back the default volume", async () => {
  const media = new FakeVideo();
  media.volume = 0;
  media.muted = true;
  media.dataset.autoMuted = "true";
  const { els } = setup({ media });
  await els.volumeToggle.fire("click");
  assert.equal(media.muted, false);
  assert.equal(media.volume, 0.1);
  assert.equal(media.dataset.autoMuted, undefined);
});

test("on a compact bar the speaker opens the slider instead of muting", async () => {
  const { els, state } = setup({ compact: true });
  await els.volumeToggle.fire("click");
  assert.equal(state.media.muted, false);
  assert.ok(els.volumeWrap.classList.contains("is-expanded"));
  assert.equal(els.volumeToggle.getAttribute("aria-expanded"), "true");
  assert.equal(els.volumeToggle.getAttribute("aria-label"), "Close volume control");
});

test("dragging the seek bar moves playback without the bar jumping back", async () => {
  const { els, controls, state } = setup();
  els.progress.value = "500";
  await els.progress.fire("input");
  assert.equal(state.media.currentTime, 50);
  // A playback tick mid-drag leaves the thumb where the pointer put it.
  state.media.currentTime = 10;
  els.progress.value = "520";
  controls.sync();
  assert.equal(els.progress.value, "520");
  await els.progress.fire("change");
  controls.sync();
  assert.equal(els.progress.value, "100");
});

for (const ending of ["pointerup", "pointercancel", "lostpointercapture", "blur"]) {
  test(`a drag that ends with ${ending} and no change event does not freeze the seek bar`, async () => {
    const { els, controls, state } = setup();
    els.progress.value = "500";
    await els.progress.fire("input");
    // Chromium sends no change when the thumb ends where it began.
    await els.progress.fire(ending);
    state.media.currentTime = 10;
    controls.sync();
    assert.equal(els.progress.value, "100");
  });
}

test("a drag left open on one item does not hold up the next one", async () => {
  const { els, controls, state } = setup();
  els.progress.value = "500";
  await els.progress.fire("input");
  // The reel swipes to another item mid-drag; no event ends the drag.
  const next = new FakeVideo();
  next.currentTime = 30;
  state.media = next;
  controls.sync();
  assert.equal(els.progress.value, "300");
});

test("coming back to an item whose drag was left open does not find it frozen", async () => {
  const { els, controls, state } = setup();
  const first = state.media;
  els.progress.value = "500";
  await els.progress.fire("input");
  state.media = new FakeVideo();
  controls.sync();
  // The reel steps back to the first item; no event ever ended its drag.
  state.media = first;
  first.currentTime = 20;
  controls.sync();
  assert.equal(els.progress.value, "200");
});

test("a bar missing required markup stays inactive instead of throwing", () => {
  const context = vm.createContext({ console: { warn() {} } });
  vm.runInContext(extractFunction(source, "createMediaControls"), context);
  const controls = context.createMediaControls({ play: null }, { getMedia: () => null });
  assert.doesNotThrow(() => controls.sync());
});

test("audio gets a seek bar only where the player asks for one", async () => {
  const plain = setup({ media: new FakeAudio() });
  plain.controls.sync();
  assert.ok(plain.els.progressWrap.classList.contains("hidden"));
  plain.els.progress.value = "500";
  await plain.els.progress.fire("input");
  assert.equal(plain.state.media.currentTime, 0);

  const seekable = setup({ media: new FakeAudio(), seekAudio: true });
  seekable.controls.sync();
  assert.ok(!seekable.els.progressWrap.classList.contains("hidden"));
  seekable.els.progress.value = "500";
  await seekable.els.progress.fire("input");
  assert.equal(seekable.state.media.currentTime, 50);
});
