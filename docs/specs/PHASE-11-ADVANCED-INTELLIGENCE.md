# Phase 11 — Advanced Intelligence, Analytics, Optimization & Scale

| | |
|---|---|
| **Goal** | Compound the advantage. Deepen the intelligence, sharpen the economics, and scale what real traffic proves needs scaling. |
| **Deliverable** | Life timeline, monthly reports, family charts, the astrologer copilot, a mature analytics practice, measured cost optimisation and a scale plan grounded in actual bottlenecks. |
| **Depends on** | Phase 8 (copilot), Phase 6 (everything else) |
| **Estimated size** | Ongoing — this is not a phase that "completes" |
| **Cost to run** | Development free; production cost *reduction* is a primary goal here |

> Phase 11 is different: it is a **menu, not a sequence.** Pick items based on what your
> analytics say users want and what your cost dashboard says is actually expensive.
> Building all of it speculatively is how good products get bloated.
>
> **Do nothing in §4 (scale) until a measurement says you must.** Premature scaling costs
> more than it saves and makes every subsequent change harder.

---

## 1. Advanced intelligence features

Each is independently shippable. Ordered by my estimate of value-per-effort; reorder
based on your data. The service each lands in is noted — most are cheaper than they look
because the deterministic half already exists.

### 1.1 Life timeline ⭐ highest value — `astro` + `ai`

A scrollable timeline of a person's life mapped against dashas and major transits —
past, present and future.

```
1994 ─── 2001 ─── 2021 ─── 2027 ─── 2037 ─── 2044
Ketu     Venus     Sun      Moon     Mars     Rahu
                                      ▲
                                    today

  ● 2019 Jupiter dasha begins
  ● 2021 Saturn antardasha — career consolidation
  ◆ 2026 Sade Sati begins (rising)
  ● 2027 Mercury antardasha
  ◆ 2029 Jupiter returns to natal position
  ● 2035 Saturn mahadasha begins
```

Why it's the strongest item: the underlying data is **already computed** by
`astro-service` (Phase 2 dashas and transits), so the astrological work is done. The LLM
writes one short line per period. Cheap to build, visually striking, intensely
shareable, and the clearest expression of "the AI knows *your* astrology."

Let users annotate it with real life events ("started my company", "got married"). That
turns a generated artifact into a personal document people return to — and it feeds
memory with high-quality, user-confirmed facts.

### 1.2 Monthly and annual reports — `ai`, batch

Long-form, generated on the user's birth-month boundary by a Go `asynq` scheduled task.
A genuine premium upsell.

Structure: the year's dasha context · major transits month by month · favourable and
challenging windows · themes per life area · remedies. 2,000–4,000 words, PDF via the
Phase 3 `chromedp` worker.

Generate at the `deep` tier through the **Batch API at 50% cost** — no latency
requirement, so there is no reason to pay real-time rates.

### 1.3 Personal astrology calendar — `astro` only

Deterministic, so it costs **nothing per user**: favourable days for specific
activities, muhurta windows, transit events, dasha changes, Rahu kaal per day. Export to
Google/Apple Calendar via ICS.

The highest engagement-per-rupee feature available — `astro-service` already computes
everything and no LLM is involved at all.

### 1.4 Astrology journaling — `api` + `ai`

The user logs how a day actually went; the app correlates it with transits over time.

> "Over the last 6 months, you've rated your mood higher on days when the Moon was in
> Taurus, Cancer or Pisces."

Two reasons to build it: it creates a daily return habit, and it generates a private,
proprietary dataset nobody else has. The correlation itself is plain statistics in Go or
a small Python job — not an LLM task.

Frame correlations honestly — as patterns observed in their own logs, never as proof of
causation.

### 1.5 Family charts — `api` + `astro`

Multiple profiles under one account (supported since Phase 2) plus family-level views:
parent-child compatibility, sibling dynamics, family transit calendar.

Strong retention and strong virality — families onboard families. **Consent matters:**
an adult family member's chart requires their own consent. Build a proper
invite-and-accept flow rather than letting one person silently store everyone's birth
data.

### 1.6 Career and relationship assistants — `ai`

Long-running, goal-scoped conversations that persist across sessions: "help me decide
about this job change over the next three months." Uses memory heavily; Go schedules
proactive check-ins at dasha or transit inflection points.

The clearest expression of "companion, not chatbot" and the strongest subscription
justification.

### 1.7 Astrologer copilot (B2B) — `ai` + Phase 8 WebSocket

For Phase 8's astrologers, during a live consultation.

