# Authenticated LAN storage and file cache

The `storage` commands transfer real files. They do not allocate demo memory,
read another process's heap, or claim to distribute a game's rendering.

## Persistent login

```
omls storage init
omls storage login --node STUDIO
omls storage check --node STUDIO
```

`init` exclusively creates `~/.config/omls/connections.env` with mode 0600.
The file is parsed as literal key/value data, never executed. A different file
can be selected with `--env`. See `.env.example` for the profile format. Node
names select `OMLS_<NODE>_` keys. Real .env files are ignored by git.

Before `login`, fill in the remote host, user, and absolute root in the private
profile. `login` creates a dedicated Ed25519 key if absent, then invokes
`ssh-copy-id`. Enter the remote account password in your own terminal for this
initial enrollment and review the host key there. Subsequent operations require
known host keys and use batch key authentication, with no password prompts.
The initial key has no passphrase to support unattended use; protect the local
account and file. Login creates the configured remote root with private
permissions.

For SMB, set MODE=smb, SHARE, USER, PASSWORD and optionally DOMAIN in the
private file. ROOT must be relative to that share. smbclient receives a
temporary 0600 credential file, removed after the operation; passwords are
not placed in process arguments or printed. Configure the share and parent
directories on the server before use. SMB login is checked with `storage check`.

## Real file path

```
omls storage put --node STUDIO --source ./asset.bin --path assets/asset.bin
omls storage get --node STUDIO --path assets/asset.bin --out ./consumer/asset.bin
```

`put` verifies SHA-256 before publishing a staged upload. SSH creates parent
directories and renames the staged file. SMB requires the parent to exist,
verifies by downloading the staged contents, and refuses replacement when
the SMB server does not support renaming over an existing destination.

`get` stores verified content by SHA-256 under `~/.cache/omls/assets` and
atomically materializes an ordinary file for a consumer. For SSH, it hashes
the remote source on every request: a valid local hit avoids downloading
the payload, but still costs a remote disk read/hash and an SSH request.
Changed remote contents invalidate the hit; corrupted local content is
downloaded again. There is no silent stale/offline fallback. SMB always
downloads before hashing, so it reports no network cache hit. Results include
bytes, payload download size, digest, hit/miss and elapsed time. Protocol
overhead is not included in payload bytes. The cache currently has no eviction
policy; remove unused cache objects manually when needed.

The configured root is a trusted location, not a chroot: remote symlinks can
refer outside it. Do not expose writable roots to untrusted users.

## Sims integration boundary

A consumer must actually open the materialized file. Merely uploading game
files does not change Sims's reads or reduce its CPU/GPU work. Do not move
live saves, mods or shader caches while the game or Steam writes them.

This laptop already has a Steam shader cache for app 1222670. Compiled shader
cache portability must be validated against GPU, driver, renderer and game
version before any reuse between machines. Generic assets and compiled shader
binaries must not share a compatibility namespace. This change does not yet
redirect Sims file reads, implement a streaming filesystem, share live GPU
memory, or deploy an authenticated service to STUDIO. Remote credentials and
consumer integration are required before an end-to-end game claim.

The OMLS cluster's coordinator election is unchanged. The laptop can operate
these transfers without becoming the elected cluster coordinator.
