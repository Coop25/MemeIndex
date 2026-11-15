// @ts-check
// Share links and deep links: copying share URLs, the share target, and
// opening a linked meme or audiobook.

// Prefers the server-resolved public origin (MEMEINDEX_PUBLIC_BASE_URL) over
// window.location.origin, so a link copied while connected through the
// direct-LAN listener still reads as the public domain, not a local IP.
function shareOrigin() {
  return state.auth?.publicBaseURL || window.location.origin;
}

async function shareAudiobookSeries(series) {
  const url = `${shareOrigin()}/?view=audiobooks&series=${encodeURIComponent(series.id)}`;
  try {
    await copyShareText(url);
    showToast(`Link to "${series.name}" copied.`, "success", { title: "Audiobook Series" });
  } catch (error) {
    console.error(error);
    showToast("Could not copy the series link.", "error", { title: "Audiobook Series" });
  }
}

// Same idea as shareAudiobookSeries, one level down: a deep link that opens
// straight to this one book's player. Whoever opens it still needs their own
// account and view access - this isn't a public, unauthenticated link like a
// meme share.
async function shareAudiobookItem(item) {
  const url = `${shareOrigin()}/?view=audiobooks&book=${encodeURIComponent(item.id)}`;
  try {
    await copyShareText(url);
    showToast(`Link to "${audiobookDisplayTitle(item)}" copied.`, "success", { title: "Audiobooks" });
  } catch (error) {
    console.error(error);
    showToast("Could not copy the link.", "error", { title: "Audiobooks" });
  }
}

async function copyShareText(value) {
  const shareText = String(value || "").trim();
  if (!shareText) throw new Error("share URL missing");
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(shareText);
      return;
    } catch (error) {
      console.warn("Clipboard API unavailable; trying browser copy fallback.", error);
    }
  }
  const input = document.createElement("textarea");
  input.value = shareText;
  input.setAttribute("readonly", "");
  input.style.position = "fixed";
  input.style.opacity = "0";
  document.body.appendChild(input);
  input.focus();
  input.select();
  input.setSelectionRange?.(0, input.value.length);
  let copied = false;
  try {
    copied = document.execCommand("copy");
  } finally {
    input.remove();
  }
  if (!copied) throw new Error("clipboard unavailable");
}

function showShareCopiedFeedback(button) {
  if (!button) return;
  const label = button.querySelector(".modal-action-label, .random-reel-nav-label, .card-copy-label, .home-spotlight-action-label");
  const originalAriaLabel = button.getAttribute("aria-label");
  const originalTooltip = button.getAttribute("data-tooltip");
  const originalLabel = label?.textContent || "Share";
  button.classList.add("share-copied");
  button.setAttribute("aria-label", "Share link copied");
  button.setAttribute("data-tooltip", "Copied to Clipboard");
  if (label) label.textContent = "Copied!";

  const dialog = button.closest("dialog");
  if (dialog) {
    dialog.querySelector(".share-copy-confirmation")?.remove();
    const confirmation = document.createElement("div");
    confirmation.className = "share-copy-confirmation";
    confirmation.setAttribute("role", "status");
    confirmation.setAttribute("aria-live", "assertive");
    confirmation.innerHTML = `<span aria-hidden="true">&#10003;</span><strong>Share link copied</strong><small>Ready to paste anywhere</small>`;
    dialog.appendChild(confirmation);
    window.setTimeout(() => confirmation.remove(), 2600);
  }

  window.setTimeout(() => {
    button.classList.remove("share-copied");
    if (originalAriaLabel) button.setAttribute("aria-label", originalAriaLabel);
    else button.removeAttribute("aria-label");
    if (originalTooltip) button.setAttribute("data-tooltip", originalTooltip);
    else button.removeAttribute("data-tooltip");
    if (label) label.textContent = originalLabel;
  }, 2400);
}

async function shareMeme(id, button) {
  if (!canView() || !id || button?.disabled) return false;
  if (button) button.disabled = true;
  try {
    const response = await fetch(`/api/memes/${encodeURIComponent(id)}/share`, { method: "POST" });
    if (!(await expectAuthorized(response, "Could not create a share link."))) return false;
    const payload = await response.json();
    const shareURL = String(payload.page_url || payload.url || "");
    if (!shareURL) throw new Error("share URL missing");
    await copyShareText(shareURL);
    showShareCopiedFeedback(button);
    showToast("Share link copied. It works for one year.", "success", { title: "Share", duration: 3200 });
    return true;
  } catch (error) {
    console.error(error);
    showToast("The share link could not be copied.", "error", { title: "Share" });
    return false;
  } finally {
    if (button) button.disabled = !canView();
  }
}

function deepLinkedMemeID() {
  const match = window.location.pathname.match(/^\/m\/([^/]+)$/);
  if (!match) return "";
  try {
    return decodeURIComponent(match[1]);
  } catch (error) {
    return "";
  }
}

function clearMemeDeepLinkURL() {
  if (!deepLinkedMemeID()) return;
  window.history.replaceState(window.history.state || {}, "", "/");
}

// consumeShareTargetResult reads the ?shared= status the server sets after an
// operating-system share sheet hands content to the installed PWA, shows a
// matching toast, and strips the marker so a reload does not repeat it.
function consumeShareTargetResult() {
  const params = new URLSearchParams(window.location.search);
  const status = params.get("shared");
  if (!status) return;

  const count = Math.max(0, parseInt(params.get("n") || "0", 10) || 0);
  const messages = {
    ok: [`Added ${count || 1} shared ${count === 1 || !count ? "item" : "items"} to your archive.`, "success", "Shared to MemeIndex"],
    dup: ["That item is already in your archive.", "info", "Shared to MemeIndex"],
    queued: ["Shared link queued for download. It will appear once it finishes.", "info", "Shared to MemeIndex"],
    linkerror: ["Could not import that shared link.", "error", "Shared to MemeIndex"],
    toolarge: ["That shared file is larger than the upload limit.", "error", "Shared to MemeIndex"],
    forbidden: ["You do not have permission to add memes.", "error", "Shared to MemeIndex"],
    ratelimited: ["Too many shares in a row. Wait a minute and try again.", "error", "Shared to MemeIndex"],
    pending: ["Still downloading that link. It will appear once it finishes.", "info", "Shared to MemeIndex"],
    empty: ["Nothing usable was in that share.", "info", "Shared to MemeIndex"],
    error: ["Something went wrong handling that share.", "error", "Shared to MemeIndex"],
  };
  const [message, type, title] = messages[status] || messages.error;
  showToast(message, type, { title, duration: type === "error" ? 5200 : 3600 });

  params.delete("shared");
  params.delete("n");
  const query = params.toString();
  window.history.replaceState(window.history.state || {}, "", query ? `/?${query}` : "/");
}

async function openDeepLinkedMeme() {
  const id = deepLinkedMemeID();
  if (!id) return;
  const response = await fetch(`/api/memes/${encodeURIComponent(id)}`);
  if (!(await expectAuthorized(response, "Could not open the shared meme."))) {
    clearMemeDeepLinkURL();
    return;
  }
  const meme = await response.json();
  openModalWithMeme(upsertMemeInState(meme) || meme);
}
