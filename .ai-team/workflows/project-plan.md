# Project Planning Workflow

Standard workflow for planning a new project from scratch.

## When to Use

- Starting a brand new project with no existing code
- Human has an idea/concept and needs a structured plan before building
- Triggered by `/plan-project` command or when human describes a new project idea

## Phases

### Phase 1: Requirements Gathering
**Agent**: Manager (direct conversation with human)

Manager asks structured questions to understand the project. Not all questions need answers — adapt based on what the human already provided.

**Core Questions:**
1. **What** — What does this project do? What problem does it solve?
2. **Who** — Who are the target users? Any distinct user roles?
3. **Core Features** — What are the must-have features for the first version (MVP)?
4. **Nice-to-haves** — What features can wait for later versions?
5. **Constraints** — Any specific tech preferences, hosting requirements, budget limits, or deadlines?
6. **Integrations** — Any third-party services, APIs, or existing systems to integrate with?
7. **Scale** — Expected number of users? Data volume? Growth expectations?
8. **Reference** — Any existing apps/sites that are similar to what they want?

**Output**: A clear requirements summary for the team to work with.

### Phase 2: Analysis & Architecture
**Agents**: Analyst + Architect + Security (parallel)

1. **Analyst** breaks down requirements:
   - Define epics and user stories with acceptance criteria
   - Identify ambiguities or gaps in requirements
   - Prioritize features (MoSCoW: Must/Should/Could/Won't for MVP)
   - Map user flows for core features

2. **Architect** designs the system:
   - Recommend tech stack with justification
   - Define project structure and folder layout
   - Design database schema (entities, relationships)
   - Plan API structure / key endpoints
   - Identify infrastructure needs (hosting, CI/CD, storage)
   - Define system architecture (monolith vs microservices, etc.)

3. **Security** identifies early requirements:
   - Authentication & authorization approach
   - Data protection needs (encryption, PII handling)
   - Compliance requirements (GDPR, etc. if applicable)
   - Security best practices for the chosen stack

### Phase 3: Project Plan Synthesis
**Agent**: Manager

Manager combines all agent outputs into a unified **Project Plan Document**:

```
## Project Plan: [Project Name]

### 1. Overview
- Project description
- Problem statement
- Target users

### 2. Epics & User Stories
For each epic:
- Epic name and description
- User stories with acceptance criteria
- Priority (Must / Should / Could / Won't)

### 3. Architecture
- System architecture diagram (text-based)
- Tech stack with justification
- Project structure / folder layout

### 4. Database Design
- Entity list with key fields
- Relationships
- Schema notes

### 5. API Design (if applicable)
- Key endpoints grouped by resource
- Authentication approach

### 6. Security Requirements
- Auth strategy
- Data protection
- Compliance notes

### 7. Implementation Phases
- **Phase 1 (MVP)**: [Must-have features, estimated scope]
- **Phase 2 (v1.0)**: [Should-have features]
- **Phase 3 (Future)**: [Could-have features]

### 8. Infrastructure
- Hosting / deployment plan
- CI/CD approach
- Third-party services needed
```

**Human reviews, adjusts, and approves the plan.**

### Phase 4: Kanban Board Setup
**Agent**: Scrum Master

Once human approves the project plan:
- Scrum Master takes the approved plan and breaks it into a kanban board
- Epics → user stories → actionable tasks with size estimates and labels
- Tasks prioritized MVP-first with dependencies noted
- Board saved to `.ai-team/context/kanban.md`
- Suggests the first sprint (2-3 tasks to start with)

### Phase 5: Kickoff
**Agent**: Manager

Once kanban board is ready:
- Save the project plan to `.ai-team/context/project.md` (or a new project context file)
- The plan becomes the reference document for all future feature/bugfix workflows
- Manager picks the first task from the kanban board and kicks off the Feature workflow

## Quick Reference

```
Human describes project idea
    |
[Manager] --> Requirements Gathering (conversation)
    |
[Analyst + Architect + Security] --> Analysis & Design (parallel)
    |
[Manager] --> Project Plan Document --> Human Approval
    |
[Scrum Master] --> Kanban Board + First Sprint suggestion
    |
[Manager] --> Save context + Pick first task
    |
Ready to build (use Feature workflow)
```

## Tips

- Don't over-plan. Focus on MVP first, keep future phases lightweight.
- If the human already has a clear vision, skip unnecessary questions.
- If the human is unsure about tech stack, Architect should provide 2-3 options with trade-offs.
- The project plan is a living document — it will evolve as development progresses.
