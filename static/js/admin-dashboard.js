// The admin dashboard: metrics, charts, work queues, and listening stats.

function dashboardTrend(current, previous, suffix = "vs previous 30 days") {
  const currentValue = Number(current || 0);
  const previousValue = Number(previous || 0);
  if (currentValue === 0 && previousValue === 0) return { direction: "flat", label: `No change ${suffix}` };
  if (previousValue === 0) return { direction: "up", label: `New activity ${suffix}` };
  const percent = Math.round(((currentValue - previousValue) / previousValue) * 100);
  return {
    direction: percent > 0 ? "up" : percent < 0 ? "down" : "flat",
    label: `${percent > 0 ? "+" : ""}${percent}% ${suffix}`,
  };
}

function adminChartTooltipAttributes(label) {
  const text = escapeHTML(label);
  return `data-chart-tooltip="${text}" aria-label="${text}"`;
}

function buildAdminChartPoints(points, coordinates, width, height, valueLabel) {
  // Each day's target extends to the midpoint of its neighbors, including both endpoints.
  return points.map((entry, index) => {
    const [x, y] = coordinates[index];
    const left = index === 0 ? 0 : (coordinates[index - 1][0] + x) / 2;
    const right = index === points.length - 1 ? width : (x + coordinates[index + 1][0]) / 2;
    const label = `${entry.date || "Unknown date"} (UTC day)\n${valueLabel(entry, index)}`;
    return `<g class="admin-chart-point" tabindex="0" role="img" ${adminChartTooltipAttributes(label)}>
      <circle class="admin-chart-dot" cx="${x.toFixed(2)}" cy="${y.toFixed(2)}" r="2.6" />
      <rect class="admin-chart-hit" x="${left.toFixed(2)}" y="0" width="${(right - left).toFixed(2)}" height="${height}" />
    </g>`;
  }).join("");
}

function bindAdminChartTooltips(root) {
  let tooltip = document.querySelector("#admin-chart-tooltip");
  if (!tooltip) {
    tooltip = document.createElement("div");
    tooltip.id = "admin-chart-tooltip";
    tooltip.className = "admin-chart-tooltip";
    tooltip.setAttribute("role", "tooltip");
    document.body.append(tooltip);
    // Fixed positioning keeps tooltips clear of the metric cards' clipped edges.
    window.addEventListener("scroll", () => { tooltip.hidden = true; }, true);
    window.addEventListener("resize", () => { tooltip.hidden = true; });
  }
  tooltip.hidden = true;
  root.querySelectorAll("[data-chart-tooltip]").forEach((target) => {
    const show = (event) => {
      tooltip.textContent = target.dataset.chartTooltip;
      tooltip.hidden = false;
      const anchor = (target.querySelector("circle") || target).getBoundingClientRect();
      const x = event.type.startsWith("pointer") ? event.clientX : anchor.left + anchor.width / 2;
      const y = event.type.startsWith("pointer") ? event.clientY : anchor.top;
      const bounds = tooltip.getBoundingClientRect();
      tooltip.style.left = `${Math.max(8, Math.min(x - bounds.width / 2, window.innerWidth - bounds.width - 8))}px`;
      const top = y - bounds.height - 12;
      tooltip.style.top = `${Math.max(8, Math.min(top < 8 ? y + 16 : top, window.innerHeight - bounds.height - 8))}px`;
    };
    const hide = () => { tooltip.hidden = true; };
    target.addEventListener("pointerenter", show);
    target.addEventListener("pointermove", show);
    target.addEventListener("pointerleave", hide);
    target.addEventListener("focus", (event) => {
      // Focusing a point may scroll it into view; position after that scroll settles.
      requestAnimationFrame(() => { if (document.activeElement === target) show(event); });
    });
    target.addEventListener("blur", hide);
    target.addEventListener("click", hide);
    target.addEventListener("keydown", (event) => { if (event.key === "Escape") hide(); });
  });
}

