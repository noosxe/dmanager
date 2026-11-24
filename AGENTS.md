# Notes for agent sessions

Project overview, build and configuration: see [README.md](README.md). The frontend is in `frontend/` (React + Vite, pnpm); the backend is Go.

## Checks

- Frontend: `pnpm --dir frontend run check` (oxlint + oxfmt), `pnpm --dir frontend run test` (Vitest), `pnpm --dir frontend exec tsc -b` (the TypeScript type check that CI's `pnpm build` runs; Vitest doesn't type-check, so run this too), `pnpm --dir frontend exec vite build --outDir ../.scratch/vite-build --emptyOutDir` (the output goes to the gitignored `.scratch/` because a plain build empties the tracked `frontend/dist`, whose `.gitkeep` Nix and `go:embed` need).
- Backend: `go test -race ./...` and `make lint` (see [Make targets](README.md#make-targets)).
- Changing `frontend/pnpm-lock.yaml`, `go.mod` or `go.sum` needs `make nix-hashes` (see [Nix package](README.md#nix-package)).

## Verifying UI changes

The UI runs in the browser with fake data, so no backend, Docker or Tailscale is needed.

1. Run `pnpm --dir frontend install` once per checkout, then start the `frontend-mock` configuration from `.claude/launch.json` (built-in browser: `preview_start`). Manually: `pnpm --dir frontend dev:mock` serves <http://localhost:5199>.
2. Open every screen the change touches. Fixtures and the mock server live in `frontend/src/mock/`; mutations (start, stop, upgrade, auto-update) change in-memory state, which resets on reload.
3. Pick variants with query parameters; they stick for the tab until changed:
   - `?scenario=admin` (default), `viewer` (non-admin: admin-only screens and actions are denied), `login` (logged out), `setup` (first run), `empty` (no containers)
   - `?tailscale=running` (default), `starting`, `failed`, `disabled`
4. Where relevant, check light and dark mode and a narrow width (for example 375px).
5. Take screenshots and save them under `.scratch/<task>/` (gitignored). In the report, list each with its absolute path and a one-line description so the reviewer can look at them.
6. Screenshots never go into the repository or into PR bodies unless asked.

When a screen needs data the fixtures lack, extend the fixtures and keep `frontend/src/mock/mock.test.ts` passing. Mock code is only loaded with `--mode mock` and must stay out of production bundles (`import.meta.env.MODE` gate plus dynamic import in `frontend/src/client.ts`).
