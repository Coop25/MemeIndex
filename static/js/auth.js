// @ts-check
// Signed-in user, permissions, the account menu, and installing the app.

function getAuthInitials() {
  const label = state.auth.user?.display_name || state.auth.user?.username || "MemeIndex";
  const pieces = label.trim().split(/\s+/).filter(Boolean).slice(0, 2);
  if (pieces.length === 0) return "MI";
  return pieces.map((part) => part[0]).join("").toUpperCase();
}

function closeAuthMenu() {
  authMenu.classList.add("hidden");
  authTrigger.setAttribute("aria-expanded", "false");
}

function toggleAuthMenu() {
  const nextHiddenState = !authMenu.classList.contains("hidden");
  authMenu.classList.toggle("hidden", nextHiddenState);
  authTrigger.setAttribute("aria-expanded", String(!nextHiddenState));
}

function isStandaloneApp() {
  return window.matchMedia("(display-mode: standalone)").matches || /** @type {Navigator & { standalone?: boolean }} */ (window.navigator).standalone === true;
}

function isIOSDevice() {
  const userAgent = window.navigator.userAgent || "";
  return /iphone|ipad|ipod/i.test(userAgent) || (window.navigator.platform === "MacIntel" && window.navigator.maxTouchPoints > 1);
}

function syncInstallAction() {
  if (!authInstall) return;
  const canOfferInstall = !isStandaloneApp() && (!!deferredInstallPrompt || isIOSDevice());
  authInstall.classList.toggle("hidden", !canOfferInstall);
}

async function installMemeIndex() {
  closeAuthMenu();
  if (isStandaloneApp()) {
    syncInstallAction();
    return;
  }

  if (deferredInstallPrompt) {
    deferredInstallPrompt.prompt();
    const choice = await deferredInstallPrompt.userChoice;
    deferredInstallPrompt = null;
    syncInstallAction();
    if (choice.outcome === "accepted") {
      showToast("MemeIndex was added to your home screen.", "success", { title: "App Installed", duration: 4200 });
    }
    return;
  }

  if (isIOSDevice()) {
    showToast("In Safari, tap the Share button, choose Add to Home Screen, then turn on Open as Web App.", "info", { title: "Install MemeIndex", duration: 9000 });
    return;
  }

  showToast("Open your browser menu and choose Install app or Add to Home screen.", "info", { title: "Install MemeIndex", duration: 7000 });
}

function canView() {
  return !!state.auth.permissions?.canView;
}

function canUpload() {
  return !!state.auth.permissions?.canUpload || !!state.auth.permissions?.canAdd;
}

function canAdd() {
  return canUpload();
}

function canAddTags() {
  return !!state.auth.permissions?.canAddTags || !!state.auth.permissions?.canManage;
}

function canRemoveTags() {
  return !!state.auth.permissions?.canRemoveTags || !!state.auth.permissions?.canManage;
}

function canDeleteMemes() {
  return !!state.auth.permissions?.canDeleteMemes || !!state.auth.permissions?.canManage;
}

function canManageUsers() {
  return !!state.auth.permissions?.canManageUsers;
}

// Audiobooks need the database-backed store; a server running on the legacy
// JSON store reports audiobooks_available: false and every audiobook request
// would answer 501, so no permission (not even admin) shows the section.
function audiobooksAvailable() {
  return state.auth.audiobooksAvailable !== false;
}

function canViewAudiobooks() {
  if (!audiobooksAvailable()) return false;
  return !!state.auth.permissions?.canViewAudiobooks || !!state.auth.permissions?.canUploadAudiobooks || !!state.auth.permissions?.canDeleteAudiobooks || canManageUsers();
}

function canUploadAudiobooks() {
  if (!audiobooksAvailable()) return false;
  return !!state.auth.permissions?.canUploadAudiobooks || canManageUsers();
}

function canDeleteAudiobooks() {
  return !!state.auth.permissions?.canDeleteAudiobooks || canManageUsers();
}

function canManageAudiobookSeries() {
  return !!state.auth.permissions?.canManageAudiobookSeries || canManageUsers();
}

function canEditMetadata() {
  return canAddTags() || canRemoveTags() || canManageUsers();
}

function canManage() {
  return canEditMetadata() || canDeleteMemes() || canManageUsers();
}

