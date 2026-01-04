# E2E Test Improvements Roadmap

This document tracks future improvements for KubeOpenCode E2E tests.

## Current Coverage

The E2E test suite covers the following core functionality:

| Feature | Test File | Status |
|---------|-----------|--------|
| Task lifecycle (Pending/Running/Completed/Failed) | task_test.go | Covered |
| Task garbage collection | task_test.go | Covered |
| Context CRD (Text type) | task_test.go | Covered |
| Context CRD (ConfigMap type) | task_test.go | Covered |
| Context CRD (Git type) | task_test.go | Covered |
| Agent contexts merge | task_test.go | Covered |
| Stop annotation (kubeopencode.io/stop) | task_test.go | Covered |
| HumanInTheLoop Sidecar | task_test.go | Covered |
| Session Persistence | session_test.go | Covered |
| Agent podSpec.labels | agent_test.go | Covered |
| Agent podSpec.scheduling | agent_test.go | Covered |
| Agent credentials (env/file) | agent_test.go | Covered |
| Default Agent resolution | agent_test.go | Covered |
| MaxConcurrentTasks | agent_test.go | Covered |
| Workflow execution | workflow_test.go | Covered |
| CronWorkflow scheduling | cronworkflow_test.go | Covered |

## Future Improvements (P2-P3)

### P2: Medium Priority

#### 1. Runtime Context Type Test

**Background**: `ContextTypeRuntime` injects platform awareness information into the agent.

**Suggested Test**:
```go
Context("Task with Runtime Context", func() {
    It("should inject platform awareness context into task.md", func() {
        // 1. Create Agent with Runtime context inline
        // 2. Create Task
        // 3. Verify task.md contains runtime info (TASK_NAME, WORKSPACE_DIR, etc.)
    })
})
```

**Files to modify**: `e2e/task_test.go`

---

#### 2. Inline Context Test

**Background**: `ContextSource.Inline` allows defining context directly in Task/Agent specs.

**Suggested Tests**:
```go
Context("Task with Inline Context", func() {
    It("should support inline Text context in Task", func() {
        // Use ContextSource.Inline with Type=Text
    })

    It("should support inline ConfigMap context in Agent", func() {
        // Use ContextSource.Inline with Type=ConfigMap in Agent.spec.contexts
    })
})
```

**Files to modify**: `e2e/task_test.go`

---

#### 3. Error Scenario Tests

**Background**: The controller should handle missing resources gracefully.

**Suggested Tests**:
```go
var _ = Describe("Error Handling E2E Tests", func() {
    It("should fail Task when Agent does not exist", func() {
        // Create Task with non-existent agentRef
        // Verify Task enters Failed phase with appropriate condition
    })

    It("should fail Task when Context CRD does not exist", func() {
        // Create Task referencing non-existent Context
        // Verify Task enters Failed phase
    })

    It("should fail Task when ConfigMap does not exist", func() {
        // Create Context with non-existent ConfigMap reference
        // Verify Task enters Failed phase
    })

    It("should fail Task when credential Secret does not exist", func() {
        // Create Agent with non-existent Secret in credentials
        // Verify Task enters Failed phase
    })
})
```

**Files to create**: `e2e/error_test.go`

---

### P3: Low Priority

#### 4. Context MountPath Relative Path Test

**Background**: Relative paths should be prefixed with `workspaceDir`.

**Suggested Test**:
```go
It("should resolve relative mountPath with workspaceDir prefix", func() {
    // Create Context with mountPath: "guides/readme.md" (relative)
    // Verify content appears at /workspace/guides/readme.md
})
```

**Files to modify**: `e2e/task_test.go`

---

#### 5. Agent Tolerations/Affinity Test

**Background**: `podSpec.scheduling.tolerations` and `affinity` are not tested.

**Suggested Test**:
```go
Context("Agent with advanced scheduling", func() {
    It("should apply tolerations to generated Jobs", func() {
        // Create Agent with tolerations
        // Verify Pod template has tolerations
    })

    It("should apply affinity rules to generated Jobs", func() {
        // Create Agent with nodeAffinity
        // Verify Pod template has affinity rules
    })
})
```

**Files to modify**: `e2e/agent_test.go`

---

#### 6. TTL Cleanup Test

**Background**: `KubeOpenCodeConfig.taskLifecycle.ttlSecondsAfterFinished` controls automatic cleanup.

**Suggested Test**:
```go
It("should delete completed Task after TTL expires", func() {
    // Create KubeOpenCodeConfig with short TTL (e.g., 10 seconds)
    // Create and complete a Task
    // Wait for TTL + buffer
    // Verify Task is deleted
})
```

**Prerequisites**: May require modifying test timeouts.

**Files to modify**: `e2e/task_test.go` or new `e2e/lifecycle_test.go`

---

## Test Infrastructure Needs

| Feature | Requirement |
|---------|-------------|
| Git Context Tests | Network access to GitHub (or local gitea) |
| Session Persistence | PVC with RWX access mode (may need NFS or similar) |
| TTL Tests | Short test TTL values for practical testing |

## Contributing

When adding new E2E tests:

1. Follow existing patterns in the test files
2. Use `uniqueName()` for all test resources to avoid conflicts
3. Clean up resources in `By("Cleaning up")` blocks
4. Use `Eventually()` with appropriate timeouts for async operations
5. Add coverage entries to this document