function buildAdminMetricSparkline(series, key, label, valueFormatter = (value) => Number(value || 0).toLocaleString()) {
  const points = Array.isArray(series) ? series : [];
  if (!points.length) return "";
  const width = 240;
  const height = 44;
  const padding = 5;
  const values = points.map((entry) => Number(entry?.[key] || 0));
  const minimum = Math.min(...values);
  const maximum = Math.max(...values);
  const range = maximum - minimum;
  const coordinates = values.map((value, index) => {
    const x = points.length === 1 ? width / 2 : (index / (points.length - 1)) * width;
    const normalized = range === 0 ? .5 : (value - minimum) / range;
    const y = height - padding - (normalized * (height - (padding * 2)));
    return [x, y];
  });
  const line = coordinates.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `0,${height} ${line} ${width},${height}`;
  const hitAreas = buildAdminChartPoints(points, coordinates, width, height, (entry, index) => `${label}: ${valueFormatter(values[index])}`);
  const gradientID = `admin-metric-fill-${key}`;
  return `
    <svg class="admin-metric-spark" viewBox="0 0 ${width} ${height}" preserveAspectRatio="none" role="group" aria-label="${escapeHTML(label)} over the last 30 days">
      <defs><linearGradient id="${gradientID}" x1="0" y1="0" x2="0" y2="1"><stop class="admin-metric-spark-fill-start" offset="0"/><stop class="admin-metric-spark-fill-end" offset="1"/></linearGradient></defs>
      <polygon class="admin-metric-spark-area" points="${area}" fill="url(#${gradientID})" />
      <polyline class="admin-metric-spark-line" points="${line}" />
      ${hitAreas}
    </svg>
  `;
}

function buildAdminUploadChart(series, options = {}) {
  const {
    emptyText = "Upload history will appear here.",
    summaryLabel = "uploads in 30 days",
    ariaLabel = "Daily uploads over the last 30 days",
    valueName = "Uploads",
    gradientId = "admin-upload-fill",
  } = options;
  const points = Array.isArray(series) ? series : [];
  if (!points.length) return `<p class="users-empty">${escapeHTML(emptyText)}</p>`;
  const width = 720;
  const height = 205;
  const max = Math.max(1, ...points.map((entry) => Number(entry.uploads || 0)));
  const coordinates = points.map((entry, index) => {
    const x = points.length === 1 ? width / 2 : (index / (points.length - 1)) * width;
    const y = height - ((Number(entry.uploads || 0) / max) * (height - 18)) - 6;
    return [x, y];
  });
  const line = coordinates.map(([x, y]) => `${x.toFixed(1)},${y.toFixed(1)}`).join(" ");
  const area = `0,${height} ${line} ${width},${height}`;
  const total = points.reduce((sum, entry) => sum + Number(entry.uploads || 0), 0);
  return `
    <div class="admin-chart-summary"><strong>${escapeHTML(String(total))}</strong><span>${escapeHTML(summaryLabel)}</span></div>
    <svg class="admin-upload-svg" viewBox="0 0 ${width} ${height}" role="group" aria-label="${escapeHTML(ariaLabel)}">
      <defs><linearGradient id="${gradientId}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#65ca73" stop-opacity=".42"/><stop offset="1" stop-color="#65ca73" stop-opacity="0"/></linearGradient></defs>
      <path class="admin-chart-grid" d="M0 35H720M0 85H720M0 135H720M0 185H720" />
      <polygon points="${area}" fill="url(#${gradientId})" />
      <polyline points="${line}" fill="none" stroke="#72d17c" stroke-width="3" stroke-linecap="round" stroke-linejoin="round" />
      ${buildAdminChartPoints(points, coordinates, width, height, (entry) => `${valueName}: ${Number(entry.uploads || 0).toLocaleString()}`)}
    </svg>
    <div class="admin-chart-axis"><span>${escapeHTML(points[0]?.date || "")}</span><span>${escapeHTML(points[Math.floor(points.length / 2)]?.date || "")}</span><span>${escapeHTML(points[points.length - 1]?.date || "")}</span></div>
  `;
}

