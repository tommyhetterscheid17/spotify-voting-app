# Fly.io Deployment Fix

## Probleem: App crashed bij startup

De app crashed waarschijnlijk omdat SPOTIFY_ID en SPOTIFY_SECRET niet zijn gezet.

## Oplossing:

### 1. Check de logs
```bash
fly logs
```

### 2. Voeg Spotify credentials toe als secrets
```bash
fly secrets set SPOTIFY_ID=your_spotify_client_id
fly secrets set SPOTIFY_SECRET=your_spotify_client_secret
```

### 3. Update je Spotify App Redirect URI
Ga naar: https://developer.spotify.com/dashboard
- Open je app
- Klik "Settings"
- Voeg toe aan Redirect URIs: `https://spotify-voting-app.fly.dev/callback`
- (vervang spotify-voting-app met jouw app naam)

### 4. Deploy opnieuw
```bash
fly deploy
```

### 5. Check of het werkt
```bash
fly status
fly logs
```

Je app zou nu moeten draaien op: https://spotify-voting-app.fly.dev

## Als het nog steeds crashed:

Check de logs voor de exacte error:
```bash
fly logs --app spotify-voting-app
```

Mogelijke problemen:
- Environment variables niet gezet
- Port binding issue (moet port 8080 gebruiken)
- Static files niet gevonden
