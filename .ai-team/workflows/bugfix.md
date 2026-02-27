# Bug Fix Workflow

Standard workflow for investigating and fixing bugs.

## Phases

### Phase 1: Investigation
**Agent**: Analyst

1. Understand the reported bug
2. Reproduce the issue (trace through code)
3. Find root cause
4. Identify affected areas
5. Check for similar issues elsewhere

### Phase 2: Assessment
**Agent**: Architect (if needed)

For complex bugs:
- Assess architectural implications
- Determine if fix requires refactoring
- Identify proper solution approach

### Phase 3: Proposal
**Agent**: Manager

Present to human:
- Bug summary and root cause
- Proposed fix approach
- Risk level
- Affected files

**Human approves or provides feedback**

### Phase 4: Fix
**Agents**: Developer + Security (parallel)

1. **Developer**:
   - Implement the fix
   - Ensure no regressions
   - Handle edge cases

2. **Security**:
   - Verify fix doesn't introduce vulnerabilities
   - Check if bug had security implications

### Phase 5: Verification
**Agent**: QA

- Confirm bug is fixed
- Suggest regression tests
- Check related functionality

### Phase 6: Summary
**Agent**: Manager

Present:
- What was fixed
- How it was fixed
- Any related issues found
- Test recommendations

**Human final approval**

## Quick Reference

```
Bug Report
    ↓
[Analyst] → Investigation
    ↓
[Architect] → Assessment (if complex)
    ↓
[Manager] → Proposal → Human Approval
    ↓
[Developer + Security] → Fix
    ↓
[QA] → Verification
    ↓
[Manager] → Summary → Human Approval
    ↓
Done
```

## Severity Guide

| Severity | Description | Response |
|----------|-------------|----------|
| Critical | System down, data loss, security breach | Immediate fix |
| High | Major feature broken, no workaround | Priority fix |
| Medium | Feature impaired, workaround exists | Scheduled fix |
| Low | Minor issue, cosmetic | Backlog |
