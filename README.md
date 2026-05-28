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

The GitHub E2E workflow is a consumer of the centralized
`agynio/e2e` harness. It validates agynd-specific integration coverage only:
the workflow builds this repository's `dist/agynd` binary, provisions the
standard platform cluster, and runs the agent-orchestrator E2E coverage where
agynd participates in agent workload execution and tracing. The workflow limits
the centralized harness to the `playwright-tracing-app` suite and disables the
shared smoke tag.

This keeps agynd-cli coverage focused on daemon behavior and avoids failing this
repository for unrelated centralized smoke coverage, such as go-core Gateway
smoke tests. Broader platform smoke coverage remains owned by the centralized
E2E repository and service-specific workflows.
