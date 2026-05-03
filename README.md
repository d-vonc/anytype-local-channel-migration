# Anytype Channel Migration Experiment

Experimental helpers for manually moving an Anytype space/channel from one vault identity to another vault identity.

This is not an official Anytype workflow. It started as a practical, curiosity-driven attempt to solve a history-preserving migration problem between two vault identities: export/import behaves more like snapshot migration, while collaboration-based migration can potentially preserve object history and author attribution.

The original code-side issue is here:

https://github.com/anyproto/anytype-heart/issues/3137

My concrete test case used two Local Only vaults because Local Only invitation-link based sharing was not available in the UI path I was exploring. Local Only is therefore the initial use case, not the whole point of the tool. A surprising side effect of the experiment was that the underlying ACL/shared-channel mechanics still worked in this setup when the destination vault was manually added to the space ACL and given a local `SpaceView`. That was unexpected because the community thread below includes the official/current-product answer that Local Only invitation-link sharing is not supported, while my later replies in that thread point out conflicting documentation/signals:

https://community.anytype.io/t/invitation-link-to-channel-is-not-working-peer-to-peer-local-only/30482

## What Worked

The working route used two tools:

1. `space-migration`
   - Reads the old and new vault mnemonics through hidden terminal prompts.
   - Verifies that the mnemonics derive the expected old and new account IDs.
   - Copies the old space `store.db` into a temporary location.
   - Adds the new account to the space ACL with writer permissions, signed by the old account key.
   - Installs the updated space store into the new vault.
   - Copies the per-space `objectstore` projection and merges `flatfs`.
   - Prints the new ACL head.

2. `spaceview-register`
   - Reads the new vault mnemonic through a hidden terminal prompt.
   - Starts Anytype Heart against the new vault data directory.
   - Creates a `SpaceView` in the new account tech-space for the migrated space.
   - Uses the ACL head printed by `space-migration`.

The removed experimental folders were:

- `space-migration-keycheck`: useful preflight only; its account verification is now part of `space-migration`.
- `spaceview-rawcreate`: raw fallback experiment; it was not the route that made the space visible.

## Safety

Close Anytype before running either tool. Both tools refuse to run if an `anytype` process appears in `/proc`.

Make full backups of both the old and the new vault before trying this. At minimum, back up both complete `--user-data-dir` directories, not just the individual `data/<accountId>` folders. These tools write directly into local Anytype data.

Do not paste mnemonics into chat, shell history, command arguments, or environment variables. The tools prompt for mnemonics using hidden terminal input.

## Build

The tools depend on a local checkout of `anytype-heart`. In the setup used for this experiment, this repo was checked out at:

```sh
local-tools/channel-migration
```

and the Go modules use a local `replace` pointing to:

```sh
../../../anyproto-repos-sorted/core/anytype-heart
```

If your checkout layout is different, update the `replace github.com/anyproto/anytype-heart => ...` line in both Go modules before building.

Build:

```sh
cd local-tools/channel-migration/space-migration
go build -o /tmp/localspace_migrate .

cd ../spaceview-register
go build -o /tmp/localspace_register_view .
```

`spaceview-register` depends on Anytype Heart native dependencies such as `tantivy_go`. If the build fails there, build/fetch Heart's native deps first.

## Usage

Run the migration step first:

```sh
/tmp/localspace_migrate \
  -old-vault "/path/to/old/user-data-dir/data/<oldAccountId>" \
  -new-vault "[hier stond de nieuwe data-root]/<newAccountId>" \
  -old-account "<oldAccountId>" \
  -new-account "<newAccountId>" \
  -space-id "<spaceId>"
```

Save the printed `acl head after add`.

Then register the space in the new vault tech-space:

```sh
/tmp/localspace_register_view \
  -data-root "[hier stond de nieuwe data-root]" \
  -account "<newAccountId>" \
  -space-id "<spaceId>" \
  -acl-head "<aclHeadFromMigration>"
```

Open Anytype with the new `--user-data-dir` after both steps complete.

## Known Limitations

The migrated/new participant may appear as `Untitled` to the original vault. This tool only injects the ACL/account metadata needed for access and creates the destination `SpaceView`; it does not fully reproduce the official profile/name propagation path.

This was validated on a specific Local Only setup. Treat it as a research tool, not a general migration product.
