[中文](./introduction.md)
# Development Documentation

This document mainly describes the software feature design. For code structure and technical details, please see [Code Structure & Purpose](./code_en.md).

## Issues to Consider

* What is the optimal algorithm for determining whether a player should be banned? See the discussion in [Ban Logic](#ban-logic).

## Core Components

### Main Program

Unless there is an additional preference, Go is recommended for implementation because it is easy to get started with, and is more convenient for compilation and concurrent processing.

* Access the local database

  * SQLite
* Establish connections with multiple Minecraft servers

  * Listen for player join events through logs, and perform queries and kick operations through RCON/adapters
  * Check whether a player matches the ban rules when they join; if matched, kick the player
  * Synchronize newly banned players to the local database
* Node-related:

  * Discover neighboring nodes
  * DHT (libp2p)
  * Certificates and signatures, trust levels
  * Query databases of other nodes
* API: used by the web panel and other nodes

### Web Panel

* Manage node connections
* Manage the local Banlist
* Manage the exemption list
* Manage cache
* Manage ban criteria for Minecraft servers
* Manage local identity

### Minecraft Adapters

* Paper
* Fabric

### Configuration File

* Minecraft server endpoint, required trust levels and counts for bans. See the example below.

  * `client`: `"ws://127.0.0.1:27000/server1"`
  * `token`: `"token_for_authentication"`
  * `criteria`:

    * `ultimate`: `1` If the player is found once in the banlist of a node with the `ultimate` trust level, the player will be kicked from the server
    * `trusted`: `2`
    * `friend`: `3`
    * `unknown`: `20`
    * `untrusted`: `0` Do not count bans from nodes with the `untrusted` trust level

### Database

* Self information

  * Node ID, generated from the certificate and identical to the Peer ID
  * Certificate
  * Key index
* Node list

  * Node ID
  * Connection address
  * Trust level
  * Valid signature count, meaning the number of signatures from nodes with a trust level of `unknown` or higher
  * Update time
* Certificate signatures

  * Node ID
  * Signing node ID
  * Signature
* Banlist

  * Player UUID
  * Player name
  * UUID source
  * Ban reason
  * Source node ID
  * Update time
  * Signature
* Exemption list

  * Player UUID
  * Exemption reason
  * Ban source node ID
  * Update time
* Cache

  * Player UUID
  * Minecraft server endpoint, since different Minecraft servers can use different ban criteria
  * Allow/Kick
  * Update time

## Business Logic

*Note: All checks must verify the timeliness and authenticity of data, including update time and signatures.*

### Ban Logic

Each Minecraft server’s evaluation criteria are defined in the [Configuration File](#configuration-file).

1. First, query whether a valid record exists in the cache. If there is a match, execute according to the cache.

   * Cache matching logic: same Server ID, same player UUID, and same Banlist version.
   * Each Banlist update will separately clear the related cache. This reduces cache validation time, because an existing cache entry can be considered valid. Expiration time may be added later depending on the situation.
2. If no valid cache is found, query the local Banlist first and determine whether the ban criteria are met. If they are met, kick the player and write the result to the cache.

   * Although the Banlist and cache are synchronized, different Minecraft servers may have different ban criteria, so matching conditions may already exist in the Banlist.
3. If the local Banlist does not meet the ban criteria, [query the Banlists of all nodes](#query-node-banlist), and write the ban records into the local Banlist. Then check the local Banlist again. If the ban criteria are met, kick the player, and finally write the corresponding result to the cache.

### Player Joins the Server

*The so-called ban only means kicking players who meet the ban criteria from the server, so it does not affect the waiting time for players joining the server.*

1. Query the local database by player UUID. If the player is in the exemption list, end this logic.
2. If the player is not in the exemption list, execute [Ban Logic](#ban-logic).

### Query Node Banlist

* Request content:

  * Player UUID
* Response content:

  * All complete entries in the node that match the player UUID

## Additional Notes

* By default, the Minecraft server has online-mode authentication enabled and can obtain real player UUIDs.

## Glossary

* Node: the MeshBan main program. Usually, a host with this program installed can be considered one node. If multiple independent MeshBan instances are installed on the same host, they are considered multiple nodes.
* Peer: a peer in the DHT network
* Certificate: equivalent to the public key
