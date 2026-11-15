// @ts-check
// Shared helpers used across the app: API error handling, formatting of
// dates, durations and sizes, HTML escaping, toasts, and confirm dialogs.

let toastSequence = 0;
const activeToastTimeouts = new Map();

function showToast(message, type = "info", options = {}) {
  if (!toastRegion || !message) {
    return 0;
  }

  const toastType = ["success", "error", "info"].includes(type) ? type : "info";
  const title = options.title || (
    toastType === "success"
      ? "Success"
      : toastType === "error"
        ? "Problem"
        : "Heads Up"
  );
  const duration = Number.isFinite(options.duration) ? options.duration : (toastType === "error" ? 4200 : 2600);
  const toastID = ++toastSequence;
  const toast = document.createElement("article");
  toast.className = `toast toast-${toastType}`;
  toast.dataset.toastId = String(toastID);
  toast.setAttribute("role", toastType === "error" ? "alert" : "status");
  const icon = toastType === "success" ? "&#10003;" : toastType === "error" ? "!" : "i";
  toast.innerHTML = `
    <span class="toast-icon" aria-hidden="true">${icon}</span>
    <div class="toast-copy"><strong class="toast-title">${escapeHTML(title)}</strong><span class="toast-message">${escapeHTML(message)}</span></div>
    <button class="toast-dismiss" type="button" aria-label="Dismiss notification">&#10005;</button>
  `;
  toast.querySelector(".toast-dismiss")?.addEventListener("click", () => dismissToast(toastID));
  toastRegion.appendChild(toast);

  window.requestAnimationFrame(() => {
    toast.classList.add("is-visible");
  });

  const timeoutID = window.setTimeout(() => {
    dismissToast(toastID);
  }, Math.max(900, duration));
  activeToastTimeouts.set(toastID, timeoutID);
  return toastID;
}

function dismissToast(toastID) {
  const selector = `[data-toast-id="${String(toastID)}"]`;
  const toast = toastRegion?.querySelector(selector);
  const timeoutID = activeToastTimeouts.get(toastID);
  if (timeoutID) {
    window.clearTimeout(timeoutID);
    activeToastTimeouts.delete(toastID);
  }
  if (!toast) {
    return;
  }

  toast.classList.remove("is-visible");
  window.setTimeout(() => {
    toast.remove();
  }, 180);
}

async function expectAuthorized(response, failureMessage) {
  if (response.status === 401) {
    window.location.href = "/auth/login";
    return false;
  }

  if (response.status === 403) {
    if (await redirectToForbiddenIfViewAccessWasRevoked()) {
      return false;
    }
    if (!state.auth.permissions?.canView && !state.auth.permissions?.canViewAudiobooks) {
      redirectToForbidden();
      return false;
    }
    window.alert("You do not have permission to do that.");
    return false;
  }

  if (!response.ok) {
    window.alert(failureMessage);
    return false;
  }

  return true;
}

async function readAPIErrorMessage(response, fallbackMessage) {
  const contentType = response.headers.get("content-type") || "";
  const body = (await response.text()).trim();
  if (!body) {
    return fallbackMessage;
  }

  const bodyLower = body.toLowerCase();
  if (contentType.includes("text/html") || bodyLower.includes("<html")) {
    if (bodyLower.includes("cloudflare") && bodyLower.includes("502")) {
      return "Cloudflare returned a 502 while the link was being processed. The app or tunnel lost the origin request. Check the app and cloudflared logs, then try again.";
    }
    if (bodyLower.includes("cloudflare") && bodyLower.includes("524")) {
      return "Cloudflare timed out waiting for the server to finish processing this request. Try a smaller/faster link or run the request through the direct host instead of the tunnel.";
    }
    return fallbackMessage;
  }

  return body;
}

function formatDurationShort(seconds) {
  if (!Number.isFinite(seconds) || seconds <= 0) return "a moment";
  if (seconds < 90) return `${Math.round(seconds)}s`;
  if (seconds < 5400) return `${Math.round(seconds / 60)} min`;
  return `${(seconds / 3600).toFixed(1)} h`;
}

function formatLastActive(unixSeconds) {
  const timestamp = Number(unixSeconds || 0);
  if (!Number.isFinite(timestamp) || timestamp <= 0) {
    return "No activity recorded yet";
  }

  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(timestamp * 1000));
  } catch (error) {
    return new Date(timestamp * 1000).toLocaleString();
  }
}

function formatDateTime(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown time";
  }

  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(date);
  } catch (error) {
    return date.toLocaleString();
  }
}

// formatDateTimeZoned is formatDateTime plus an explicit timezone abbreviation,
// for admin columns (share expiry, backup timestamps) where the absolute moment
// matters and there is no relative-time context to lean on.
function formatDateTimeZoned(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown time";
  }
  try {
    return new Intl.DateTimeFormat(undefined, {
      dateStyle: "medium",
      timeStyle: "short",
      timeZoneName: "short",
    }).format(date);
  } catch (error) {
    return date.toLocaleString();
  }
}

