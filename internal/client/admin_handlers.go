package client

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"memeindex/internal/accessor"
	"memeindex/internal/manager"
	"memeindex/internal/tagsuggest"
)

type adminSystemHealth struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Healthy bool   `json:"healthy"`
}

type adminDashboardResponse struct {
	manager.AdminDashboardStats
	UserCount         int                             `json:"user_count"`
	ActiveUsers30D    int                             `json:"active_users_30d"`
	NewUsers30D       int                             `json:"new_users_30d"`
	ActiveShares      int                             `json:"active_share_count"`
	RecentActivity    []accessor.GlobalMemeAuditEntry `json:"recent_activity"`
	BackupStatus      *backupJobStatus                `json:"backup_status,omitempty"`
	AudiobookStats    *adminAudiobookStats            `json:"audiobook_stats,omitempty"`
	AudiobookActivity *adminAudiobookActivity         `json:"audiobook_activity,omitempty"`
	WorkQueues        adminWorkQueues                 `json:"work_queues"`
	Disk              *adminDiskStats                 `json:"disk,omitempty"`
	LastBackup        *adminLastBackup                `json:"last_backup,omitempty"`
	TotalStorageBytes int64                           `json:"total_storage_bytes"`
	SystemHealth      []adminSystemHealth             `json:"system_health"`
	GeneratedAt       time.Time                       `json:"generated_at"`
}

func (s *Server) handlePendingDeleteQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	offset := parseQueryInt(r, "offset", 0)
	limit := parseQueryInt(r, "limit", 50)
	records, err := s.managers.ListPendingDeletes(offset, limit)
	if err != nil {
		log.Printf("list pending deletes failed: %v", err)
		http.Error(w, "failed to load pending deletes", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, s.protectPendingDeletesForResponse(r, records))
}

