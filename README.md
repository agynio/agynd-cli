# agynd

The agynd daemon bridges agent CLIs with the platform by connecting to Threads
and Notifications services, preparing the agent runtime environment, and managing
the agent process lifecycle.

Architecture: https://github.com/agynio/architecture/blob/main/architecture/agynd-cli.md

## Local Development

Full setup: https://github.com/agynio/architecture/blob/main/architecture/operations/local-development.md

### Run from sources

```bash
devspace dev
devspace dev -w
```

## E2E validation

The GitHub E2E workflow runs this repository's local E2E tests with:

```bash
go test -v -count=1 -tags e2e ./test/e2e/
```

Those tests validate the local agent CLI bridge behavior against deterministic
TestLLM endpoints through the Codex and AGN SDK flows. The workflow checks out
`agynio/agn-cli` so the AGN coverage builds and runs the current AGN CLI during
the test. They also build and execute this repository's `cmd/agynd` binary with
`/agyn-bin/config.json` installed by the test harness. That test uses a stub
Gateway and fake AGN agent to verify daemon startup, platform initialization,
and subscriber startup without requiring a full cluster.

This repository does not run the centralized `agynio/e2e` smoke suite. Broader
platform and service smoke coverage remains owned by the centralized E2E
repository and service-specific workflows.
