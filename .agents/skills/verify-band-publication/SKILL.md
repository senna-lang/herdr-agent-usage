---
name: verify-band-publication
description: Verify exclusive sidebar limit-band publication when changing writeLimitBandTokensWith, metadata write throttling, pane snapshot acquisition, or concurrent event/watch publishers. Use for /verify-band-publication or changes that can leave multiple limit colour bands visible.
---

# Verify sidebar band publication

**Promise:** after all successful publishers of a nonempty limit row finish, exactly one of `limit_high`, `limit_mid`, `limit_low` contains one publisher's complete row.

**Harm:** a comfortable-headroom row can remain beside a near-limit warning, leaving the user with contradictory quota information.

This skill checks a bounded TLA+ interleaving model and replays its failing schedule through the real Go band writer. The Go replay supplies an in-memory store at the existing metadata writer boundary; it does not invoke Herdr or mock the publication algorithm.

## 1. Confirm the model still describes the code

**Sync commit:** `c0c74f5c5ad40c92d87fc62c38580940109d4ef2`.

Read every covered-code change since that commit, including uncommitted changes, before interpreting a pass. Run `git log` and `git diff` with that commit and the coverage paths below. Look for new publishers or moved functions as well as edits to existing names. Do not advance the sync commit until the mapping and assumptions have been confirmed at the new commit. Keep the sync record here; there is no second coverage registry.

| Covered code | Model correspondence |
| --- | --- |
| `internal/core/limitband.go`: `LimitTokenNames`, `LimitBandFor` | `Bands` is the canonical high → mid → low order. The selected 80% and 20% inputs choose high and low. If the family or order changes, update the model and replay together. |
| `internal/core/throttle.go`: `ShouldWriteToken` | `Publish` compares the desired value with `snapshot[w][name]`, not the live store. `force=false` throughout. |
| `internal/update/update.go`: `writeMetadataTokenWith`, `writeLimitBandTokensWith` | Each successful per-token set/clear is one atomic store operation. An empty desired inactive band is cleared only when its frozen snapshot differs. The snapshot is not updated by the writer. |
| `internal/update/update.go`: `RunUpdateForPane` | `ReadSnapshot` is `GetPaneInfo`; the returned `pane.Tokens` survives collection and is passed into the band writer. |
| `internal/update/publish.go`: `PublishCollectedLimitsWith`, `applyLimitPublishTargets` | A periodic publisher independently snapshots the same pane and calls the same band writer. There is no shared publication lock spanning these callers. |
| `internal/herdrcli/herdr.go`: `GetPaneInfo`, `SetMetadataToken`, `ClearMetadataToken` | One pane snapshot and separate `report-metadata` requests, not an atomic three-token transaction. |
| `cmd/usagebar/main.go`: `status`/`update`, `watch`, limits-pane `renderFull`; `bin/run-status.sh`, `bin/run-usagebar.sh`; `internal/update/startup.go` | Independent entrypoints can call the event and periodic publishers. The launch wrappers execute the command without a common per-pane publication lock; startup also reuses the event publisher. Recheck these routes if serialization is added outside the helper. |

Treat the whole family as one indivisible step only if the real code obtains a transaction or a lock covering snapshot through final write across every publisher. If that changes intentionally, update the model from the new code, not from the result you want.

## 2. Run the check

From the repository root:

```sh
.agents/skills/verify-band-publication/scripts/check
```

Prerequisites: `mise`, `jq`, and a Go launcher that supports `GOTOOLCHAIN`. The command installs/uses TLC **1.7.4** with SHA-256 `936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88`, Temurin **25.0.5+8.0.LTS**, and Go **1.26.11**. First use may require the network. No Herdr process, account, or credentials are needed.

The command runs both TLC and the real-function replay even when one fails. It prints their independent exit codes and an evidence directory outside the repository containing `tlc.out` and `replay.out`. The temporary Go overlay is removed; product files are never overwritten. Normal `go test ./...` does not discover the deliberately failing replay assets under `.agents`.

**Nonzero means a finding or tool error, never success.** At the sync commit, the default check is expected to fail: the concurrent model violates `QuiescentExclusive`, and `TestVerifyBandPublicationConcurrent` leaves both high and low rows present. The single-publisher control passes. A parser error, unavailable tool, Go compilation error, or failed installation is a blocked check, not confirmation of the finding. Use the final status line and both logs to distinguish them.

