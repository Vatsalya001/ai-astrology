# Roadmap

Twelve phases. One at a time. Each has a gate in `docs/specs/PHASE-NN-*.md` that must
pass in full before the next begins — a gate is a hard stop, not a suggestion.

**MVP = Phases 0 → 6.** Everything after that is expansion.

---

## Sequence

| # | Phase | Deliverable | Size | State |
|---|---|---|---|---|
| 0 | Foundation | Three-service skeleton, enforced invariants, CI | 5–8d | 🟢 gate closing |
| 1 | Auth & users | Sign up, log in, manage a profile, delete everything | 6–9d | ⬜ next |
| 2 | Astrology engine | Birth details → chart, dashas, transits, deterministically | 12–18d | ⬜ |
| 3 | Kundli UI | The chart made visible and understandable | 8–12d | ⬜ |
| 4 | AI infrastructure | Provider abstraction, prompts, intent, safety | 10–14d | ⬜ |
| 5 | RAG & chat | "Chat with my Kundli" — streaming, grounded, explainable | 14–20d | ⬜ |
| 6 | Memory & personalization | Cross-conversation memory, daily astrology, **eval harness** | 14–18d | ⬜ |
| — | **MVP complete and shippable** | | | |
| 7 | Monetization | Subscriptions, credits, wallet, double-entry ledger | 14–20d | ⬜ |
| 8 | Astrologer marketplace | AI→human handoff, consultations, per-minute billing | 25–35d | ⬜ |
| 9 | Voice AI | Speak to the astrologer; free local STT/TTS | 12–18d | ⬜ |
| 10 | Mobile | Expo app on the same Go API | 15–25d | ⬜ |
| 11 | Advanced intelligence | Life timeline, reports, copilot, scale | ongoing | ⬜ |

Estimates are for the work, not the calendar.

---

## Why this order

**Phases 2 and 3 before any AI.** The chart and its UI have standalone value. Shipping
them first proves the product works before a single token is spent, and gives Phase 5's
"Why am I seeing this?" a real chart to highlight against. It also means the most
correctness-critical code in the system — the ephemeris — is written and golden-tested
while it has your full attention, not alongside prompt engineering.

**Phase 4 before Phase 5.** The provider abstraction, prompt registry and safety layer
are built and tested with no user-facing surface. Getting the PII guard and the
fabricated-fact validator right is much easier when nothing is shipping on top of them.

**Phase 6 before Phase 7.** The eval harness lands before anyone pays. Charging for
output you cannot objectively measure is how quality regressions reach customers.

**Phase 7 before Phase 8.** The marketplace runs on the wallet and ledger. Building
per-minute consultation billing before the ledger exists means building it twice.

**Phase 8 last of the majors.** A two-sided marketplace is a materially harder
operational business — supply quality, liquidity, disputes, payouts, trust and safety.
Do not start it until the single-sided product has real users and real revenue.

Phases 9 and 10 depend only on Phase 6 and can run in parallel with 7 and 8 if there
are people to do it.

---

## Decisions that must be made by a given phase

| Decision | Due by | Why it cannot slip |
|---|---|---|
| [ADR-003](decisions/003-astrology-engine.md) — Swiss Ephemeris licence: AGPL vs commercial vs MIT `skyfield` | **Phase 7** | The AGPL network clause is incompatible with closed-source SaaS. Once money changes hands this stops being a task and becomes a liability. |
| Managed auth vs hand-rolled | Phase 1 | Hand-rolled auth is a classic vulnerability source. Decide before building, not after. |
| Deletion vs financial retention | Phase 7 | Phase 1 promises complete account deletion; tax law requires keeping transaction records. A genuine conflict needing an explicit, documented resolution. |
| In-app purchase strategy | Phase 10 | 15–30% store commission is a pricing decision, not a technical one, and it must be settled before submission. |

---

## Cost trajectory

Development is free throughout. Production cost appears in Phase 4 and is shaped by
three levers, in order of impact:

1. **Prompt caching** — the astrology rule corpus is a large, byte-stable prefix reused
   on every request. The single biggest lever in the system.
2. **Batch API** (50% off) — daily horoscopes and monthly reports have no latency
   requirement.
3. **Segmentation** — daily horoscopes are generated per *segment*
   (moon sign × ascendant × dasha), not per user. A 100–1000× reduction with no loss of
   astrological fidelity, because two users in the same segment genuinely have the same
   daily conditions.

Judge **cost per completed task**, not per request. A cheaper call that needs a retry or
a follow-up question is not cheaper.

---

## What would change this roadmap

- **Phase 3 analytics.** The landing page emits `ai_chat_box_tapped` while the chat box
  is disabled. If almost nobody taps it, Phase 5 is not the most valuable next thing.
- **ADR-003 resolving toward `skyfield`.** That adds work inside Phase 2.
- **Marketplace demand appearing early.** If users ask for human astrologers before
  Phase 7 ships, reconsider the 7-before-8 ordering — but not the ledger dependency.
