package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
	"github.com/zmb3/spotify/v2"
	spotifyauth "github.com/zmb3/spotify/v2/auth"
	"golang.org/x/oauth2"
	"github.com/joho/godotenv"
)

var (
	redirectURL string                    
	auth        *spotifyauth.Authenticator
	state        = "spotify-voting-app"
	store        = sessions.NewCookieStore([]byte("super-secret-key-change-in-production"))
	clients      = make(map[*websocket.Conn]bool)
	broadcast    = make(chan VoteUpdate)
	upgrader     = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}
	clientsMu sync.RWMutex
)

func getRedirectURL() string {
	// Check if running on Fly.io
	if appName := os.Getenv("FLY_APP_NAME"); appName != "" {
		return fmt.Sprintf("https://%s.fly.dev/callback", appName)
	}
	// Check for custom redirect URL
	if customURL := os.Getenv("REDIRECT_URL"); customURL != "" {
		return customURL
	}
	// Default to localhost
	return "http://localhost:8080/callback"
}

type Track struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Artists  string `json:"artists"`
	Album    string `json:"album"`
	ImageURL string `json:"image_url"`
	URI      string `json:"uri"`
	Votes    int    `json:"votes"`
}

type VoteUpdate struct {
	TrackID string `json:"track_id"`
	Votes   int    `json:"votes"`
}

type UserSession struct {
	Client       *spotify.Client
	Token        *oauth2.Token
	UserID       string
	TokenSource  oauth2.TokenSource
	LastRefresh  time.Time
}

type App struct {
	sessions map[string]*UserSession // sessionID -> UserSession
	votes    map[string]int          // trackID -> vote count (in-memory cache)
	db       *sql.DB                 // SQLite database
	mu       sync.RWMutex
}

func NewApp() *App {
	// Use persistent volume if available (Fly.io), otherwise local
	dbPath := "./votes.db"
	if _, err := os.Stat("/data"); err == nil {
		dbPath = "/data/votes.db"
		log.Println("📁 Using persistent volume for database: /data/votes.db")
	} else {
		log.Println("📁 Using local database: ./votes.db")
	}

	// Initialize SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}

	// Create votes table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS votes (
			track_id TEXT PRIMARY KEY,
			vote_count INTEGER NOT NULL DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal("Failed to create votes table:", err)
	}

	// Create sessions table for persistence across restarts
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			session_id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			access_token TEXT NOT NULL,
			refresh_token TEXT,
			token_expiry TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal("Failed to create sessions table:", err)
	}

	app := &App{
		sessions: make(map[string]*UserSession),
		votes:    make(map[string]int),
		db:       db,
	}

	// Load existing votes from database
	app.loadVotesFromDB()

	// Load existing sessions from database
	app.loadSessionsFromDB()

	// Start token refresh goroutine
	go app.refreshTokensPeriodically()

	// Periodic database sync (every 30 seconds)
	go app.syncVotesToDBPeriodically()

	return app
}

