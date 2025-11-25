# BROWSER CACHE PROBLEEM - OPLOSSING

Je browser cached de oude HTML. Hier zijn 3 manieren om dit op te lossen:

## Optie 1: Verwijder de oude static folder VOLLEDIG

```bash
# Stop de server (Ctrl+C)
cd /pad/naar/je/project
rm -rf static/
# Unzip de nieuwe versie
unzip -o spotify-voting-app.zip
go run main.go
```

## Optie 2: Direct de HTML file vervangen terwijl server draait

```bash
# In een nieuwe terminal (laat server draaien)
cd /pad/naar/je/project
# Kopieer de nieuwe index.html over de oude
cp index.html static/index.html
# Of unzip met overschrijven:
unzip -o spotify-voting-app.zip
```

Ga dan naar: http://localhost:8080/?v=2

## Optie 3: Cache handmatig clearen in Chrome/Edge

1. Open DevTools (F12)
2. Ga naar Network tab
3. Vink "Disable cache" aan (bovenaan)
4. Houd Ctrl ingedrukt en klik Refresh
5. Of: Rechtsklik op refresh button → "Empty Cache and Hard Reload"

## Optie 4: Test met curl eerst

```bash
curl http://localhost:8080/ | grep "Starting loadPlaylists"
```

Als je "Starting loadPlaylists" ziet, is de HTML goed.
Als niet, is de file nog niet vervangen.

## Verificatie

Open de pagina en kijk in de browser console. 
Je MOET zien:
```
=== Starting loadPlaylists ===
Fetching playlists from /api/playlists...
```

Als je alleen ziet:
```
Failed to load playlists: TypeError: Cannot read properties of undefined
```

Dan gebruikt de browser NOG STEEDS de oude HTML!
