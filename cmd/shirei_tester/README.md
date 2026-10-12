# shirei_tester

IDE-style snapshot test runner (usually for Shirei UI tests; any Go module
that uses the same patterns works).

```bash
# from the shirei module root:
go run ./cmd/shirei_tester
go run ./cmd/shirei_tester ~/code/maqam-gui

# install:
go install go.hasen.dev/shirei/cmd/shirei_tester@latest
```

## What it does

1. **Discovers** packages under the scan root (`go.mod` / `go.work`) by
   walking the filesystem for `*_test.go` that mention `TestSnapshot`,
   `ReportSnap`, `layoutSnapshot`, or `drive.Start` (no `go list`, no
   goldens on disk). Nested modules (their own `go.mod`, e.g.
   `examples/ferry`) are included. Discovery runs in the background so
   the window opens immediately.
2. **Lists** every `Test*` by parsing `*_test.go` sources (no compile).
   Drive-only packages list tests from files that call `drive.Start`.
3. **Runs** all / package / single test with `go test -json` and
   `SHIREI_SNAP_REPORT` when the harness supports it. Drive tests also
   get `SHIREI_DRIVE_TRACE`. Run all skips drive-only packages (they
   open windows); run a drive test or package explicitly.
4. **Shows** a wipe compare (actual vs golden, with diff highlight) and
   **Accept** when the report includes paths; otherwise you still get
   pass/fail and log output. **All diffs** lists every mismatch on one
   screen, each with the wipe slider and Accept, so many can be reviewed
   without stepping Next fail. A drive test shows a filmstrip of
   `drive.Shot` beats and the UDP commands between them.

## Harness report (optional)

| Env | Meaning |
|-----|---------|
| `SHIREI_SNAP_REPORT` | Append-only JSONL path (`shirei.SnapEvent`) |
| `SHIREI_DRIVE_TRACE` | Directory for drive JSONL + PNGs (`drive.Shot`). Tester sets a temp dir per run and removes those dirs on exit. |
| `UPDATE_SNAPSHOTS=1` | Rewrite goldens (still reported as `updated`) |

Event shape (one JSON object per line):

```json
{"pkg":"/abs/pkg","test":"TestSnapshotFoo","name":"id","status":"mismatch","golden":"/abs/a.png","actual":"/abs/a.actual.png"}
```

`status`: `match` | `mismatch` | `created` | `updated` | `skip`

Emitted by `shirei.ReportSnap` / `shirei.Snapshot`. Other projects can emit
the same lines from an inlined helper.
