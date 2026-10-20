# Backlog

Ordered. The GitHub issues are the source of truth. Take the first open item.
Close the issue when it is done (comment with the commits and the tests), then remove it here.
Each item ends with a green gate run and a commit on `dev`.


## M1 Engine core


## M2 Scheduling and templates


## M3 Server, login and CLI


## M4 Web UI


## M5 History and re-runs


## M6 SDKs and Argo migration


## M7 Durable task workers


## M8 Release readiness

35. #34 E2E on Kind with Dex, Playwright and security scans in CI (waits for a green CI run; all checks pass locally)

## Soak test findings (2026-10-20)

A soak test ran all features three times on the demo cluster. Fix them in this order. Mark each item done with its commit.

High:
- S1. (done) A stopped or terminated run cannot be retried: spec.shutdown stays set. The retry ends at once, runs the exit handler again, and can start a Pod that runs while the run shows Failed. Stop on a finished run has the same effect.
- S2. (done) A task result above 64 KiB leaves the node Running forever: the server refuses CompleteTask and the SDKs ignore the error. A crashed worker without heartbeatTimeout also leaves the node Running forever.

Bugs:
- S3. (done) `approve -p <unknown>=x` approves anyway.
- S4. (done) A suspend output default outside its enum is admitted.
- S5. (done) The logs of stopped, terminated and timed-out steps are lost, also with archiveLogs.
- S6. (done) stop, suspend and resume of a finished run report success. Suspend then retry gives a Suspended run with an empty tree.
- S7. (done) Converter: templateDefaults.timeout replaces a template activeDeadlineSeconds.
- S8. (done) `chronicle get` after a retry leaves out kept nodes. The workflow onExit node is never shown.
- S9. (done) Stale lock messages on Running runs. A node that timed out while it waited for a lock ends with "waiting for the lock".
- S10. (done) The Python example worker deadlocks on a second SIGTERM (Event.set in the signal handler).

Smaller:
- S11. (done) Docs say a parameter value never becomes shell code, but placeholders in command or args go into `sh -c` as text.
- S12. (done) A descending withSequence gives zero items (Argo counts down).
- S13. (done) Unknown workflow parameters are accepted by `submit --from`, `rerun -p` and SubmitTemplate.
- S14. (done) `retry --node <bad>` exits 0 without --watch; display names are refused.
- S15. (done) `chronicle logs <run>` stops at the first missing Pod.
- S16. (done) Cluster templates are not listed for namespace users; the UI gives 403.
- S17. The UI stat "Runs in 24 h" counts the newest 200 runs.
- S18. (done) `lint` reads YAML 1.1, `submit -f` YAML 1.2. `convert` fails on a non-Argo document; lint skips it.
- S19. (done: the CRD checks durations with a pattern, and lint and submit name the field) Webhook duration errors have no field path. An invalid constant regex in `when` passes admission.
- S20. (done; LAST uses the new status field `lastStartedTime`) `get` does not show global output artifacts. `cron list` LAST and NEXT texts. `list` hides "only in the archive" when the run has a message.
- S21. (done; the missing edge did not come back on the current build) UI: at 390 px the graph is cut off on first render; a running graph does not refit; a missing edge from a nested DAG task; Tasks page icons and filters; a duration suspend uses the hand icon; "the depends condition is false" in steps; "soak-tlock, soak-tlock holds"; "<nil>: Invalid value" in a submit error; a foreign namespace URL keeps "demo" in the sidebar.

## Later (do not start in this run)

- #37 Durable workflow code with deterministic replay
- #38 Event triggers
- #40 UI edits of GitOps resources as pull requests
- #41 Multi-cluster runs
