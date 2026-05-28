# Code Structure & Purpose
This document briefly explain the code structure and each file's purpose
## main.go
The main programme that handles other components.

- Initialise configuration and database
- Create the WebUI server
- Start enabled Minecraft monitors

## api
The WebUI and API server, manage request authentication

*PS: moving the auth logic to auth could be a good idea*

## auth
Authentication helper of [api](#api)

- Handle WebUI connection authentication (token/session cokkie based)
- Prevent basic brute force attack of WebUI token

## config
Initialise/Load configuration files

- Create config.yaml from example_config.yaml if not present.

## database
Handle data stored in the database

### banlist.go
Handle the banlist table

- Validate entries before writing to database
  - require: player UUID, ban reason, source node ID

### cache.go
Handle player decision cache entries used by Minecraft join checks.

- Cache allow/kick decisions per Minecraft instance and player UUID
- Invalidate cached decisions when the local banlist version changes

### identity.go
Handle node identities using [identity/service.go](#servicego)

- Local
- Others (not implemented yet)

## debug/peer
A debug tool to test peer connections during development, not fully implemented yet.

## minecraft
Monitor Minecraft servers through log tailing and RCON.

- Tail the configured server log and check players when join events appear
- Poll RCON `banlist players` and import resolvable server bans into the local banlist
- Resolve player UUIDs from log UUID lines, RCON entity data, or the optional UUID resolver
- Check cached allow/kick decisions before evaluating local banlist entries
- Kick players who satisfy the configured policy

## identity
Directly handle key pair

### private_key.go
Handle private key encryption and decryption, versioning. A helper for [service.go](#servicego)

### service.go
Manage local identity, signing, and private key

- Initialise local identity from database
- Initialise passphrase of the private key in .env file
- Key pair import/export
  - Validation of imported json
- Fetch/Re information about other nodes

## secrets
Generate new passphrase/token and save them to .env

## web
The part where users interact with, but all traffic goes through ```adminAccessMiddleware()``` in [api](#api) for authentication first.

It allow users to:
- Login
- Manage local identity:
  - import/export/regenerate key pairs
- Manage WebUI security options:
  - Binding address
  - Copy/set/regenerate token
  - Toggle token verification of remote clients
- Manage key security options:
  - Remove/set private key passphrase
- Manage local Banlist entries:
  - Add/edit
- Debug options
- View logs
- Manage Minecraft server log/RCON connections and status
- Manage peer connections: (to be implemented)
  - Manage peer list
  - Manage trust level
  - Deny request from blacklisted IPs
### static & templates
css and html files used to construct the frontend WebUI

### web.go
Construct WebUI frontend