function permissionLabel() {
  if (canManageUsers()) return "Super Admin";
  const permissions = [];
  if (canView()) permissions.push("View");
  if (canUpload()) permissions.push("Upload");
  if (canAddTags()) permissions.push("Add tags");
  if (canRemoveTags()) permissions.push("Remove tags");
  if (canDeleteMemes()) permissions.push("Delete memes");
  if (canViewAudiobooks()) permissions.push("View audiobooks");
  if (canUploadAudiobooks()) permissions.push("Upload audiobooks");
  if (canDeleteAudiobooks()) permissions.push("Delete audiobooks");
  if (canManageAudiobookSeries()) permissions.push("Manage series");
  if (permissions.length > 0) return permissions.join(" • ");
  return "No access";
}

function renderAuthState() {
  const displayName = state.auth.user?.display_name || state.auth.user?.username || "Local access";
  authName.textContent = displayName;
  authRole.textContent = permissionLabel();
  authVersionValue.textContent = state.auth.version || "dev";
  authAvatarFallback.textContent = getAuthInitials();
  authTrigger.title = `${displayName} (${permissionLabel()}) - ${state.auth.version || "dev"}`;
  authLogout.href = state.auth.logoutURL || "/auth/logout";
  authLogout.classList.toggle("hidden", !state.auth.enabled || !state.auth.authenticated);
  authAdmin?.classList.toggle("hidden", !canManageUsers());
  openUploadModalButton.disabled = !canUpload();
  openUploadModalButton.setAttribute("aria-disabled", String(!canUpload()));
  openUploadModalButton.title = canUpload() ? "Add File" : "You do not have permission to upload";
  navAudiobooksItem?.classList.toggle("hidden", !canViewAudiobooks());
  audiobooksSubnav?.classList.toggle("hidden", !canViewAudiobooks());
  openAudiobookUploadButton?.classList.toggle("hidden", !canUploadAudiobooks());
  openAudiobookSeriesManagerButton?.classList.toggle("hidden", !canManageAudiobookSeries());
  document.body.classList.toggle("audiobooks-only-access", !canView() && canViewAudiobooks());

  if (state.auth.user?.avatar_url) {
    authAvatar.src = state.auth.user.avatar_url;
    authAvatar.classList.remove("hidden");
    authAvatarFallback.classList.add("hidden");
  } else {
    authAvatar.removeAttribute("src");
    authAvatar.classList.add("hidden");
    authAvatarFallback.classList.remove("hidden");
  }
}

function redirectToForbidden() {
  window.location.href = "/forbidden";
}

async function redirectToForbiddenIfViewAccessWasRevoked() {
  try {
    const response = await fetch("/api/auth/session", {
      credentials: "same-origin",
      cache: "no-store",
    });
    if (response.status === 401) {
      window.location.href = "/auth/login";
      return true;
    }
    if (!response.ok) {
      return false;
    }

    const payload = await response.json();
    const permissions = payload.permissions || {};
    if (payload.enabled && payload.authenticated !== false && !permissions.canView && !permissions.canViewAudiobooks) {
      redirectToForbidden();
      return true;
    }
  } catch (error) {
    console.error(error);
  }

  return false;
}

async function fetchAuthSession() {
  const response = await fetch("/api/auth/session");
  if (response.status === 401) {
    window.location.href = "/auth/login";
    return;
  }
  if (!response.ok) {
    throw new Error("Failed to load auth session");
  }

  const payload = await response.json();
  state.auth = {
    enabled: !!payload.enabled,
    authenticated: payload.authenticated !== false,
    user: payload.user || null,
    version: payload.version || "dev",
    audiobookUploadConcurrency:
      Number.isFinite(payload.audiobook_upload_concurrency) && payload.audiobook_upload_concurrency > 0
        ? payload.audiobook_upload_concurrency
        : AUDIOBOOK_UPLOAD_CONCURRENCY_DEFAULT,
    permissions: payload.permissions || {
      canView: false,
      canUpload: false,
      canAddTags: false,
      canRemoveTags: false,
      canDeleteMemes: false,
      canManageUsers: false,
      canViewAudiobooks: false,
      canUploadAudiobooks: false,
      canDeleteAudiobooks: false,
      canManageAudiobookSeries: false,
      canAdd: false,
      canManage: false,
    },
    logoutURL: payload.logout_url || "/auth/logout",
    publicBaseURL: String(payload.public_base_url || "").trim(),
    audiobooksAvailable: payload.audiobooks_available !== false,
  };

  if (state.auth.enabled && state.auth.authenticated && !state.auth.permissions?.canView && !state.auth.permissions?.canViewAudiobooks) {
    redirectToForbidden();
    return;
  }

  renderAuthState();
}

function authPanelContains(target) {
  return !!target && document.querySelector("#auth-panel")?.contains(target);
}
