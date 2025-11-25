# Spotify Playlist Voting App 

A **multi-user** real-time voting system for Spotify playlists where multiple users can simultaneously vote on tracks and play them directly through Spotify. Test.

## Features

- 🎵 **Spotify Integration**: Connect with your Spotify account
- 👥 **Multi-User Support**: Multiple users can login simultaneously with different accounts
- 📋 **Playlist Selection**: Choose from your personal playlists
- 👍 **Voting System**: Upvote and downvote tracks
- ▶️ **Playback Control**: Play tracks directly from the interface on your device
- 🔴 **Real-time Updates**: Live vote counts via WebSocket for all users
- 🍪 **Session-Based Auth**: Each browser maintains its own login session
- 🎨 **Modern UI**: Brutalist design with vibrant colors

## Prerequisites

- Go 1.21 or higher
- A Spotify account (Premium required for playback)
- Spotify Developer App credentials

## Setup

### 1. Create Spotify App

1. Go to [Spotify Developer Dashboard](https://developer.spotify.com/dashboard)
2. Log in with your Spotify account
3. Click "Create App"
4. Fill in:
   - **App Name**: Playlist Voting App (or any name)
   - **App Description**: A voting system for playlists
   - **Redirect URI**: `http://localhost:8080/callback`
   - **Which API/SDKs are you planning to use?**: Web API
5. Accept the terms and click "Save"
6. Click "Settings" to see your Client ID and Client Secret

### 2. Configure Environment Variables

Create a `.env` file in the project root:

```bash
SPOTIFY_ID=your_client_id_here
SPOTIFY_SECRET=your_client_secret_here
```

Or export them directly:

```bash
export SPOTIFY_ID=your_client_id_here
export SPOTIFY_SECRET=your_client_secret_here
```

### 3. Install Dependencies

```bash
go mod download
```

### 4. Run the Application

```bash
go run main.go
```

The server will start at `http://localhost:8080`

## Usage

### Multi-User Setup

**The app now supports multiple users simultaneously!**

1. **User A - Normal Browser**:
   - Open `http://localhost:8080`
   - Click "Connect Spotify" and login with Account A
   
2. **User B - Incognito Window**:
   - Open an incognito/private window
   - Go to `http://localhost:8080`
   - Click "Connect Spotify" and login with Account B

3. **User C - Different Browser**:
   - Open a different browser (e.g., Firefox if you used Chrome)
   - Go to `http://localhost:8080`
   - Click "Connect Spotify" and login with Account C

**All users can now:**
- Browse their own playlists
- Vote on the same tracks simultaneously
- See vote changes in real-time
- Play tracks on their own Spotify devices

### First Time Setup

1. Open `http://localhost:8080` in your browser
2. Click "Connect Spotify"
3. Authorize the application in the Spotify OAuth page
4. You'll be redirected back to the app

### Voting and Playing

1. **Select a Playlist**: Choose from the dropdown menu
2. **Vote on Tracks**: 
   - Click ↑ to upvote
   - Click ↓ to downvote
   - Vote counts update in real-time for all connected users
3. **Play Tracks**: 
   - Click the "▶ Play" button on any track
   - Make sure you have Spotify open on a device (desktop app, mobile, or web player)
   - The track will start playing on your active Spotify device

### Real-time Features

- All vote changes are instantly synchronized across all connected browsers
- Multiple users can vote simultaneously
- Tracks are automatically sorted by vote count (highest first)

## Architecture

### Backend (Go)

- **Web Server**: Gorilla Mux for routing
- **Session Management**: Cookie-based sessions with Gorilla Sessions
- **Multi-User Support**: Each user gets their own Spotify client stored in a session map
- **Spotify API**: zmb3/spotify library for OAuth and API calls
- **WebSockets**: Real-time vote updates via Gorilla WebSocket
- **Concurrency**: Thread-safe vote storage and session management with sync.RWMutex

### Frontend

- **Vanilla JavaScript**: No framework dependencies
- **WebSocket Client**: Real-time updates
- **Session Cookies**: Automatic authentication persistence
- **Responsive Design**: Works on desktop and mobile
- **Modern CSS**: Custom animations and gradients

### API Endpoints

- `GET /login` - Initiate Spotify OAuth
- `GET /callback` - OAuth callback
- `GET /api/auth-status` - Check authentication status
- `GET /api/playlists` - Get user's playlists
- `GET /api/playlist/{id}/tracks` - Get tracks from a playlist
- `POST /api/vote` - Submit a vote
- `POST /api/play` - Play a track
- `WS /ws` - WebSocket connection for real-time updates

## Troubleshooting

### "Failed to play track" Error

- Make sure you have Spotify Premium (playback control requires Premium)
- Ensure Spotify is open and active on at least one device
- Try playing a song manually in Spotify first to activate a device

### "Not authenticated" Error

- Clear your browser cookies and log in again
- Check that your Spotify credentials are correct
- Verify the redirect URI in your Spotify app matches `http://localhost:8080/callback`

### WebSocket Connection Issues

- Check your firewall settings
- Ensure port 8080 is not blocked
- Try refreshing the page

## Development

### Project Structure

```
.
├── main.go           # Backend server
├── go.mod            # Go dependencies
├── static/
│   └── index.html    # Frontend application
└── README.md         # Documentation
```

### Adding Features

The codebase is structured to easily extend:

- **Vote persistence**: Add database integration in `App.votes`
- **User authentication**: Track votes per user
- **Playlist management**: Add create/modify playlist features
- **Advanced playback**: Queue management, skip controls, volume

## License

MIT License - feel free to use and modify!

## Credits

Built with:
- [Spotify Web API](https://developer.spotify.com/documentation/web-api)
- [zmb3/spotify](https://github.com/zmb3/spotify) - Go library for Spotify
- [Gorilla Mux](https://github.com/gorilla/mux) - HTTP router
- [Gorilla WebSocket](https://github.com/gorilla/websocket) - WebSocket implementation
