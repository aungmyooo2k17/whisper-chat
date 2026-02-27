# Feature Implementation Workflow

Standard workflow for implementing new features.

## Phases

### Phase 1: Analysis
**Agents**: Analyst + Architect (parallel)

1. **Analyst** investigates:
   - Break down requirements
   - Identify affected components
   - List edge cases
   - Flag ambiguities for human clarification

2. **Architect** assesses:
   - How it fits into existing architecture
   - Design approach
   - Technical decisions needed
   - Potential risks

### Phase 2: Proposal
**Agent**: Manager

Manager synthesizes findings and presents:
- Feature summary
- Technical approach
- Files to be modified/created
- Any decisions needed from human
- Risk assessment

**Human approves or provides feedback**

### Phase 3: Implementation
**Agents**: Developer + Security (parallel)

1. **Developer** implements:
   - Write the code
   - Follow project patterns
   - Handle edge cases

2. **Security** reviews:
   - Check for vulnerabilities
   - Verify secure patterns

### Phase 4: Quality
**Agent**: QA

- Review implementation
- Suggest test cases
- Identify gaps

### Phase 5: Final Review
**Agent**: Manager

Manager presents:
- Summary of changes
- Security findings
- Test recommendations
- Ready for human final approval

## Quick Reference

```
Human Request
    ↓
[Analyst + Architect] → Analysis
    ↓
[Manager] → Proposal → Human Approval
    ↓
[Developer + Security] → Implementation
    ↓
[QA] → Quality Check
    ↓
[Manager] → Final Summary → Human Approval
    ↓
Done
```
