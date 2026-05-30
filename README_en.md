[中文](./README.md)
# MeshBan

A P2P-based Minecraft banned-player list sharing system. It does not rely on any central server. Instead, it uses a DHT network and certificate signatures, allowing nodes to establish connections with each other and configure trust levels and ban rules independently.

## Features

* Not a plugin or mod, but standalone software. I hope to support all server cores through adapters.
* Certificate-based node identity, making all ban entries traceable.
* Sharing through a Friend-to-Friend trust network, while also using DHT to discover more nodes.
* Each node can configure ban rules separately for every Minecraft server it connects to.
* Different trust levels can be assigned to different nodes, allowing more fine-grained control when combined with ban rules.
* Certificate signatures are used to infer the trustworthiness of unknown nodes.
* Passive banlist querying helps prevent the spread of malicious bans.
* After a player joins the server, ban information is queried in the background. If the player matches the ban rules, they will be kicked without affecting connection speed.

## Current Workflow

* MeshBan connects to Minecraft servers via RCON to execute Kick commands. It obtains server updates by checking `banned-player.json` and `latest.log`, minimizing the performance impact on Minecraft as much as possible. In the future, adapters may be added to actively push updates through WebSocket.
* When a player joins the server, MeshBan first checks whether there is a matching allow/kick record in the local cache. If not, it checks the local Banlist to determine whether the kick condition is met. If the condition is still not met, it queries all connected Nodes. After synchronizing the local Banlist, it recalculates the result and writes it to the cache to speed up future queries.
* Adding a Node: enter the target Node address in the WebUI. MeshBan calls the API to obtain the Node certificate, verifies it, and then saves it to the local Node list.

## WebUI

The default binding address is `127.0.0.1:30000`, which can be configured in `config.yaml`.

### Features

* Database:

  * Banlist-related:

    * Clear cache
    * Manually add Banlist records
    * Manage the local Banlist
  * Node-related:

    * Add Node
    * Manage the Node list
* Minecraft

  * Global settings:

    * Master switch for Minecraft connections
    * Kick message
    * Contact information included in the kick message
    * Kick criteria, based on the following levels:

      * ultimate, trusted, friend, unknown, untrusted
        If the local Banlist contains a **configured number** of player ban records from nodes of a given trust level, the player will be kicked. `0` means that this trust level will not be counted.
    * Third-party UUID lookup API:
      This can be used to connect to unofficial UUID sources. It has not been thoroughly tested yet. For the current local log-based lookup, it is not very useful. It may become necessary later if Minecraft adapters are added.

      * Enable/disable switch
      * Request settings
      * Proxy settings
  * Per-Minecraft-server settings:

    * Overridable options: kick message, contact information, and kick criteria.
    * Required per-server settings: RCON connection information, `latest.log` file path, and `banned-players.json` file path. If the third-party UUID lookup becomes useful in the future, corresponding override options should also be added.
  * Test Minecraft server connectivity.
  * View logs filtered by Minecraft server instance.
  * Add/delete Minecraft server instances.
* Identity:

  * Create a new key pair, directly overwriting the original key pair. This action is irreversible.
  * Export the key pair. The encrypted private key will not be automatically decrypted. If the private key is encrypted by default, the private key password from the `.env` file must also be configured on the target node before import can succeed.
  * Import a key pair. If the import succeeds, it will directly overwrite the original key pair. This action is irreversible.
* Security:

  * Change the WebUI/API binding address
  * View/set/generate the WebUI token
  * Set/remove database key encryption
* Logs: view logs.

## Planned Features

See the [issues](https://github.com/PeterTerpe/MeshBan/issues) labeled `enhancement`.

## Security Notes

* Node connections: MeshBan will only actively connect to Nodes configured by the user, and by default it accepts query requests from all known or unknown Nodes. An IP blacklist may be added later to reject requests from certain IP addresses.
* MeshBan uses Node IDs to index the source Node identity and trust level of ban records. Therefore, it is crucial to ensure that the Node IDs in the local Banlist are correct. When MeshBan obtains a new node identity or Banlist entry, it locally calculates the Node ID using the target Node’s public key. When writing to the local Banlist, it uses the locally calculated ID, and issues a warning if the ID does not match.

## Want to Contribute to This Project?

For the latest code, see the [dev branch](https://github.com/PeterTerpe/MeshBan/tree/dev).

[Development Documentation](./docs/introduction_en.md)

[How to Contribute](./docs/contribution_en.md)