func (app *App) loadSessionsFromDB() {
	rows, err := app.db.Query("SELECT session_id, user_id, access_token, refresh_token, token_expiry FROM sessions")
	if err != nil {
		log.Printf("⚠️  Failed to load sessions from database: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	expired := 0
	for rows.Next() {
		var sessionID, userID, accessToken string
		var refreshToken sql.NullString
		var tokenExpiry time.Time

		if err := rows.Scan(&sessionID, &userID, &accessToken, &refreshToken, &tokenExpiry); err != nil {
			log.Printf("⚠️  Error scanning session row: %v", err)
			continue
		}

		// Skip expired sessions (expired more than 1 hour ago to allow for refresh)
		if tokenExpiry.Before(time.Now().Add(-1 * time.Hour)) {
			expired++
			continue
		}

		// Recreate token and client
		token := &oauth2.Token{
			AccessToken: accessToken,
			Expiry:      tokenExpiry,
			TokenType:   "Bearer",
		}
		
		if refreshToken.Valid {
			token.RefreshToken = refreshToken.String
		}

		// Create token source for refresh
		ctx := context.Background()
		tokenSource := auth.Client(ctx, token).Transport.(*oauth2.Transport).Source

		// Create Spotify client
		httpClient := oauth2.NewClient(ctx, tokenSource)
		client := spotify.New(httpClient)

		// Store in memory
		app.sessions[sessionID] = &UserSession{
			Client:      client,
			Token:       token,
			UserID:      userID,
			TokenSource: tokenSource,
			LastRefresh: time.Now(),
		}
		count++
	}

	log.Printf("📊 Loaded %d sessions from database (%d expired and skipped)", count, expired)
}

func (app *App) saveSessionToDB(sessionID string, session *UserSession) error {
	refreshToken := sql.NullString{}
	if session.Token.RefreshToken != "" {
		refreshToken.Valid = true
		refreshToken.String = session.Token.RefreshToken
	}

	_, err := app.db.Exec(`
		INSERT INTO sessions (session_id, user_id, access_token, refresh_token, token_expiry, updated_at) 
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(session_id) 
		DO UPDATE SET 
			access_token = ?,
			refresh_token = ?,
			token_expiry = ?,
			updated_at = CURRENT_TIMESTAMP
	`, sessionID, session.UserID, session.Token.AccessToken, refreshToken, session.Token.Expiry,
		session.Token.AccessToken, refreshToken, session.Token.Expiry)
	
	return err
}

func (app *App) loadVotesFromDB() {
	rows, err := app.db.Query("SELECT track_id, vote_count FROM votes")
	if err != nil {
		log.Printf("⚠️  Failed to load votes from database: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var trackID string
		var voteCount int
		if err := rows.Scan(&trackID, &voteCount); err != nil {
			log.Printf("⚠️  Error scanning vote row: %v", err)
			continue
		}
		app.votes[trackID] = voteCount
		count++
	}

	log.Printf("📊 Loaded %d votes from database", count)
}

func (app *App) syncVotesToDB(trackID string, votes int) error {
	_, err := app.db.Exec(`
		INSERT INTO votes (track_id, vote_count, updated_at) 
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(track_id) 
		DO UPDATE SET vote_count = ?, updated_at = CURRENT_TIMESTAMP
	`, trackID, votes, votes)
	return err
}

func (app *App) syncVotesToDBPeriodically() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		app.mu.RLock()
		votesToSync := make(map[string]int)
		for trackID, votes := range app.votes {
			votesToSync[trackID] = votes
		}
		app.mu.RUnlock()

		// Sync all votes to database
		for trackID, votes := range votesToSync {
			if err := app.syncVotesToDB(trackID, votes); err != nil {
				log.Printf("⚠️  Failed to sync votes for track %s: %v", trackID, err)
			}
		}

		if len(votesToSync) > 0 {
			log.Printf("💾 Synced %d votes to database", len(votesToSync))
		}
	}
}

func (app *App) refreshTokensPeriodically() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		app.mu.Lock()
		for sessionID, session := range app.sessions {
			// Refresh if token is close to expiry (within 5 minutes)
			if session.Token.Expiry.Before(time.Now().Add(5 * time.Minute)) {
				log.Printf("🔄 Refreshing token for session: %s (user: %s)", sessionID, session.UserID)
				
				newToken, err := session.TokenSource.Token()
				if err != nil {
					log.Printf("❌ Failed to refresh token for %s: %v", session.UserID, err)
					continue
				}
				
				session.Token = newToken
				session.LastRefresh = time.Now()
				
				// Create new client with refreshed token
				httpClient := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(newToken))
				session.Client = spotify.New(httpClient)
				
				log.Printf("✅ Token refreshed for %s, expires at: %s", session.UserID, newToken.Expiry)
			}
		}
		app.mu.Unlock()
	}
}

func (app *App) getSession(r *http.Request) (*UserSession, error) {
	session, err := store.Get(r, "spotify-session")
	if err != nil {
		log.Printf("Session get error: %v", err)
		return nil, err
	}

	sessionID, ok := session.Values["id"].(string)
	if !ok || sessionID == "" {
		log.Printf("No session ID in cookie")
		return nil, fmt.Errorf("no session ID")
	}

	log.Printf("Looking up session: %s", sessionID)

	app.mu.RLock()
	userSession, exists := app.sessions[sessionID]
	app.mu.RUnlock()

	if !exists {
		log.Printf("Session not found in memory: %s", sessionID)
		return nil, fmt.Errorf("session not found")
	}

	log.Printf("Session found for user: %s", userSession.UserID)
	return userSession, nil
}

