# Agent Guidelines

- **Nix Dev Shell**: All terminal commands, builds, tests, and execution of workspace tools must be performed within the Nix development shell environment. If executing commands directly via a terminal tool, ensure they run inside `nix develop --command <cmd>` or that the shell has been initialized with the flake.
- **Error Handling**: In case of any environmental setup issues, package resolution errors, or other system problems, stop immediately, report the exact details to the user, and ask for assistance.
