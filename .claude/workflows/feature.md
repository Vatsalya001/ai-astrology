# Workflow: implementing a feature

1. **Locate it.** Which service owns this? See the language-boundary table in
   `.claude/CLAUDE.md`. Getting this wrong is the most expensive mistake available.
2. **Read the phase spec.** The task list, data model and API surface are already
   written. Do not redesign them ad hoc.
3. **Smallest coherent increment.** Not the whole phase in one change.
4. **Test the negative case.** A guard never observed to fire is a guard you cannot
   trust. If you add a check, add the test that proves it rejects.
5. **`task verify`** before committing.
6. **Update `docs/PROJECT_STATUS.md`.**

## When a cross-service API changes

Regenerate contracts (`task contracts`) and commit the output. CI fails if the
committed artefacts differ from what the source produces — that check is what makes a
contract change visible in review rather than discovered in production.

Never hand-write a cross-service client.