func (s *Server) handleAdminDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.managers.AdminDashboard()
	for index := range stats.RecentMemes {
		stats.RecentMemes[index].FilePath = s.protectAssetPathForResponse(r, stats.RecentMemes[index].FilePath)
		stats.RecentMemes[index].PreviewPath = s.protectAssetPathForResponse(r, stats.RecentMemes[index].PreviewPath)
	}

	response := adminDashboardResponse{
		AdminDashboardStats: stats,
		RecentActivity:      []accessor.GlobalMemeAuditEntry{},
		SystemHealth: []adminSystemHealth{
			{Name: "Web Server", Status: "Operational", Healthy: true},
		},
		GeneratedAt: time.Now().UTC(),
	}
	if shares, err := s.managers.ListActiveMemeShares(response.GeneratedAt); err != nil {
		log.Printf("admin dashboard active shares failed: %v", err)
	} else {
		response.ActiveShares = len(shares)
	}

	usersHealthy := true
	if s.users != nil {
		users, err := s.users.ListUsers(r.Context())
		if err != nil {
			usersHealthy = false
			log.Printf("admin dashboard users failed: %v", err)
		} else {
			response.UserCount = len(users)
			activeCutoff := time.Now().Add(-30 * 24 * time.Hour).Unix()
			newCutoff := time.Now().Add(-30 * 24 * time.Hour)
			newUsersByDate := map[string]int{}
			for _, user := range users {
				if user.LastActiveAt >= activeCutoff {
					response.ActiveUsers30D++
				}
				if user.CreatedAt.After(newCutoff) {
					response.NewUsers30D++
				}
				newUsersByDate[user.CreatedAt.UTC().Format("2006-01-02")]++
			}

			recentUsers := 0
			for _, point := range response.MetricSeries {
				recentUsers += newUsersByDate[point.Date]
			}
			runningUsers := max(0, response.UserCount-recentUsers)
			for index := range response.MetricSeries {
				runningUsers += newUsersByDate[response.MetricSeries[index].Date]
				response.MetricSeries[index].Users = runningUsers
			}
		}
	}
	databaseStatus := "Local file store"
	if strings.TrimSpace(s.config.DatabaseURL) != "" {
		databaseStatus = "Connected"
	}
	response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Database", Status: databaseStatus, Healthy: usersHealthy})

	if activity, err := s.managers.ListAuditFeed(0, 10, ""); err != nil {
		log.Printf("admin dashboard activity failed: %v", err)
		response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Activity Tracking", Status: "Unavailable", Healthy: false})
	} else {
		protected := s.protectAuditFeedForResponse(r, activity)
		response.RecentActivity = protected.Events
		response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Activity Tracking", Status: "Operational", Healthy: true})
	}

	storageHealthy := false
	if _, err := os.Stat(s.managers.UploadDir()); err == nil {
		storageHealthy = true
	}
	response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Storage", Status: map[bool]string{true: "Accessible", false: "Unavailable"}[storageHealthy], Healthy: storageHealthy})

	if s.backup != nil {
		status := s.backup.exportStatus()
		response.BackupStatus = &status
		backupHealthy := status.State != "failed"
		backupState := strings.TrimSpace(status.State)
		if backupState == "" {
			backupState = "idle"
		}
		response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Backup Service", Status: strings.ToUpper(backupState[:1]) + backupState[1:], Healthy: backupHealthy})
	} else {
		response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Backup Service", Status: "Requires PostgreSQL", Healthy: false})
	}
	response.TotalStorageBytes = stats.TotalSizeBytes
	audiobookStatsOK := true
	if audiobookStats, err := s.cachedAudiobookAdminStats(r.Context()); err != nil {
		audiobookStatsOK = false
		log.Printf("admin dashboard audiobook stats failed: %v", err)
	} else if audiobookStats != nil {
		response.AudiobookStats = audiobookStats
		response.TotalStorageBytes += audiobookStats.TotalBytes
	}

	response.WorkQueues.SuggestedTags = stats.PendingSuggestionMemes
	if pending, err := s.managers.ListPendingDeletes(0, 1); err != nil {
		log.Printf("admin dashboard pending deletes failed: %v", err)
	} else {
		response.WorkQueues.DeleteQueue = pending.Total
	}
	if s.linkRetries != nil {
		retries := s.linkRetries.Snapshot()
		response.WorkQueues.LinkRetries = len(retries.Queued)
		response.WorkQueues.RejectedLinks = len(retries.Rejected)
	}
	audiobookHealth := adminSystemHealth{Name: "Audiobooks", Status: "Operational", Healthy: true}
	activity, activityErr := s.cachedAudiobookActivity(r.Context())
	if activityErr != nil {
		log.Printf("admin dashboard audiobook activity failed: %v", activityErr)
	} else if activity != nil {
		if len(activity.TopListeners) > 0 {
			names := s.adminUserNames(r.Context())
			for index := range activity.TopListeners {
				name := names[activity.TopListeners[index].UserID]
				if name == "" {
					name = activity.TopListeners[index].UserID
				}
				activity.TopListeners[index].DisplayName = name
			}
		}
		response.AudiobookActivity = activity
		response.WorkQueues.ConversionQueue = activity.Health.AwaitingConversion
		if activity.Partial {
			audiobookHealth = adminSystemHealth{Name: "Audiobooks", Status: "Degraded", Healthy: false}
		}
	}
	if activityErr != nil || !audiobookStatsOK {
		audiobookHealth = adminSystemHealth{Name: "Audiobooks", Status: "Unavailable", Healthy: false}
	}
	if s.audiobooks != nil {
		response.SystemHealth = append(response.SystemHealth, audiobookHealth)
	}

	diskStatus, diskHealthy := "Unknown", false
	if disk, ok := diskUsage(s.config.DataDir); ok {
		response.Disk = &disk
		diskStatus, diskHealthy = formatAdminBytes(disk.FreeBytes)+" free", !disk.Low
		if disk.Low {
			diskStatus = "Low: " + diskStatus
		}
	}
	response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Disk", Status: diskStatus, Healthy: diskHealthy})

	backupStatus, backupHealthy := "Requires PostgreSQL", false
	if s.backup != nil {
		backupStatus = "None yet"
		if last, err := s.backup.lastCompletedBackup(response.GeneratedAt); err != nil {
			log.Printf("admin dashboard last backup failed: %v", err)
			backupStatus = "Unknown"
		} else if last != nil {
			response.LastBackup = last
			backupStatus, backupHealthy = formatAdminAge(response.GeneratedAt.Sub(last.CompletedAt)), !last.Stale
		}
	}
	response.SystemHealth = append(response.SystemHealth, adminSystemHealth{Name: "Last Backup", Status: backupStatus, Healthy: backupHealthy})

	response.SystemHealth = append(response.SystemHealth,
		adminSystemHealth{Name: "Image Processing", Status: "Available", Healthy: s.managers.ThumbnailDir() != ""},
		adminSystemHealth{Name: "Tag Suggestions", Status: map[bool]string{true: "Configured", false: "Optional / disabled"}[s.config.TagSuggestions.Enabled()], Healthy: true},
	)
	if reporter, ok := s.storeForHealth().(searchIndexReporter); ok {
		searchOK := reporter.SearchIndexAvailable()
		response.SystemHealth = append(response.SystemHealth, adminSystemHealth{
			Name:    "Search Index",
			Status:  map[bool]string{true: "Operational", false: "Degraded (pg_trgm unavailable)"}[searchOK],
			Healthy: searchOK,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	offset := parseQueryInt(r, "offset", 0)
	limit := parseQueryInt(r, "limit", 100)
	events, err := s.managers.ListAuditFeed(offset, limit, strings.TrimSpace(r.URL.Query().Get("actor")))
	if err != nil {
		log.Printf("list audit logs failed: %v", err)
		http.Error(w, "failed to load audit logs", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, s.protectAuditFeedForResponse(r, events))
}

func (s *Server) handleTagHygiene(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
		switch view {
		case "", "pairs", "tags":
		default:
			http.Error(w, "invalid view (want pairs or tags)", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, s.managers.TagHygienePage(manager.TagHygienePageParams{
			View:   view,
			Offset: parseQueryInt(r, "offset", 0),
			Limit:  parseQueryInt(r, "limit", 0),
			Search: r.URL.Query().Get("q"),
		}))
	case http.MethodPost:
		var payload struct {
			SourceTag string `json:"source_tag"`
			TargetTag string `json:"target_tag"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}

		result, err := s.managers.MergeTags(payload.SourceTag, payload.TargetTag, currentAuditActor(r))
		if err != nil {
			http.Error(w, "failed to merge tags", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, result)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTagSuggestionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	reviewOffset := parseQueryInt(r, "review_offset", 0)
	reviewLimit := parseQueryInt(r, "review_limit", 50)
	writeJSON(w, http.StatusOK, s.managers.TagSuggestionQueueStatus(reviewOffset, reviewLimit))
}

func (s *Server) handleResetTagSuggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := s.managers.ResetTagSuggestionsAndRequeueUntagged(currentAuditActor(r))
	if err != nil {
		switch {
		case errors.Is(err, tagsuggest.ErrDisabled):
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "tag suggestions are disabled",
			})
		default:
			log.Printf("reset tag suggestions failed: %v", err)
			http.Error(w, "failed to reset tag suggestions", http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleRetryFailedTagSuggestions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result, err := s.managers.RetryFailedTagSuggestions(currentAuditActor(r))
	if err != nil {
		if errors.Is(err, tagsuggest.ErrDisabled) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{
				"error": "tag suggestions are disabled",
			})
			return
		}
		log.Printf("retry failed tag suggestions failed: %v", err)
		http.Error(w, "failed to retry tag suggestions", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleLinkDownloadStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.linkRetries == nil {
		http.Error(w, "link retry queue unavailable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, s.linkRetries.Snapshot())
}

func (s *Server) handleLinkDownloadActions(w http.ResponseWriter, r *http.Request) {
	if s.linkRetries == nil {
		http.Error(w, "link retry queue unavailable", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/link-downloads/")
	if !strings.HasSuffix(path, "/retry") || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSuffix(strings.TrimSuffix(path, "/retry"), "/")
	if strings.TrimSpace(id) == "" {
		http.NotFound(w, r)
		return
	}
	if !s.linkRetries.RequeueRejected(id) {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"retried": true})
}

func (s *Server) handleAdminMemeActions(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/admin/memes/")
	if path == "" {
		http.NotFound(w, r)
		return
	}

	switch {
	case r.Method == http.MethodGet && !strings.Contains(path, "/"):
		id := strings.TrimSuffix(path, "/")
		if id == "" {
			http.NotFound(w, r)
			return
		}
		s.handleAdminMemeByID(w, r, id)
	case strings.HasSuffix(path, "/audit"):
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimSuffix(path, "/audit"), "/")
		s.handleMemeAudit(w, r, id)
	case strings.HasSuffix(path, "/approve-delete"):
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimSuffix(path, "/approve-delete"), "/")
		if err := s.managers.ApprovePendingDelete(id, currentAuditActor(r)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			log.Printf("approve delete failed: %v", err)
			http.Error(w, "failed to approve delete", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(path, "/reject-delete"):
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		id := strings.TrimSuffix(strings.TrimSuffix(path, "/reject-delete"), "/")
		if err := s.managers.RejectPendingDelete(id, currentAuditActor(r)); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				http.NotFound(w, r)
				return
			}
			log.Printf("reject delete failed: %v", err)
			http.Error(w, "failed to reject delete", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleAdminMemeByID(w http.ResponseWriter, r *http.Request, id string) {
	meme, err := s.managers.GetAdminMeme(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.NotFound(w, r)
			return
		}
		log.Printf("get admin meme failed: %v", err)
		http.Error(w, "failed to load meme", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, s.protectMemeForResponse(r, meme))
}

func (s *Server) handleMemeAudit(w http.ResponseWriter, r *http.Request, id string) {
	limit := parseQueryInt(r, "limit", 100)
	audit, err := s.managers.ListMemeAudit(id, limit)
	if err != nil {
		log.Printf("list meme audit failed: %v", err)
		http.Error(w, "failed to load meme audit", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"events": audit,
	})
}
