#!/bin/bash

# Spotify Playlist Voting App - Quick Start Script

echo "🎵 Spotify Playlist Voting App - Quick Start"
echo "============================================"
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "❌ Error: Go is not installed"
    echo "Please install Go from: https://golang.org/dl/"
    exit 1
fi

echo "✅ Go is installed: $(go version)"
echo ""

# Check for environment variables
if [ -z "$SPOTIFY_ID" ] || [ -z "$SPOTIFY_SECRET" ]; then
    echo "⚠️  Environment variables not set"
    echo ""
    echo "Please set your Spotify credentials:"
    echo "1. Go to https://developer.spotify.com/dashboard"
    echo "2. Create an app (redirect URI: http://localhost:8080/callback)"
    echo "3. Set environment variables:"
    echo ""
    echo "   export SPOTIFY_ID=your_client_id"
    echo "   export SPOTIFY_SECRET=your_client_secret"
    echo ""
    echo "Or create a .env file (see .env.example)"
    echo ""
    read -p "Press Enter to continue if you've already set them, or Ctrl+C to exit..."
fi

# Download dependencies
echo ""
echo "📦 Installing dependencies..."
go mod download

if [ $? -ne 0 ]; then
    echo "❌ Failed to download dependencies"
    exit 1
fi

echo "✅ Dependencies installed"
echo ""

# Start the server
echo "🚀 Starting server..."
echo ""
echo "Server will be available at: http://localhost:8080"
echo "Press Ctrl+C to stop"
echo ""

go run main.go
