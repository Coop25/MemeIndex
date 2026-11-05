package client

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// adminUserActivity is one row of the admin User Activity page.
type adminUserActivity struct {
	UserID             string  `json:"user_id"`
	DisplayName        string  `json:"display_name"`
	Username           string  `json:"username"`
	AvatarURL          string  `json:"avatar_url"`
	MemesUploaded      int     `json:"memes_uploaded"`
	AudiobooksUploaded int     `json:"audiobooks_uploaded"`
	Favorites          int     `json:"favorites"`
	Seconds30d         float64 `json:"seconds_30d"`
	SecondsAllTime     float64 `json:"seconds_all_time"`
	BooksFinished      int     `json:"books_finished"`
	LastActiveAt       int64   `json:"last_active_at"`
}

func (s *Server) handleAdminUserActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	rows := []adminUserActivity{}
	if s.users == nil {
		writeJSON(w, http.StatusOK, map[string]any{"users": rows})
		return
	}
	users, err := s.users.ListUsers(ctx)
	if err != nil {
		log.Printf("admin user activity users failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "user activity failed"})
		return
	}

	// Each source is best-effort: a store that cannot report a figure leaves
	// it at zero rather than failing the whole page.
	uploads, err := s.managers.CountAuditActionsByActor("uploaded")
	if err != nil {
		log.Printf("admin user activity uploads failed: %v", err)
	}
	favorites, err := s.managers.CountFavoritesByUser()
	if err != nil {
		log.Printf("admin user activity favorites failed: %v", err)
	}
	var books map[string]adminUserAudiobookTotals
	if s.audiobooks != nil {
		books, err = s.audiobooks.AdminUserAudiobookTotals(ctx, time.Now().UTC().AddDate(0, 0, -30))
		if err != nil {
			log.Printf("admin user activity audiobooks failed: %v", err)
		}
	}

	for _, user := range users {
		name := strings.TrimSpace(user.DisplayName)
		if name == "" {
			name = strings.TrimSpace(user.Username)
		}
		b := books[user.UserID]
		rows = append(rows, adminUserActivity{
			UserID:             user.UserID,
			DisplayName:        name,
			Username:           user.Username,
			AvatarURL:          user.AvatarURL,
			MemesUploaded:      uploads[user.UserID],
			AudiobooksUploaded: b.Uploaded,
			Favorites:          favorites[user.UserID],
			Seconds30d:         b.Seconds30d,
			SecondsAllTime:     b.SecondsAllTime,
			BooksFinished:      b.Finished,
			LastActiveAt:       user.LastActiveAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": rows})
}
