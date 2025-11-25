# Persistent Storage Setup for Fly.io

## De votes blijven nu bewaard na restarts! 🎉
Test
### Hoe het werkt:

1. **SQLite database**: Votes worden opgeslagen in `votes.db`
2. **Fly.io Volume**: Een persistent volume mount op `/data`
3. **Auto-sync**: Votes worden automatisch elke 30 seconden gesynchroniseerd
4. **Immediate save**: Elke vote wordt ook direct opgeslagen

### Eerste keer deployen met volume:

```bash
# 1. Maak een volume aan (doe dit maar 1x!)
fly volumes create votes_data --region ams --size 1

# 2. Deploy de app
fly deploy

# 3. Check of het werkt
fly logs
```

Je zou dit moeten zien in de logs:
```
📁 Using persistent volume for database: /data/votes.db
📊 Loaded X votes from database
```

### Na restart:

```bash
fly apps restart
```

De votes blijven behouden! Ze worden geladen uit de database bij startup.

### Volume beheer:

```bash
# Lijst volumes
fly volumes list

# Volume details
fly volumes show votes_data

# Backup maken (optioneel)
fly ssh console
cd /data
cat votes.db > /tmp/backup.db
exit
```

### Lokaal testen:

Lokaal wordt de database opgeslagen als `./votes.db` in je project directory.
Dit bestand kun je committen naar git of backuppen.

### Database stats bekijken:

```bash
# SSH in je Fly.io app
fly ssh console

# Bekijk de database
sqlite3 /data/votes.db "SELECT * FROM votes ORDER BY vote_count DESC LIMIT 10;"
```

### Reset votes (als je wilt):

```bash
# Lokaal
rm votes.db

# Op Fly.io
fly ssh console
rm /data/votes.db
exit
fly apps restart
```

### Belangrijk:

- **Volume is persistent** - blijft bestaan zelfs als je de app verwijdert
- **Backups**: Fly.io maakt automatisch snapshots
- **1GB volume** is gratis en meer dan genoeg voor miljoenen votes

## Troubleshooting:

**"Failed to open database: unable to open database file"**
- Check of het volume gemount is: `fly volumes list`
- Check of de app toegang heeft: `fly ssh console -C "ls -la /data"`

**"Loaded 0 votes from database" maar je had votes**
- Check of het juiste volume gemount is
- Kijk naar de database: `fly ssh console -C "sqlite3 /data/votes.db 'SELECT COUNT(*) FROM votes;'"`
