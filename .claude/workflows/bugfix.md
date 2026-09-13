# Workflow: fixing a bug

1. **Reproduce it first.** Write the failing test before the fix. If you cannot
   reproduce it, you cannot know you fixed it.
2. **Find the trace ID.** Every request carries one across all three services. Start
   there rather than guessing which service is at fault.
3. **Fix the cause, not the symptom.**
4. **Keep the test.** It is now a regression test.
5. **Search for the same pattern elsewhere.** One instance usually means more.

## If the bug touches an invariant

Stop. A bug in the single-writer rule, the determinism principle, the PII guard or the
money handling is not a normal bug. Fix it, add the machinery that makes the class of
bug impossible, and note it in the relevant ADR.
