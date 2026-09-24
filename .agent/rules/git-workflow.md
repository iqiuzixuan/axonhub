---
alwaysApply: true
---

# Fork Development Workflow

Read and follow [the project workflow](../../docs/solutions/fork-development-workflow.md) before branch, feature, merge, or upstream-sync work.

1. `origin` is this fork; `upstream` is the original repository. Verify their URLs and current branches before acting.
2. Keep `unstable` free of fork-only changes. Maintain our tested changes on `develop`.
3. Create independent `feat/*`, `fix/*`, `docs/*`, or `chore/*` branches explicitly from `develop`; never assume the current checkout is the correct base.
4. Complete relevant tests before merging a feature back into `develop`. Keep each logical purpose in a separate commit; use a normal merge to preserve ancestry.
5. Sync upstream on a temporary `sync/upstream-YYYYMMDD` branch created from `develop`. Merge `upstream/unstable`, resolve conflicts, test both upstream and fork behavior, then merge back. Do not reset or rebase published `develop` onto upstream. Acceptance means every upstream change is reachable and usable in the UI/behavior, not merely compiling with green tests; when fork tests conflict with an upstream feature, update the tests to follow the feature instead of dropping it, and run a syntax-level check (esbuild/`tsc --noEmit`) after stitching JSX/TSX conflicts.
6. Upstream PRs use a separate `pr/*` branch based on the intended upstream target, containing only that contribution. Do not open upstream PRs from `develop`.
7. Do not delete a deployed database migration when upstream implements the same feature differently. Merge schema sources first, regenerate generated files, and verify migration of existing data.
8. Record the exact tested commit and deployed image digest. Branch merge is not deployment authorization. Follow existing push approval rules; never force-sync our customized `develop` to upstream.
9. Store execution and verification reports in `.agent/summary/`; keep enduring rules in project documentation.
