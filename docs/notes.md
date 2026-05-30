# Notes
This document records notes as a reminder for future development

- default uuid source is hardcoded in /internal/minecraft/server_bans.go loadBannedPlayersFile()
- no format check when importing banned-player.json
- check if banlist is updated by checking banlist_version table in database, and increase everytime banlist is updated
- signature verification of new banlist entries is done in `internal/nodes/client.go:queryNode()`

## Temporary notes
Should be useless after commit

- remove unnecessary format check when importing banned-players.json
- add template cache
