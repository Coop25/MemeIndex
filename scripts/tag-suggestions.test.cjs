// Run with: node --test scripts/tag-suggestions.test.cjs
const assert = require("node:assert/strict");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { test } = require("node:test");
const vm = require("node:vm");
const source = readFileSync(resolve(__dirname, "../static/app.js"), "utf8");

function functionSource(name) {
  const start = source.indexOf(`function ${name}(`);
  assert.notEqual(start, -1);
  return source.slice(start, source.indexOf("\n}", start) + 2);
}

class Element {
  constructor(id = "") {
    this.id = id;
    this.value = "";
    this.attributes = {};
    this.children = [];
    this.listeners = {};
    this.classes = new Set();
    this.classList = {
      add: (name) => this.classes.add(name),
      remove: (name) => this.classes.delete(name),
      contains: (name) => this.classes.has(name),
      toggle: (name, on) => on ? this.classes.add(name) : this.classes.delete(name),
    };
  }
  setAttribute(name, value) { this.attributes[name] = value; }
  getAttribute(name) { return this.attributes[name]; }
  hasAttribute(name) { return name in this.attributes; }
  removeAttribute(name) { delete this.attributes[name]; }
  addEventListener(name, handler) { this.listeners[name] = handler; }
  replaceChildren() { this.children = []; }
  appendChild(child) { this.children.push(child); }
  querySelectorAll() { return this.children.filter((child) => child.className === "tag-suggestion"); }
  scrollIntoView() { this.scrolled = true; }
  focus() { this.focused = true; }
}

const editors = [
  ["uploadTagsInput", "uploadTagSuggestions", "uploadSuggestionState", "activeUploadSuggestionIndex", "renderUploadTagSuggestions", "addUploadTag"],
  ["linkUploadTagsInput", "linkUploadTagSuggestions", "linkUploadSuggestionState", "activeLinkUploadSuggestionIndex", "renderLinkUploadTagSuggestions", "addLinkUploadTag"],
  ["modalTagsInput", "modalTagSuggestions", "modalSuggestionState", "activeSuggestionIndex", "renderTagSuggestions", "addModalTag"],
  ["tagSearchInput", "tagSearchSuggestions", "topTagSuggestionState", "activeTopTagSuggestionIndex", "renderTopTagSuggestions", "applyTagSearch"],
];

function setup(editor = editors[0], tags = ["food", "football", "footage"]) {
  const [inputName, listName, stateName, indexName, renderName, addName] = editor;
  const input = new Element(inputName);
  const list = new Element(listName);
  input.value = "foo";
  const selected = [];
  const context = vm.createContext({
    document: { activeElement: input, createElement: () => new Element() },
    window: { clearTimeout() {} }, topTagSearchDebounce: null,
    canAddTags: () => true, canRemoveTags: () => true,
    uploadTagState: [], linkUploadTagState: [], modalTagState: [],
    [inputName]: input, [listName]: list, [stateName]: tags, [indexName]: -1,
    [addName]: async (tag) => { selected.push(tag); },
  });
  for (const name of ["escapeHTML", "highlightTagSuggestion", "dismissTagSuggestions", "navigateTagSuggestions", "renderTagSuggestionList", renderName]) {
    vm.runInContext(functionSource(name), context);
  }
  const marker = `${inputName}${inputName === "linkUploadTagsInput" ? "?." : "."}addEventListener("keydown"`;
  const start = source.indexOf(marker);
  assert.notEqual(start, -1);
  vm.runInContext(source.slice(start, source.indexOf("\n});", start) + 4), context);
  context[renderName]();
  const key = async (value, options = {}) => {
    const event = { key: value, prevented: false, stopped: false,
      preventDefault() { this.prevented = true; }, stopPropagation() { this.stopped = true; }, ...options };
    await input.listeners.keydown(event);
    return event;
  };
  return { input, list, selected, context, key, stateName, indexName, renderName };
}

for (const editor of editors) {
  test(`${editor[0]}: Tab highlights each suggestion and Enter selects it`, async () => {
    const fixture = setup(editor);
    const { input, list, selected, key } = fixture;
    assert.equal(input.getAttribute("aria-expanded"), "true");
    assert.equal((await key("Tab")).prevented, true);
    assert.equal(selected.length, 0, "Tab must not add the typed fragment");
    assert.equal(input.getAttribute("aria-activedescendant"), `${list.id}-option-0`);
    await key("Tab");
    assert.equal(list.children[1].getAttribute("aria-selected"), "true");
    assert.equal(list.children[0].getAttribute("aria-selected"), "false");
    assert.equal(list.children[1].scrolled, true);
    await key("Enter");
    assert.deepEqual(selected, ["football"]);
  });
}

test("Tab wraps; Shift+Tab and arrow keys navigate backwards and forwards", async () => {
  const { key, context, indexName } = setup();
  await key("Tab", { shiftKey: true });
  assert.equal(context[indexName], 2);
  await key("Tab");
  assert.equal(context[indexName], 0);
  await key("ArrowUp");
  assert.equal(context[indexName], 2);
  await key("ArrowDown");
  assert.equal(context[indexName], 0);
});

test("mouse highlighting shares selection with Enter; clicks also select", async () => {
  const { input, list, selected, key } = setup();
  await key("Tab");
  list.children[2].listeners.pointermove();
  assert.equal(list.children[2].classList.contains("is-active"), true);
  await key("Enter");
  list.children[0].listeners.click();
  assert.deepEqual(selected, ["footage", "food"]);
  assert.equal(input.focused, true);
});

test("Escape closes only the suggestions, then Tab can leave without adding a tag", async () => {
  const { input, list, key, selected } = setup();
  await key("Tab");
  const escape = await key("Escape");
  assert.equal(escape.prevented && escape.stopped, true);
  assert.equal(list.classList.contains("hidden"), true);
  assert.equal(input.getAttribute("aria-expanded"), "false");
  assert.equal(input.hasAttribute("aria-activedescendant"), false);
  assert.equal((await key("Tab")).prevented, false);
  assert.deepEqual(selected, []);
});

test("no matches preserve Tab, and Enter still creates a new tag", async () => {
  const { key, selected } = setup(editors[0], []);
  assert.equal((await key("Tab")).prevented, false);
  await key("Enter");
  assert.deepEqual(selected, ["foo"]);
});

test("composition and browser shortcuts do not select suggestions", async () => {
  const { key, selected } = setup();
  assert.equal((await key("Enter", { isComposing: true })).prevented, false);
  assert.equal((await key("Tab", { ctrlKey: true })).prevented, false);
  assert.deepEqual(selected, []);
});

test("blur dismisses suggestions and late results do not reopen them", async () => {
  const { input, list, key, context, renderName } = setup();
  await key("Tab");
  input.listeners.blur();
  context.document.activeElement = null;
  context[renderName]();
  assert.equal(list.classList.contains("hidden"), true);
  assert.equal(input.hasAttribute("aria-activedescendant"), false);
});

test("suggestion labels escape HTML and keep options out of the normal tab order", () => {
  const { list } = setup(editors[0], ['<img src=x onerror="alert(1)">']);
  assert.doesNotMatch(list.children[0].innerHTML, /<img/);
  assert.match(list.children[0].innerHTML, /&lt;img/);
  assert.equal(list.children[0].tabIndex, -1);
});