function buildAdminTopTags(tags) {
  const topTags = (Array.isArray(tags) ? tags : []).slice(0, 12);
  if (!topTags.length) return `<p class="users-empty">No tags have been used yet.</p>`;
  const colors = ["#47b76a", "#55a1f3", "#9b72e4", "#e1ad39", "#32c2c9", "#f08d46", "#e66791", "#79c95b", "#4f78d6", "#c869d4", "#d6ca4f", "#45a98f"];
  const segments = topTags.map((entry, index) => ({ label: entry.tag || "", count: Number(entry.count || 0), color: colors[index] }));
  const displayedAssignments = Math.max(1, segments.reduce((sum, entry) => sum + entry.count, 0));
  const tooltipLabel = (entry) => `${entry.label}\n${entry.count.toLocaleString()} tag assignments (${((entry.count / displayedAssignments) * 100).toFixed(1)}% of top tags)`;
  let angle = -Math.PI / 2;
  const position = (radius, radians) => `${(66 + radius * Math.cos(radians)).toFixed(4)},${(66 + radius * Math.sin(radians)).toFixed(4)}`;
  const slices = segments.filter((entry) => entry.count > 0).map((entry) => {
    const end = angle + (entry.count / displayedAssignments) * Math.PI * 2;
    const middle = (angle + end) / 2;
    // Two arcs per edge also handle a single tag occupying the entire ring.
    const path = `M${position(65, angle)} A65,65 0 0 1 ${position(65, middle)} A65,65 0 0 1 ${position(65, end)} L${position(37.5, end)} A37.5,37.5 0 0 0 ${position(37.5, middle)} A37.5,37.5 0 0 0 ${position(37.5, angle)} Z`;
    angle = end;
    return `<path class="admin-tag-slice" d="${path}" fill="${entry.color}" tabindex="0" role="img" ${adminChartTooltipAttributes(tooltipLabel(entry))} />`;
  }).join("");
  return `
    <div class="admin-category-donut">
      <svg viewBox="0 0 132 132" role="group" aria-label="Top tags by tag assignments">${slices}</svg>
    </div>
    <div class="admin-category-legend">${segments.map((entry) => `<button type="button" data-admin-tag="${escapeHTML(entry.label)}" ${adminChartTooltipAttributes(tooltipLabel(entry))}><i style="--tag-color:${entry.color}"></i><span class="tag-chip">${escapeHTML(entry.label)}</span><strong>${Math.round((entry.count / displayedAssignments) * 100)}%</strong><small>${entry.count.toLocaleString()}</small></button>`).join("")}</div>
  `;
}

