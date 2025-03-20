# ArgoCD App Webhook

## Overview

The **ArgoCD App Webhook** is a Kubernetes admission webhook designed to optimize the handling of ArgoCD `Application` resources by filtering out unnecessary updates. Specifically, it prevents updates that only modify `status.reconciledAt`, reducing API server load and ETCD database growth.

### Benefits

1. **Reduces API Server Load**: Prevents frequent PATCH API calls caused by `status.reconciledAt` updates.
2. **Optimizes ETCD Storage**: Minimizes unnecessary revision history storage in ETCD.

## Usage

### Log Levels

- **INFO**: Logs high-level details like server startup, validation results, and detected changes.
- **DEBUG**: Logs detailed field-level differences in `metadata`, `spec`, and `status`.

To enable debug logs, set the log level:

```go
log.SetLevel(log.DebugLevel)
```

## Example Logs

**When no significant differences are found:**

```json
{"level":"info","msg":"No significant differences found."}
```

**When changes are detected:**

```json
{"level":"info","msg":"----- Spec Differences -----"}
{"level":"debug","msg":"Key: foo, Old Value: bar, New Value: baz"}
```
