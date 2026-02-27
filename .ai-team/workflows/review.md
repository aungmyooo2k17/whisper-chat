# Code Review Workflow

Standard workflow for reviewing code changes.

## Phases

### Phase 1: Overview
**Agent**: Analyst

1. Understand what the code is supposed to do
2. Identify scope of changes
3. Note areas requiring deeper review

### Phase 2: Architecture Review
**Agent**: Architect

1. Check adherence to patterns
2. Assess design decisions
3. Identify coupling/cohesion issues
4. Evaluate maintainability

### Phase 3: Security Review
**Agent**: Security

1. Check for vulnerabilities
2. Review auth/authz logic
3. Verify input validation
4. Check for data exposure

### Phase 4: Quality Review
**Agent**: QA

1. Assess test coverage
2. Identify missing test cases
3. Check edge case handling

### Phase 5: Implementation Review
**Agent**: Developer

1. Code quality and style
2. Performance considerations
3. Error handling
4. Edge cases in implementation

### Phase 6: Summary
**Agent**: Manager

Synthesize all reviews:
- Critical issues (must fix)
- Suggestions (should consider)
- Nitpicks (optional)
- Overall assessment

**Present to human for action**

## Quick Reference

```
Code to Review
    ↓
[Analyst] → Understand scope
    ↓
[Architect + Security + QA + Developer] → Parallel reviews
    ↓
[Manager] → Synthesized feedback
    ↓
Human decides on actions
```

## Review Checklist

### Must Check
- [ ] Does it work correctly?
- [ ] Is it secure?
- [ ] Does it follow project patterns?
- [ ] Is error handling appropriate?
- [ ] Are edge cases covered?

### Should Check
- [ ] Is it readable/maintainable?
- [ ] Is it testable?
- [ ] Is it performant?
- [ ] Is it documented where needed?

### Consider
- [ ] Could it be simpler?
- [ ] Are there better patterns?
- [ ] Will it scale?