Load **`/statechart` for every TLA+ result**. Use its current instructions to read this model/config and the generated result, show the shortest counterexample in plain words, or draw the passing state graph. `model/view.mjs` supplies reader-facing labels. Keep statechart's executable paths and options out of this skill and its scripts; resolve them from that skill when needed. Use a fresh scratch output folder for each run.

## 3. Replay and interpret

The deterministic counterexample is:

1. The critical-headroom publisher snapshots an empty family.
2. It skips the high clear and then the middle clear, because its snapshot already has empty values.
3. Before its low write reaches the store, the comfortable-headroom publisher also snapshots the still-empty family.
4. The critical publisher completes its low write, `5h 20%`.
5. The comfortable publisher writes high, `5h 80%`, then skips the middle and low clears against its frozen snapshot.
6. Both publishers are finished, but both rows remain visible.

`replay/concurrent_test.go` captures the second snapshot inside the first publisher's low-write callback, immediately before that write commits. It then completes the first call and runs the second. This reproduces the snapshot/write ordering of the statechart's shortest counterexample, including skipped comparisons through the real `writeLimitBandTokensWith`; only the atomic backing store and read timing are supplied. Its assertion requires one complete row and must continue to fail on the current implementation. Do not make it assert the buggy two-row contents. The model explores other interleavings too.

The single-publisher control starts with a high row, then changes it to low using a matching snapshot:

```sh
REPLAY_PATTERN='^TestVerifyBandPublicationSingle$' .agents/skills/verify-band-publication/scripts/check Single
```

Both parts must pass. This is a control, not a claim that concurrent publication is correct. Setting `force=true` or issuing every clear is not a demonstrated fix for a family of separate writes; another publisher may still interleave them.

## 4. Prove the check can fail after a change

Use an isolated worktree and preserve every product file you touch. Do not mutate the user's working copy.

1. Run the single-publisher control above; require TLC and Go to pass.
2. In the scratch copy of `writeLimitBandTokensWith` only, suppress inactive-band clears while keeping the active-band write. This is a broken variant, not a proposed fix.
3. Run the **unchanged `Single` command** again. The original model still passes because it does not see Go source edits; the unchanged real-code replay must fail. This proves the executable check catches a real-code break rather than merely selecting a failing model.
4. Run the following to also check the model of that broken implementation. `NoClears.cfg` describes the same omission; the overlay still calls the actual modified Go function.

   ```sh
   REPLAY_PATTERN='^TestVerifyBandPublicationSingle$' .agents/skills/verify-band-publication/scripts/check NoClears
   ```

5. Require a `QuiescentExclusive` counterexample and the same failed single-publisher Go assertion showing the old high row survived. A model-only failure is insufficient.
6. Restore the exact original product bytes. Run `Single` again and require both parts to pass, then rerun the default check. At the sync commit it must return only the already-recorded concurrent finding.

When a later implementation fixes the concurrent bug, the default assertions should pass without being weakened. Reconfirm the model's atomic steps and repeat a real-code broken-variant experiment before advancing the sync commit.

## Bounds and exclusions

- One pane, the three existing limit-band names, two publishers, one snapshot and one high → mid → low pass each. The control uses one publisher and an existing high row. Inputs are nonempty, `force=false`, and `retainExistingOnEmpty=false`.
- The transition relation permits all orders of the modeled snapshot and per-token operations. TLC stops at the first invariant violation; `/statechart` also explores the bounded reachable graph past the violation. Terminal stuttering prevents a finished run being mistaken for deadlock. There is no fairness/liveness or unbounded proof.
- Individual successful token writes are assumed atomic. The server's internal behavior, request failures/timeouts, deleted panes, and a real Herdr UI are not verified. The replay demonstrates the actual Go decision logic, not a live GUI incident.
- Provider selection, percentage calculation, cache bands, per-window bands, empty-row retention, and unrelated tokens are outside this claim. Their shared-looking code is not proof that they have been checked.

## Hand off

Report model and replay outcomes separately, including tool errors, current-code findings, the control, the real-code broken variant, and restoration. State bounds and missing live-UI coverage. Record observed evidence in `results.md`; do not turn an expected failure into a passing check. If covered code changed without rechecking, use `/maintain-formal-verification-skill` to bring this verification back in sync. Product fixes are a separate user request.