function buildAdminDashboardMetrics(dashboard) {
  const counts = dashboard.counts || {};
  const number = (value) => Number(value || 0);
  const uploadTrend = dashboardTrend(dashboard.uploaded_last_30_days, dashboard.uploaded_previous_30_days);
  const storageTrend = dashboardTrend(dashboard.bytes_last_30_days, dashboard.bytes_previous_30_days);
  const audiobooks = dashboard.audiobook_stats || null;
  const memeBytes = number(dashboard.total_size_bytes);
  const metricSeries = Array.isArray(dashboard.metric_series) ? dashboard.metric_series : [];
  let storageSeries = metricSeries;
  if (audiobooks && Array.isArray(audiobooks.series) && audiobooks.series.length) {
    const bytesByDate = new Map(audiobooks.series.map((entry) => [entry.date, number(entry.bytes)]));
    let carried = 0;
    storageSeries = metricSeries.map((entry) => {
      if (bytesByDate.has(entry.date)) carried = bytesByDate.get(entry.date);
      return { ...entry, storage_bytes: number(entry.storage_bytes) + carried };
    });
  }
  const metrics = [
    { icon: "M", label: "Total Memes", value: number(counts.total).toLocaleString(), note: uploadTrend.label, trend: uploadTrend.direction, accent: "green", seriesKey: "memes", seriesLabel: "Cumulative meme count", series: metricSeries },
    { icon: "#", label: "Tags", value: number(dashboard.unique_tags).toLocaleString(), note: `${number(dashboard.total_tag_assignments).toLocaleString()} assignments`, trend: "flat", accent: "purple", seriesKey: "tags", seriesLabel: "Cumulative unique tag count", series: metricSeries },
    { icon: "U", label: "Users", value: number(dashboard.user_count).toLocaleString(), note: `${number(dashboard.active_users_30d)} active in 30 days`, trend: number(dashboard.new_users_30d) > 0 ? "up" : "flat", accent: "blue", seriesKey: "users", seriesLabel: "Cumulative user count", series: metricSeries },
    {
      icon: "S", label: "Storage Used",
      value: formatSize(audiobooks ? number(dashboard.total_storage_bytes) : memeBytes),
      note: audiobooks ? `Memes ${formatSize(memeBytes)} · Audiobooks ${formatSize(number(audiobooks.total_bytes))}` : storageTrend.label,
      trend: audiobooks ? "flat" : storageTrend.direction, accent: "amber",
      seriesKey: "storage_bytes", seriesLabel: "Cumulative storage used", series: storageSeries,
      valueFormatter: (value) => `${formatSize(value)} (${value.toLocaleString()} bytes)`,
    },
  ];
  if (audiobooks) {
    const hours = Math.round(number(audiobooks.duration_seconds) / 3600);
    metrics.push({ icon: "A", label: "Audiobooks", value: number(audiobooks.count).toLocaleString(), note: `${hours.toLocaleString()} h total · +${number(audiobooks.added_last_30_days).toLocaleString()} in 30 days`, trend: number(audiobooks.added_last_30_days) > 0 ? "up" : "flat", accent: "purple" });
  }
  metrics.push(
    { icon: "F", label: "Favorites", value: number(counts.favorites).toLocaleString(), note: "Across all users", trend: "flat", accent: "red", seriesKey: "favorites", seriesLabel: "Cumulative favorite assignments", series: metricSeries },
    { icon: "L", label: "Active Share Links", value: number(dashboard.active_share_count).toLocaleString(), note: "Public links available now", trend: "flat", accent: "cyan" },
  );
  const disk = dashboard.disk || null;
  const diskTotal = disk ? number(disk.total_bytes) : 0;
  metrics.push({
    icon: "D", label: "Disk",
    value: disk ? `${formatSize(number(disk.free_bytes))} free` : "Unknown",
    note: disk ? `of ${formatSize(diskTotal)}` : "Disk usage unavailable",
    trend: "flat", accent: disk && disk.low ? "red" : "slate",
    meter: disk && diskTotal > 0 ? Math.min(1, Math.max(0, number(disk.used_bytes) / diskTotal)) : null,
  });
  return metrics;
}

// Badge counts prefer each tab's live source once loaded, and fall back to the
// dashboard's work_queues before that, then to the existing state.
function computeAdminTabBadgeCounts(admin) {
  const queues = admin.dashboard?.work_queues || null;
  const linkStatus = admin.linkRetryStatus;
  const pick = (liveLoaded, live, key) => (liveLoaded || !queues ? live : Number(queues[key]) || 0);
  return {
    tagReview: pick(!!admin.tagReview?.loaded, Number(admin.tagReview?.total || 0) || 0, "suggested_tags"),
    linkRetries: pick(!!linkStatus, Array.isArray(linkStatus?.queued) ? linkStatus.queued.length : 0, "link_retries"),
    rejectedLinks: pick(!!linkStatus, Array.isArray(linkStatus?.rejected) ? linkStatus.rejected.length : 0, "rejected_links"),
    deleteQueue: pick(!!admin.queue?.loaded, Number(admin.queue?.total || 0) || 0, "delete_queue"),
  };
}

function adminBadgeCountsChanged(previous, next) {
  if (!previous || !next) return true;
  const keys = new Set([...Object.keys(previous), ...Object.keys(next)]);
  return [...keys].some((key) => (previous[key] || 0) !== (next[key] || 0));
}

// syncAdminWorkQueue writes a freshly loaded tab count into the cached dashboard
// so the dashboard strip agrees when it is shown again.
function syncAdminWorkQueue(admin, key, value) {
  if (admin.dashboard?.work_queues) admin.dashboard.work_queues[key] = Math.max(0, Number(value) || 0);
}

// Work-queue chips and audiobook-health counts link to other admin tabs. `canOpen`
// lets the caller render a plain count when the target tab does not exist (yet).
const ADMIN_WORK_QUEUE_CHIPS = Object.freeze([
  { key: "delete_queue", tab: "delete-queue", label: "Delete queue" },
  { key: "suggested_tags", tab: "tag-review", label: "Suggested tags" },
  { key: "link_retries", tab: "link-retries", label: "Link retries" },
  { key: "rejected_links", tab: "rejected-links", label: "Rejected links" },
  { key: "conversion_queue", tab: "audiobook-conversion", label: "MP3 conversion" },
]);