```
┌──────────────────────────────────────────┐
│  Consultation with Priya    ⏱ 08:14      │
├─────────────────────┬────────────────────┤
│  CHAT               │  ✦ COPILOT         │
│                     │                    │
│  Priya: I'm worried │  Chart highlights: │
│  about my son's     │  • 5th house: Leo  │
│  education...       │  • 5th lord Sun    │
│                     │    in 9th, exalted │
│                     │  • Jupiter aspects │
│                     │    5th house       │
│                     │  • Current: Mercury│
│                     │    antardasha      │
│                     │                    │
│                     │  Previous session: │
│                     │  asked about her   │
│                     │  daughter's        │
│                     │  marriage (Jul)    │
│                     │                    │
│                     │  Suggested probes: │
│                     │  • Son's age?      │
│                     │  • Which stream?   │
└─────────────────────┴────────────────────┘
```

Rules: **the astrologer is always in control.** The copilot surfaces facts and
suggestions; it never drafts messages to send, and nothing it produces reaches the user
without the astrologer typing it. Copilot suggestions are never shown to the user and
never logged as the astrologer's own words.

Delivered over the existing Phase 8 WebSocket as a separate event type scoped to the
astrologer's socket only — the user's socket never receives copilot events, enforced by
the hub's per-event authorisation rather than by client-side filtering.

Real value: raises the floor on consultation quality, shortens time-to-relevant-answer,
and gives astrologers a reason to stay on your platform rather than take clients
off-platform. That last one is the actual business case.

### 1.8 Advanced voice personas — `ai`

Building on Phase 9: distinct voices per persona, adjustable pace, regional Indian
language voices (Tamil, Telugu, Bengali, Marathi). Extends reach substantially — the
addressable market for Hindi + English is a fraction of the market for Indian languages
generally. Piper has voices for several of these; Sarvam AI covers more in production.

---

## 2. Analytics maturity

### The funnel, instrumented end to end

```
Landing                100%
  ↓ 42%
Signup completed        42%
  ↓ 78%
Birth profile created   33%      ← biggest drop. Every field here costs you users.
  ↓ 94%
Kundli generated        31%
  ↓ 61%
First AI question       19%      ← the activation event
  ↓ 44%
Second session           8%      ← the retention event
  ↓ 31%
Paid (sub or credits)  2.6%
  ↓ 18%
Human consultation     0.5%
```

Numbers are illustrative. What matters is instrumenting every step (Phases 1–8 already
emit the events) and watching the two that predict everything:

- **First AI question** — activation. If a user never asks, they never come back.
- **Second session** — retention. The single best predictor of lifetime value.

### Cohort and segment analysis

Retention curves by acquisition channel, by whether a birth time was known, by chart
style, by first intent. That last one is unusually informative: users whose first
question is `CAREER` behave very differently from users whose first question is
`MARRIAGE`, and it should shape onboarding.

### AI quality in production

Extend the Phase 6 eval harness to sample live traffic:

- Sample 1% of responses nightly, run the deterministic graders
- Alert on any grounding or safety failure in production — these are P0
- Track thumbs-up/down and report rate per prompt version
- Correlate response quality with retention, not just with judge scores

A prompt version that scores well offline but correlates with churn is a worse prompt.
Offline evals gate safety; production data decides quality.

### Tooling

PostHog (self-hosted free, or free cloud tier) for product analytics and funnels;
Metabase (free, open source) on a Postgres read replica for business metrics; Sentry free
tier for errors; Grafana + Prometheus (free) for infrastructure and AI telemetry, with
OpenTelemetry traces from Phase 0 tying a request across all three services.

All free at this scale, and self-hosting keeps behavioural data in your own database —
which, given what this product knows about people, is the right default.

---

## 3. Cost optimization

Do this in order. Free wins before tradeoffs, always.

### 3.1 Free wins — no quality cost

| Lever | Expected impact |
|---|---|
| **Prompt caching** | Largest single lever. The astrology rule corpus, safety rules and persona form a big, byte-stable prefix reused on every request. Verify `cached_input_tokens > 0` in production and alert if the hit rate drops. |
| **Batch API** for daily horoscopes, monthly reports, memory-extraction backfills | 50% off, and none have latency requirements |
| **Input-token hygiene** | The Phase 5 context builder already filters by intent. Audit for creep — contexts grow silently as features are added. |
| **Output-token hygiene** | Cap `max_tokens` per job type. A daily horoscope doesn't need 4,000 tokens of headroom. |
| **Daily segment deduplication** | Already done in Phase 6; verify the segment count hasn't drifted as the user base grows |
| **Keyword pre-pass on intent** | Already in Phase 4; measure what fraction of messages skip the model call and tune the rules upward |
| **Cancel abandoned streams** | Phase 5 propagates client disconnect Go → Python → provider. Verify it still works; a regression here silently burns tokens on requests nobody is reading. |