func (app *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "spotify-session")
	
	// Generate a unique session ID
	sessionID := fmt.Sprintf("session-%d", time.Now().UnixNano())
	session.Values["id"] = sessionID
	session.Save(r, w)

	url := auth.AuthURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (app *App) handleCallback(w http.ResponseWriter, r *http.Request) {
	log.Printf("🔄 Callback received from: %s", r.RemoteAddr)
	
	// Check state first
	receivedState := r.FormValue("state")
	log.Printf("State check - Expected: %s, Received: %s", state, receivedState)
	
	if receivedState != state {
		http.NotFound(w, r)
		log.Printf("❌ ERROR: State mismatch: %s != %s", receivedState, state)
		return
	}

	// Get token
	log.Printf("Attempting to get token...")
	token, err := auth.Token(r.Context(), state, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("Couldn't get token: %v", err), http.StatusForbidden)
		log.Printf("❌ ERROR: Token error: %v", err)
		log.Printf("Request URL: %s", r.URL.String())
		log.Printf("Request headers: %v", r.Header)
		return
	}
	log.Printf("✅ Token obtained successfully")

	// Create new client with fresh token
	log.Printf("Creating Spotify client...")
	httpClient := auth.Client(r.Context(), token)
	client := spotify.New(httpClient)
	
	// Try to get current user with retries
	var user *spotify.PrivateUser
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		log.Printf("Attempting to get current user (attempt %d/%d)...", i+1, maxRetries)
		user, err = client.CurrentUser(r.Context())
		if err == nil {
			break
		}
		log.Printf("⚠️  Attempt %d failed: %v", i+1, err)
		if i < maxRetries-1 {
			time.Sleep(time.Second * 2)
		}
	}
	
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to get user after %d attempts: %v", maxRetries, err)
		http.Error(w, errorMsg, http.StatusInternalServerError)
		log.Printf("❌ ERROR: %s", errorMsg)
		log.Printf("Token details - Access token present: %v, Expiry: %v", token.AccessToken != "", token.Expiry)
		return
	}
	
	log.Printf("✅ User retrieved: %s (Display: %s)", user.ID, user.DisplayName)

	// Get or create session
	session, err := store.Get(r, "spotify-session")
	if err != nil {
		log.Printf("⚠️  Session get error (creating new): %v", err)
		session, _ = store.New(r, "spotify-session")
	}
	
	sessionID, ok := session.Values["id"].(string)
	if !ok || sessionID == "" {
		sessionID = fmt.Sprintf("session-%d", time.Now().UnixNano())
		log.Printf("📝 Creating new session ID: %s", sessionID)
	}
	
	session.Values["id"] = sessionID
	err = session.Save(r, w)
	if err != nil {
		log.Printf("⚠️  Session save error: %v", err)
	}

	// Create token source for automatic refresh
	tokenSource := auth.Client(r.Context(), token).Transport.(*oauth2.Transport).Source

	// Store user session with token source
	app.mu.Lock()
	app.sessions[sessionID] = &UserSession{
		Client:      client,
		Token:       token,
		UserID:      string(user.ID),
		TokenSource: tokenSource,
		LastRefresh: time.Now(),
	}
	app.mu.Unlock()

	// Save session to database for persistence
	if err := app.saveSessionToDB(sessionID, app.sessions[sessionID]); err != nil {
		log.Printf("⚠️  Failed to save session to database: %v", err)
	} else {
		log.Printf("💾 Session saved to database")
	}

	log.Printf("✅ User logged in successfully: %s (ID: %s) - Session: %s", user.DisplayName, user.ID, sessionID)
	log.Printf("🔑 Token expires at: %s", token.Expiry)
	log.Printf("📊 Total active sessions: %d", len(app.sessions))
	
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (app *App) handleGetPlaylists(w http.ResponseWriter, r *http.Request) {
	userSession, err := app.getSession(r)
	if err != nil {
		log.Printf("ERROR: Not authenticated: %v", err)
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	log.Printf("Fetching playlists for user: %s", userSession.UserID)
	playlists, err := userSession.Client.CurrentUsersPlaylists(r.Context(), spotify.Limit(50))
	if err != nil {
		log.Printf("ERROR: Failed to get playlists: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("SUCCESS: Found %d playlists for user %s", len(playlists.Playlists), userSession.UserID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(playlists)
}

func (app *App) handleGetPlaylistTracks(w http.ResponseWriter, r *http.Request) {
	userSession, err := app.getSession(r)
	if err != nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	vars := mux.Vars(r)
	playlistID := spotify.ID(vars["id"])

	tracks := []Track{}
	offset := 0
	limit := 100

	for {
		playlistTracks, err := userSession.Client.GetPlaylistItems(
			r.Context(),
			playlistID,
			spotify.Limit(limit),
			spotify.Offset(offset),
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		for _, item := range playlistTracks.Items {
			if item.Track.Track == nil {
				continue
			}
			track := item.Track.Track

			artists := ""
			for i, artist := range track.Artists {
				if i > 0 {
					artists += ", "
				}
				artists += artist.Name
			}

			imageURL := ""
			if len(track.Album.Images) > 0 {
				imageURL = track.Album.Images[0].URL
			}

			app.mu.RLock()
			votes := app.votes[string(track.ID)]
			app.mu.RUnlock()

			tracks = append(tracks, Track{
				ID:       string(track.ID),
				Name:     track.Name,
				Artists:  artists,
				Album:    track.Album.Name,
				ImageURL: imageURL,
				URI:      string(track.URI),
				Votes:    votes,
			})
		}

		if len(playlistTracks.Items) < limit {
			break
		}
		offset += limit
	}

	// Sort by votes (highest first)
	sort.Slice(tracks, func(i, j int) bool {
		return tracks[i].Votes > tracks[j].Votes
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tracks)
}

func (app *App) handleVote(w http.ResponseWriter, r *http.Request) {
	// Don't require authentication for voting - anyone can vote!
	var req struct {
		TrackID string `json:"track_id"`
		Vote    int    `json:"vote"` // 1 for upvote, -1 for downvote
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if req.Vote != 1 && req.Vote != -1 {
		http.Error(w, "Vote must be 1 or -1", http.StatusBadRequest)
		return
	}

	app.mu.Lock()
	app.votes[req.TrackID] += req.Vote
	newVotes := app.votes[req.TrackID]
	app.mu.Unlock()

	// Immediately sync to database
	if err := app.syncVotesToDB(req.TrackID, newVotes); err != nil {
		log.Printf("⚠️  Failed to sync vote to database: %v", err)
	}

	// Broadcast vote update to all connected clients
	update := VoteUpdate{
		TrackID: req.TrackID,
		Votes:   newVotes,
	}
	broadcast <- update

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"votes":   newVotes,
	})
}

func (app *App) handlePlayTrack(w http.ResponseWriter, r *http.Request) {
	userSession, err := app.getSession(r)
	if err != nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var req struct {
		URI      string `json:"uri"`
		DeviceID string `json:"device_id,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	// First, check available devices
	devices, err := userSession.Client.PlayerDevices(ctx)
	if err != nil {
		log.Printf("⚠️  Failed to get devices for %s: %v", userSession.UserID, err)
		http.Error(w, "Failed to get Spotify devices", http.StatusInternalServerError)
		return
	}

	if len(devices) == 0 {
		log.Printf("⚠️  No active devices found for %s", userSession.UserID)
		http.Error(w, "No active Spotify devices found. Please open Spotify on your phone, computer, or web player.", http.StatusBadRequest)
		return
	}

	// Find an active device or use the first available one
	var targetDeviceID *spotify.ID
	var activeDevice *spotify.PlayerDevice
	
	for i := range devices {
		if devices[i].Active {
			activeDevice = &devices[i]
			break
		}
	}

	// If no active device, use the first available one
	if activeDevice == nil {
		activeDevice = &devices[0]
		log.Printf("ℹ️  No active device, using: %s (%s)", activeDevice.Name, activeDevice.Type)
	} else {
		log.Printf("ℹ️  Using active device: %s (%s)", activeDevice.Name, activeDevice.Type)
	}

	targetDeviceID = &activeDevice.ID

	// Try to play on the device
	playOptions := &spotify.PlayOptions{
		URIs:     []spotify.URI{spotify.URI(req.URI)},
		DeviceID: targetDeviceID,
	}

	err = userSession.Client.PlayOpt(ctx, playOptions)
	if err != nil {
		// If it fails, try to transfer playback to the device first
		log.Printf("⚠️  Play failed, attempting to transfer playback to %s", activeDevice.Name)
		
		transferErr := userSession.Client.TransferPlayback(ctx, *targetDeviceID, true)
		if transferErr != nil {
			log.Printf("❌ Failed to transfer playback for %s: %v", userSession.UserID, transferErr)
			http.Error(w, fmt.Sprintf("Failed to play track: %v. Try playing something manually in Spotify first.", err), http.StatusInternalServerError)
			return
		}

		// Wait a moment for transfer to complete
		time.Sleep(500 * time.Millisecond)

		// Try playing again
		err = userSession.Client.PlayOpt(ctx, playOptions)
		if err != nil {
			log.Printf("❌ Failed to play after transfer for %s: %v", userSession.UserID, err)
			http.Error(w, fmt.Sprintf("Failed to play track: %v", err), http.StatusInternalServerError)
			return
		}
	}

	log.Printf("▶️  User %s played track: %s on device: %s", userSession.UserID, req.URI, activeDevice.Name)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"device":     activeDevice.Name,
		"device_type": activeDevice.Type,
	})
}

// Add new endpoint to get available devices
func (app *App) handleGetDevices(w http.ResponseWriter, r *http.Request) {
	userSession, err := app.getSession(r)
	if err != nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	ctx := context.Background()
	devices, err := userSession.Client.PlayerDevices(ctx)
	if err != nil {
		log.Printf("Failed to get devices: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(devices)
}

// Delete track from playlist
func (app *App) handleDeleteTrack(w http.ResponseWriter, r *http.Request) {
	userSession, err := app.getSession(r)
	if err != nil {
		http.Error(w, "Not authenticated", http.StatusUnauthorized)
		return
	}

	var req struct {
		PlaylistID string `json:"playlist_id"`
		TrackURI   string `json:"track_uri"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx := context.Background()

	// Use the Spotify Web API directly to remove tracks
	// The library's method has type issues, so we use HTTP client directly
	apiURL := fmt.Sprintf("https://api.spotify.com/v1/playlists/%s/tracks", req.PlaylistID)
	
	requestBody := map[string]interface{}{
		"tracks": []map[string]string{
			{
				"uri": req.TrackURI,
			},
		},
	}
	
	bodyBytes, err := json.Marshal(requestBody)
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", apiURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}

	// Add authorization header
	httpReq.Header.Set("Authorization", "Bearer "+userSession.Token.AccessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		log.Printf("❌ Failed to remove track from playlist for %s: %v", userSession.UserID, err)
		http.Error(w, fmt.Sprintf("Failed to remove track: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("❌ Spotify API error: %d - %s", resp.StatusCode, string(body))
		http.Error(w, fmt.Sprintf("Spotify API error: %s", string(body)), resp.StatusCode)
		return
	}

	log.Printf("🗑️  User %s removed track %s from playlist %s", 
		userSession.UserID, req.TrackURI, req.PlaylistID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (app *App) handleGetAuthStatus(w http.ResponseWriter, r *http.Request) {
	userSession, err := app.getSession(r)
	
	response := map[string]interface{}{
		"authenticated": err == nil,
	}
	
	if err == nil && userSession != nil {
		response["user_id"] = userSession.UserID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (app *App) handleLogout(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "spotify-session")
	sessionID, ok := session.Values["id"].(string)

	if ok && sessionID != "" {
		app.mu.Lock()
		userSession := app.sessions[sessionID]
		delete(app.sessions, sessionID)
		app.mu.Unlock()
		
		// Delete from database
		_, err := app.db.Exec("DELETE FROM sessions WHERE session_id = ?", sessionID)
		if err != nil {
			log.Printf("⚠️  Failed to delete session from database: %v", err)
		}
		
		if userSession != nil {
			log.Printf("🚪 User logged out: %s - Session: %s", userSession.UserID, sessionID)
		}
	}

	// Clear session
	session.Values["id"] = ""
	session.Options.MaxAge = -1
	session.Save(r, w)

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}
	defer conn.Close()

	clientsMu.Lock()
	clients[conn] = true
	clientsMu.Unlock()

	defer func() {
		clientsMu.Lock()
		delete(clients, conn)
		clientsMu.Unlock()
	}()

	// Keep connection alive
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func handleBroadcast() {
	for {
		update := <-broadcast
		clientsMu.RLock()
		for client := range clients {
			err := client.WriteJSON(update)
			if err != nil {
				log.Printf("WebSocket error: %v", err)
				client.Close()
				clientsMu.RUnlock()
				clientsMu.Lock()
				delete(clients, client)
				clientsMu.Unlock()
				clientsMu.RLock()
			}
		}
		clientsMu.RUnlock()
	}
}

func main() {
	// Set Spotify credentials from environment variables
	// Load environment variables from .env file if present
	if _, err := os.Stat(".env"); err == nil {
		if err := godotenv.Load(".env"); err != nil {
			log.Fatalf("Failed to load .env file: %v", err)
		}
	}

	clientID := os.Getenv("SPOTIFY_ID")
	clientSecret := os.Getenv("SPOTIFY_SECRET")

	if clientID == "" || clientSecret == "" {
		log.Fatal("SPOTIFY_ID and SPOTIFY_SECRET environment variables must be set")
	}

	redirectURL = getRedirectURL()
	auth = spotifyauth.New(
		spotifyauth.WithRedirectURL(redirectURL),
		spotifyauth.WithScopes(
			spotifyauth.ScopeUserReadPrivate,
			spotifyauth.ScopeUserReadEmail,
			spotifyauth.ScopePlaylistReadPrivate,
			spotifyauth.ScopePlaylistModifyPublic,
			spotifyauth.ScopePlaylistModifyPrivate,
			spotifyauth.ScopeUserModifyPlaybackState,
			spotifyauth.ScopeUserReadPlaybackState,
			spotifyauth.ScopeStreaming,
		),
	)


	app := NewApp()
	go handleBroadcast()

	r := mux.NewRouter()

	// API routes
	r.HandleFunc("/login", app.handleLogin).Methods("GET")
	r.HandleFunc("/callback", app.handleCallback).Methods("GET")
	r.HandleFunc("/logout", app.handleLogout).Methods("GET")
	r.HandleFunc("/api/auth-status", app.handleGetAuthStatus).Methods("GET")
	r.HandleFunc("/api/playlists", app.handleGetPlaylists).Methods("GET")
	r.HandleFunc("/api/playlist/{id}/tracks", app.handleGetPlaylistTracks).Methods("GET")
	r.HandleFunc("/api/vote", app.handleVote).Methods("POST")
	r.HandleFunc("/api/play", app.handlePlayTrack).Methods("POST")
	r.HandleFunc("/api/devices", app.handleGetDevices).Methods("GET")
	r.HandleFunc("/api/delete-track", app.handleDeleteTrack).Methods("POST")
	r.HandleFunc("/ws", handleWebSocket)

	// Serve static files
	fs := http.FileServer(http.Dir("./static"))
	r.PathPrefix("/").Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("📁 Serving: %s", r.URL.Path)
		fs.ServeHTTP(w, r)
	}))

	server := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Println("🎵 Multi-user Spotify Voting App starting on http://localhost:8080")
	log.Println("👥 Multiple users can now login and vote simultaneously!")
	log.Printf("🔗 Redirect URL: %s", redirectURL)
	
	// Check if static directory exists
	if _, err := os.Stat("./static"); os.IsNotExist(err) {
		log.Println("⚠️  WARNING: ./static directory does not exist!")
		log.Println("Current working directory:", mustGetWd())
		log.Println("Trying to list files...")
		files, _ := os.ReadDir(".")
		for _, f := range files {
			log.Printf("  - %s", f.Name())
		}
	} else {
		log.Println("✅ Static directory found")
	}
	
	log.Fatal(server.ListenAndServe())
}

func mustGetWd() string {
	wd, _ := os.Getwd()
	return wd
}