function buildAdminWorkQueues(queues, canOpen = () => true) {
  const source = queues || {};
  return ADMIN_WORK_QUEUE_CHIPS.map((chip) => {
    const count = Math.max(0, Number(source[chip.key]) || 0);
    const inner = `<strong>${escapeHTML(count.toLocaleString())}</strong><span>${escapeHTML(chip.label)}</span>`;
    const empty = count === 0;
    if (!canOpen(chip.tab)) return `<div class="admin-queue-chip" data-empty="${empty}">${inner}</div>`;
    return `<button type="button" class="admin-queue-chip" data-admin-open-tab="${escapeHTML(chip.tab)}" data-empty="${empty}">${inner}</button>`;
  }).join("");
}

function formatListeningDuration(seconds) {
  const total = Math.max(0, Math.round(Number(seconds) || 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  if (hours) return `${hours}h ${minutes}m`;
  if (minutes) return `${minutes}m`;
  return `${total}s`;
}

function buildAdminListening(activity) {
  const source = activity || {};
  const daily = Array.isArray(source.daily_seconds) ? source.daily_seconds : [];
  const books = Array.isArray(source.top_books) ? source.top_books : [];
  const listeners = Array.isArray(source.top_listeners) ? source.top_listeners : [];
  const totalSeconds = daily.reduce((sum, entry) => sum + (Number(entry.seconds) || 0), 0);
  const chart = totalSeconds > 0
    ? buildAdminUploadChart(daily.map((entry) => ({ date: entry.date, uploads: Math.round((Number(entry.seconds) || 0) / 60) })), {
      summaryLabel: "minutes listened in 30 days",
      ariaLabel: "Daily listening minutes over the last 30 days",
      valueName: "Minutes",
      gradientId: "admin-listening-fill",
    })
    : `<p class="users-empty">No listening yet.</p>`;
  const list = (rows) => rows.length
    ? `<ol class="admin-rank-list">${rows.map((row) => `<li><span title="${escapeHTML(row.label)}">${escapeHTML(row.label)}</span><strong>${escapeHTML(formatListeningDuration(row.seconds))}</strong></li>`).join("")}</ol>`
    : `<p class="users-empty">No listening yet.</p>`;
  return `
    <div class="admin-dashboard-upload-chart admin-listening-chart">${chart}</div>
    <div class="admin-rank-columns">
      <div><h4>Top books</h4>${list(books.map((book) => ({ label: book.title || "Untitled", seconds: book.seconds })))}</div>
      <div><h4>Top listeners</h4>${list(listeners.map((who) => ({ label: who.display_name || "Unknown", seconds: who.seconds })))}</div>
    </div>
  `;
}

const ADMIN_AUDIOBOOK_HEALTH_ROWS = Object.freeze([
  { key: "awaiting_conversion", filter: "not-converted", label: "Awaiting conversion" },
  { key: "failed_conversion", filter: "conversion-failed", label: "Failed conversion" },
  { key: "missing_cover", filter: "no-cover", label: "Missing cover" },
  { key: "missing_chapters", filter: "no-chapters", label: "Missing chapters" },
  { key: "missing_series", filter: "no-series", label: "Missing series" },
]);

function buildAdminAudiobookHealth(health, canOpen = () => true) {
  const source = health || {};
  const linked = canOpen("audiobook-library");
  return `<div class="admin-audiobook-health-list">${ADMIN_AUDIOBOOK_HEALTH_ROWS.map((row) => {
    const count = Math.max(0, Number(source[row.key]) || 0);
    const inner = `<span>${escapeHTML(row.label)}</span><strong data-empty="${count === 0}">${escapeHTML(count.toLocaleString())}</strong>`;
    return linked
      ? `<button type="button" class="admin-health-row" data-admin-open-tab="audiobook-library" data-admin-library-filter="${row.filter}">${inner}</button>`
      : `<div class="admin-health-row">${inner}</div>`;
  }).join("")}</div>`;
}

function renderAdminDashboard() {
  if (!adminDashboardPanel || !adminDashboardGrid || !adminDashboardTags || !adminDashboardRecent || !adminDashboardUploadChart || !adminDashboardHealth || !adminDashboardActivity) return;

  const visible = isAdminView() && canManageUsers() && activeAdminTab() === "dashboard";
  const tooltip = document.querySelector("#admin-chart-tooltip");
  if (tooltip) tooltip.hidden = true;
  adminDashboardPanel.classList.toggle("hidden", !visible);
  if (!visible) return;

  const dashboard = state.admin.dashboard;
  if (!dashboard) {
    adminDashboardGrid.innerHTML = `<p class="users-empty">Loading dashboard...</p>`;
    [adminDashboardTags, adminDashboardRecent, adminDashboardUploadChart, adminDashboardHealth, adminDashboardActivity].forEach((node) => { node.innerHTML = ""; });
    return;
  }

  const metrics = buildAdminDashboardMetrics(dashboard);
  adminDashboardGrid.innerHTML = metrics.map((metric) => `
    <article class="admin-dashboard-card" data-accent="${metric.accent}">
      <div class="admin-dashboard-card-head"><span class="admin-dashboard-card-icon">${metric.icon}</span><span class="admin-dashboard-label">${escapeHTML(metric.label)}</span></div>
      <strong class="admin-dashboard-value">${escapeHTML(metric.value)}</strong>
      <span class="admin-dashboard-note" data-trend="${metric.trend}">${metric.trend === "up" ? "&#8593; " : metric.trend === "down" ? "&#8595; " : ""}${escapeHTML(metric.note)}</span>
      ${metric.seriesKey ? buildAdminMetricSparkline(metric.series, metric.seriesKey, metric.seriesLabel, metric.valueFormatter) : ""}
      ${metric.meter != null ? `<div class="admin-disk-meter" role="meter" aria-label="Disk used" aria-valuemin="0" aria-valuemax="100" aria-valuenow="${Math.round(metric.meter * 100)}"><i style="width:${(metric.meter * 100).toFixed(1)}%"></i></div>` : ""}
    </article>
  `).join("");

  adminDashboardUploadChart.innerHTML = buildAdminUploadChart(dashboard.upload_series);

  const canOpenTab = (tab) => isValidAdminTab(tab);
  const queuesNode = document.querySelector("#admin-dashboard-queues");
  if (queuesNode) queuesNode.innerHTML = buildAdminWorkQueues(dashboard.work_queues, canOpenTab);
  const audiobookActivity = dashboard.audiobook_activity || null;
  const audiobookRow = document.querySelector("#admin-dashboard-audiobooks");
  if (audiobookRow) audiobookRow.classList.toggle("hidden", !audiobookActivity);
  if (audiobookActivity) {
    const listeningNode = document.querySelector("#admin-dashboard-listening");
    const audiobookHealthNode = document.querySelector("#admin-dashboard-audiobook-health");
    if (listeningNode) listeningNode.innerHTML = buildAdminListening(audiobookActivity);
    if (audiobookHealthNode) audiobookHealthNode.innerHTML = buildAdminAudiobookHealth(audiobookActivity.health, canOpenTab);
  }

  adminDashboardTags.innerHTML = buildAdminTopTags(dashboard.top_tags);
  bindAdminChartTooltips(adminDashboardPanel);

  const health = Array.isArray(dashboard.system_health) ? dashboard.system_health : [];
  const healthyCount = health.filter((entry) => entry.healthy).length;
  adminDashboardHealth.innerHTML = `
    <div class="admin-health-summary" data-healthy="${healthyCount === health.length}"><strong>${healthyCount === health.length ? "All systems operational" : `${healthyCount} of ${health.length} healthy`}</strong><span title="${escapeHTML(formatDateTimeZoned(dashboard.generated_at))}">Updated ${escapeHTML(formatRelativeTime(dashboard.generated_at))}</span></div>
    <div class="admin-health-list">${health.map((entry) => `<div><span>${escapeHTML(entry.name || "Service")}</span><strong data-healthy="${!!entry.healthy}">${escapeHTML(entry.status || "Unknown")}</strong></div>`).join("")}</div>
  `;

  const activity = Array.isArray(dashboard.recent_activity) ? dashboard.recent_activity : [];
  const uploadActors = new Map(activity.filter((event) => event.action === "uploaded").map((event) => [event.meme_id, event.actor?.display_name || event.actor?.username || "Unknown"]));
  const recentMemes = Array.isArray(dashboard.recent_memes) ? dashboard.recent_memes : [];
  if (!recentMemes.length) {
    adminDashboardRecent.innerHTML = `<p class="users-empty">No memes uploaded yet.</p>`;
  } else {
    adminDashboardRecent.innerHTML = `<div class="admin-upload-table-head"><span>Meme</span><span>Uploaded by</span><span>Tags</span><span>Size</span><span>Uploaded</span></div>${recentMemes.map((meme) => `
      <button class="admin-dashboard-recent-row" type="button" data-admin-meme-id="${escapeHTML(meme.id || "")}" title="Open ${escapeHTML(meme.original_name || "Unknown meme")}">
        <span class="admin-recent-meme"><span class="admin-recent-thumb">${meme.preview_path ? `<img src="${escapeHTML(meme.preview_path)}" alt="" />` : "FILE"}</span><strong title="${escapeHTML(meme.original_name || "Unknown meme")}">${escapeHTML(meme.original_name || "Unknown meme")}</strong></span>
        <span>${escapeHTML(uploadActors.get(meme.id) || "Unknown")}</span>
        <span class="admin-recent-tags">${(meme.tags || []).slice(0, 2).map((tag) => `<span class="tag-chip">${escapeHTML(tag)}</span>`).join("") || "&mdash;"}</span>
        <span>${escapeHTML(formatSize(Number(meme.size_bytes || 0)))}</span>
        <span title="${escapeHTML(formatDateTimeZoned(meme.created_at))}">${escapeHTML(formatRelativeTime(meme.created_at))}</span>
      </button>
    `).join("")}`;
  }

  if (!activity.length) {
    adminDashboardActivity.innerHTML = `<p class="users-empty">No tracked activity yet.</p>`;
  } else {
    const actionLabels = { uploaded: "uploaded", favorited: "favorited", unfavorited: "unfavorited", deleted: "deleted", delete_requested: "requested deletion of", tag_added: "tagged", tag_removed: "untagged" };
    adminDashboardActivity.innerHTML = activity.slice(0, 7).map((event) => `
      <button class="admin-activity-row" type="button" data-admin-meme-id="${escapeHTML(event.meme_id || "")}" title="${escapeHTML(event.meme_original_name || event.description || "Activity")}">
        <span class="admin-activity-icon" data-action="${escapeHTML(event.action || "activity")}"></span>
        <span><strong>${escapeHTML(event.actor?.display_name || event.actor?.username || "System")}</strong> ${escapeHTML(actionLabels[event.action] || event.description || "updated")} <b>${escapeHTML(event.meme_original_name || "a meme")}</b></span>
        <time datetime="${escapeHTML(String(event.created_at || ""))}" title="${escapeHTML(formatDateTimeZoned(event.created_at))}">${escapeHTML(formatRelativeTime(event.created_at))}</time>
      </button>
    `).join("");
  }

  adminDashboardPanel.querySelectorAll("[data-admin-open-tab]").forEach((button) => button.addEventListener("click", () => {
    if (button.dataset.adminLibraryFilter != null) {
      state.admin.audiobookLibrary = typeof window.adminLibraryPresetState === "function"
        ? window.adminLibraryPresetState(state.admin.audiobookLibrary, button.dataset.adminLibraryFilter)
        : { ...(state.admin.audiobookLibrary || {}), filter: button.dataset.adminLibraryFilter, page: 1, data: null };
    }
    navigateAdminTab(button.dataset.adminOpenTab);
  }));
  document.querySelectorAll("[data-admin-meme-id]").forEach((button) => button.addEventListener("click", () => openAdminMemeByID(button.dataset.adminMemeId)));
  document.querySelectorAll("[data-admin-tag]").forEach((button) => button.addEventListener("click", () => {
    state.filters.tag = button.dataset.adminTag || "";
    state.filters.view = "library";
    loadInitialMemes().catch((error) => console.error(error));
  }));
}