### 3.2 Tradeoff levers — measure before and after

| Lever | Notes |
|---|---|
| **Effort tuning** | `low` for classification and extraction, `medium` for chat, `high`/`xhigh` only where it demonstrably helps. Measure per route, not globally — which workloads repay higher effort is a property of the workload. |
| **Model tier changes** | Before building a cheaper-model cascade, test the simpler alternative: the same model at lower effort. Lower effort on a newer model often beats a prior-generation model at high effort, and one model means one cache namespace — a cascade forfeits cache reuse across its members. |
| **Context trimming** | Fewer RAG chunks, shorter memory window. Measure retrieval recall before and after; this is the lever most likely to quietly degrade answers. |

**Judge cost per completed task, not per request.** A cheaper call that needs a retry, a
regeneration after validation failure, or a follow-up question because the answer was
thin is not cheaper. The Phase 4 telemetry in `ai_request_logs` already has everything
needed to compute this — use it.

Run the full Phase 6 eval suite before and after every cost change. A cost reduction that
fails a safety or grounding gate is not a cost reduction; it is a regression with a
smaller invoice.

---

## 4. Scale — only when measured

You already have three services, so the usual "when do we split the monolith" question
is mostly answered. The remaining questions are about replicas, data volume and the
database.

### Signals that actually justify action

| Signal | Response |
|---|---|
| Postgres CPU consistently > 70% | Read replica for analytics and reporting first; point Metabase and `ai-service` reads at it |
| `astro-service` CPU saturated | Scale replicas horizontally — it is **stateless**, so this is trivial. Charts are cached, so load should be low; if it isn't, the cache key is wrong. |
| `ai-service` saturated | Scale replicas. Watch for GIL contention from Phase 9's STT work — CPU-bound audio and async LLM I/O in the same process may warrant splitting voice into its own deployment. |
| API WebSocket connections exceed one instance | Already handled: Phase 8's Redis pub/sub fan-out. Add replicas behind a sticky-session-free load balancer. |
| `ai_request_logs` / `messages` tables enormous | Partition by month; archive cold partitions to object storage |
| pgvector search slow at scale | Tune HNSW `ef_search`; only consider a dedicated vector DB well past ~1M chunks |
| Redis memory pressure | Audit key TTLs first — usually something is cached without expiry |
| `asynq` queue backing up | Scale worker replicas; they're stateless |

### The one split worth considering

**Voice out of `ai-service`.** Phase 9 puts CPU-bound STT/TTS in the same process as
async LLM orchestration. Under load these interfere: the GIL means audio transcription
can stall the event loop serving chat. If the Phase 9 concurrency test starts failing in
production, split `voice-service` out. Everything else should scale by adding replicas.

### Reliability work that matters more than scaling

- Database backups **with restore drills**. An untested backup is a hope, not a backup.
- **Graceful degradation.** If `ai-service` is down: Kundli, chart, dashas, transits, daily predictions and PDF all still work. That's most of the product. If `astro-service` is down: every existing chart still renders from cache; only new profiles fail. Verify both degradation paths deliberately — they are the main reliability dividend of this architecture and they are worth nothing if untested.
- Circuit breakers on every external dependency (Phase 4 has them for LLM; extend to payments, LiveKit, push)
- Chaos testing: kill each service, kill Redis, kill a worker — verify each degradation path
- Runbooks for the top 10 incident types
- Status page

---

## 5. Ongoing eval-driven improvement

The Phase 6 harness becomes a continuous improvement loop rather than a gate:

```
Baseline the current prompt version on the full suite
      ↓
Change one thing (prompt, context, retrieval, effort)
      ↓
Re-run: hard gates (safety, grounding) + quality rubric
      ↓
Improved on a held-out split?  ──no──► revert
      ↓ yes
Ship behind a percentage rollout (Phase 4 prompt A/B)
      ↓
Watch production: thumbs, reports, retention
      ↓
Promote or roll back
```

Keep a **held-out test split** that is never used for iteration. Tuning against your
whole eval set until it passes is overfitting to the eval, and it produces a prompt that
scores well and performs worse. This is the most common way eval-driven development
quietly stops working.

---

## 6. Environment variables added

