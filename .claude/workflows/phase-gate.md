# Workflow: closing a phase gate

A gate is a hard stop. Do not begin the next phase with items outstanding.

## Steps

1. **Open the spec.** `docs/specs/PHASE-NN-*.md`, last section.
2. **Evaluate every box against the running system**, not against memory or against
   what you intended to build. Run the command. Read the output.
3. **Record honestly.** A partially-met item is 🟡, not ✅. Overstating progress in
   `PROJECT_STATUS.md` is worse than missing the item, because it hides the gap from
   whoever reads it next.
4. **Deferrals must be explicit.** If an item genuinely belongs in a later phase, say
   so in `PROJECT_STATUS.md` with the reason. "We ran out of time" is a legitimate
   reason; silently dropping it is not.
5. **Update** `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md`.
6. **Run `task verify`.** It must pass from a clean tree.
7. **Confirm CI is green** on the pushed commit — not just locally.

## The failure mode this exists to prevent

Marking a gate closed because most of it is done, then discovering three phases later
that the missing piece was load-bearing. The gates are ordered so that each phase's
foundations are proven before anything is built on them.