// formatRelativeTime renders "just now" / "5m ago" / "3h ago" / "2d ago" for
// recent moments and falls back to the absolute date beyond a week. Admin feeds
// use this as the visible label and keep the absolute time in a title tooltip.
function formatRelativeTime(value) {
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) {
    return "Unknown time";
  }
  const deltaSeconds = Math.round((Date.now() - date.getTime()) / 1000);
  const future = deltaSeconds < 0;
  const seconds = Math.abs(deltaSeconds);
  const frame = (text) => (future ? `in ${text}` : `${text} ago`);
  if (seconds < 45) return future ? "in a moment" : "just now";
  if (seconds < 90) return frame("a minute");
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return frame(`${minutes}m`);
  const hours = Math.round(minutes / 60);
  if (hours < 24) return frame(`${hours}h`);
  const days = Math.round(hours / 24);
  if (days < 7) return frame(`${days}d`);
  return formatDateTime(date);
}

function formatRelativeQueueState(value) {
  switch (value) {
    case "waiting_for_ollama":
      return "Waiting for Ollama";
    case "processing":
      return "Processing";
    case "retrying":
      return "Retrying";
    case "idle":
      return "Idle";
    case "disabled":
      return "Disabled";
    case "starting":
      return "Starting";
    default:
      return value ? String(value) : "Unknown";
  }
}

function formatQueueError(value) {
  const message = String(value || "").trim();
  if (!message) {
    return "No recent worker error";
  }

  return message
    .replace(/^tag suggestion service is unavailable:\s*/i, "")
    .replace(/^ERROR:\s*/i, "")
    .replace(/\s+/g, " ")
    .trim();
}

function formatDurationWords(totalSeconds) {
  const seconds = Number(totalSeconds || 0);
  if (!Number.isFinite(seconds) || seconds <= 0) {
    return "Unknown";
  }
  if (seconds % 3600 === 0) {
    const hours = seconds / 3600;
    return `${hours} hour${hours === 1 ? "" : "s"}`;
  }
  if (seconds % 60 === 0) {
    const minutes = seconds / 60;
    return `${minutes} minute${minutes === 1 ? "" : "s"}`;
  }
  return `${seconds} second${seconds === 1 ? "" : "s"}`;
}

function escapeHTML(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll("\"", "&quot;")
    .replaceAll("'", "&#39;");
}

function forceFreshHTMLReload() {
  const url = new URL(window.location.href);
  url.searchParams.set("refresh", `${Date.now()}`);
  closeAuthMenu();
  window.location.replace(url.toString());
}

// confirmViaModal is a styled stand-in for window.confirm(): it resolves true
// only if the Confirm button is clicked, and false for Cancel, Escape, or any
// other way the dialog closes. Falls back to window.confirm if the modal
// markup is missing for some reason.
/**
 * @param {{ title?: string, message?: string, confirmLabel?: string, cancelLabel?: string, danger?: boolean }} [options]
 * @returns {Promise<boolean>}
 */
function confirmViaModal({ title, message, confirmLabel = "Confirm", cancelLabel = "Cancel", danger = true } = {}) {
  return new Promise((resolve) => {
    if (!confirmModal || !confirmModalConfirmButton || !confirmModalCancelButton) {
      resolve(window.confirm([title, message].filter(Boolean).join("\n\n")));
      return;
    }

    if (confirmModalTitle) confirmModalTitle.textContent = title || "Are you sure?";
    if (confirmModalMessage) confirmModalMessage.textContent = message || "";
    confirmModalConfirmButton.textContent = confirmLabel;
    confirmModalCancelButton.textContent = cancelLabel;
    confirmModalConfirmButton.classList.toggle("danger-button", danger);
    confirmModalConfirmButton.classList.toggle("primary-button", !danger);

    let result = false;
    const onConfirmClick = () => { result = true; confirmModal.close(); };
    const onCancelClick = () => { result = false; confirmModal.close(); };
    const onClose = () => {
      confirmModalConfirmButton.removeEventListener("click", onConfirmClick);
      confirmModalCancelButton.removeEventListener("click", onCancelClick);
      confirmModal.removeEventListener("close", onClose);
      resolve(result);
    };

    confirmModalConfirmButton.addEventListener("click", onConfirmClick);
    confirmModalCancelButton.addEventListener("click", onCancelClick);
    confirmModal.addEventListener("close", onClose);
    confirmModal.showModal();
  });
}

function isModalBusy(modal) {
  return modal?.classList.contains("is-busy");
}

function setModalBusyState(modal, overlay, messageNode, busy, message) {
  if (!modal || !overlay) return;
  modal.classList.toggle("is-busy", busy);
  overlay.classList.toggle("hidden", !busy);
  overlay.setAttribute("aria-hidden", String(!busy));
  if (messageNode && typeof message === "string" && message.trim()) {
    messageNode.textContent = message;
  }
}

function splitTags(raw) {
  return raw
    .split(",")
    .map((tag) => tag.trim().toLowerCase())
    .filter(Boolean);
}

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}