```bash
# api-service
FEATURE_LIFE_TIMELINE=false
FEATURE_MONTHLY_REPORTS=false
FEATURE_ASTRO_CALENDAR=false
FEATURE_JOURNALING=false
FEATURE_FAMILY_CHARTS=false
FEATURE_ASSISTANTS=false
FEATURE_ASTROLOGER_COPILOT=false

REPORT_GENERATION_CRON=0 1 * * *
REPORT_USE_BATCH_API=true
DB_READ_REPLICA_URL=
MESSAGE_PARTITION_STRATEGY=monthly
ARCHIVE_AFTER_MONTHS=12

# ai-service
EVAL_PRODUCTION_SAMPLE_PERCENT=1
EVAL_ALERT_ON_GROUNDING_FAILURE=true
```

---

## 7. Task list (menu — prioritise from data)

| # | Service | Task | Prerequisite |
|---|---|---|---|
| 11.1 | astro+ai | Life timeline + user annotations | Phase 2 dashas |
| 11.2 | ai+go | Monthly/annual reports via Batch API | Phase 7 |
| 11.3 | astro | Personal astrology calendar + ICS export | Phase 2 |
| 11.4 | go+ai | Journaling + transit correlation | Phase 6 |
| 11.5 | go+astro | Family charts with a consent-based invite flow | Phase 2 |
| 11.6 | ai | Career and relationship assistants | Phase 6 memory |
| 11.7 | ai+go | Astrologer copilot over the Phase 8 socket | Phase 8 |
| 11.8 | ai | Regional language voices | Phase 9 |
| 11.9 | — | Funnel dashboards + cohort analysis | Phases 1–8 events |
| 11.10 | ai | Production eval sampling + alerting | Phase 6 harness |
| 11.11 | — | Cost optimisation pass (free wins first) | Phase 4 telemetry |
| 11.12 | go | Read replica + Metabase | Measured DB pressure |
| 11.13 | go | Table partitioning + archival | Measured table size |
| 11.14 | — | Chaos testing + runbooks + status page | — |
| 11.15 | — | **Backup restore drill** | — (do this now, regardless) |

---

## 8. Security and privacy checklist

- [ ] Family charts require explicit consent from each adult whose data is stored
- [ ] Journal entries private, encrypted, excluded from analytics
- [ ] Copilot suggestions never shown to the user and never attributed to the astrologer
- [ ] Copilot events scoped to the astrologer's socket by hub authorisation, not client filtering
- [ ] Copilot sees only what the user consented to share (Phase 8 rules unchanged)
- [ ] Production eval sampling uses anonymised or consented data
- [ ] Read replica has the same access controls as the primary; `ai-service` stays read-only against both
- [ ] Archived data retains its encryption and access controls
- [ ] Analytics tooling self-hosted or contractually restricted — behavioural data here is sensitive
- [ ] Correlation claims in journaling framed as observed patterns, never proof
- [ ] Backup restore tested, and the restored copy is as protected as production

---

## 9. Risks

| Risk | Mitigation |
|---|---|
| **Feature bloat** | This phase is a menu. Ship one item, measure, then decide the next. Anything without a metric moving is a candidate for deletion. |
| Premature scaling | Every item in §4 is gated on a measurement, not an intuition |
| Cost optimisation degrades quality | Full eval suite before and after; hard gates on safety and grounding |
| Overfitting to the eval set | Held-out test split, never used for iteration |
| GIL contention between voice and chat | Phase 9's concurrency test run against production-like load; split `voice-service` if it fails |
| Degradation paths untested | Chaos testing makes them explicit; the architecture's reliability dividend is worth nothing unproven |
| Copilot makes astrologers homogeneous | Suggestions only, never drafts; monitor consultation ratings after rollout |
| Journaling correlations read as pseudo-scientific proof | Careful framing, reviewed by someone outside the team |
| Family charts store non-consenting people's data | Invite-and-accept flow, not silent creation |

---

## 10. Phase Gate 🔒

There is no single gate here — this phase never "completes." Instead, **each item ships
behind its own gate:**

- [ ] Feature flag exists and defaults off
- [ ] Full Phase 6 eval suite green, including safety and grounding hard gates
- [ ] Analytics events instrumented before launch, not after
- [ ] Security checklist reviewed for the specific item
- [ ] Cost impact measured and accepted
- [ ] Cross-service contract regenerated if any service API changed
- [ ] Staged rollout plan with a rollback trigger defined in advance
- [ ] `docs/ARCHITECTURE.md` and `docs/DECISIONS.md` updated
- [ ] `task verify` and `task eval` green

Standing commitments for as long as the product runs:

- [ ] Backup restore drilled quarterly
- [ ] Production eval sampling running with alerting
- [ ] Cost per completed task tracked and reviewed monthly
- [ ] Held-out eval split kept genuinely held out
- [ ] **Both degradation paths verified: `ai-service` down and `astro-service` down**
