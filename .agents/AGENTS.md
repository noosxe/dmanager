# Agent Guidelines

- **Nix Dev Shell**: All terminal commands, builds, tests, and execution of workspace tools must be performed within the Nix development shell environment. If executing commands directly via a terminal tool, ensure they run inside `nix develop --command <cmd>` or that the shell has been initialized with the flake.
- **Error Handling**: In case of any environmental setup issues, package resolution errors, or other system problems, stop immediately, report the exact details to the user, and ask for assistance.

## Development Workflow & PR Guidelines

- **Documentation First**: Design documents, requirements, and schemas must always be updated *first* before implementing any feature. Keeping implementation and documentation strictly in sync is critical. After implementing a story, the agent must update the stories tracker document (`docs/stories.md`) to mark the completed story as done.
- **Consulting Documentation**: The agent must use the documentation as the primary source of truth to understand the application. You must consult existing documentation first during any feature development workflow, and ensure documents are continuously maintained in a useful, accurate state.
- **Manageable PR Sizes**: Keep Pull Requests small and focused to ensure they are easily reviewable by humans. Avoid large changes (such as updates exceeding 1,000 lines of code or massive multi-file changes). Break features down into small, logical, and incrementally reviewable PRs.

## Git Workflow & Code Quality Guidelines

### 1. Branching Strategy
- **Pull Before Branching**: Before creating a new branch, agents must always switch to the `main` branch, pull the latest changes from the remote origin (`git checkout main && git pull`), and create the new branch off the up-to-date local `main` branch to prevent divergence and conflicts.
- **Protected Main Branch**: Direct commits to the `main` branch are strictly prohibited.
- **Branch Naming**: All new changes must be pushed on separate branches prefixed according to the change theme:
  * `feat/...` for new features
  * `bug/...` or `fix/...` for bug fixes
  * `docs/...` for documentation updates
  * `chore/...` for build steps, package configuration, or tool updates
  * `test/...` for writing or updating test suites

### 2. Commits & Messages
- **Commit Message Prefix**: Match the branch prefix format (e.g., `feat: ...`, `fix: ...`, `docs: ...`, `chore: ...`).
- **Descriptive Details**: Commit messages must describe the changes in detail (what was changed, how it was changed, and the rationale). Avoid vague messages.

### 3. Pre-Commit Validation
Before staging or committing any code, agents must verify local code standards:
- **Go Backend Changes**:
  * Run Go formatter (`gofmt` or `goimports`).
  * Run Go compiler checking/vetting (`go vet ./...` or `golangci-lint run`).
- **Frontend Changes**:
  * Run Biome formatting and lint verification (`pnpm biome check` or `pnpm biome format`).
- **Build Checks**:
  * Ensure the Go binary successfully compiles (`go build -o /dev/null` or similar build command).
  * Ensure the React application successfully builds (`pnpm build`).
- **Testing**: Run relevant tests (`go test` and `pnpm test`) to prevent regressions.
- **Untracked Files**: Check `git status` for new/untracked files. Verify if they are intended to be committed. If they are temporary/local files, update the root `.gitignore` file instead of committing them.

### 4. Code Pull Requests (PRs)
- **Automatic PR Creation**: After a successful branch push, check if the GitHub CLI tool (`gh`) is available in the environment. If it is, automatically generate a Pull Request to merge the branch into `main`.
- **PR Description**: Include a clear description explaining the changes made, the implementation details, and the verification checks performed.

### 5. Safe Git Operations
- **No Blind Discarding**: Never run commands that blindly discard unstaged changes (e.g., `git reset --hard` or `git checkout -- .`) without user approval.
- **Confirmation & Stashing**: If you are unsure whether local changes are needed, confirm with the user first. If you are confident they are required but need a clean working tree, utilize `git stash` instead of discarding them.
