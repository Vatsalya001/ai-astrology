# Phase 8 — Human Astrologer Marketplace (Go)

| | |
|---|---|
| **Goal** | Let a user move from the AI to a real astrologer without losing context — and pay for it per minute, correctly. |
| **Deliverable** | Astrologer onboarding and verification, availability, matching, live chat and call consultations, per-minute billing, the AI→human handoff with consent, reviews and payouts. |
| **Depends on** | Phase 7 |
| **Unlocks** | Phase 11 |
| **Estimated size** | 25–35 days — the largest phase in the project |
| **Cost to run** | ₹0 in development — self-hosted LiveKit and Razorpay test mode |

> This phase turns a product into a two-sided marketplace: a materially harder
> operational business, where supply quality, liquidity, disputes, payouts and trust &
> safety all become your problem. Do not start it until Phases 6 and 7 have real users
> and real revenue proving the single-sided product works.

**This is the phase Go was chosen for.** Thousands of long-lived WebSocket connections,
a billing tick firing every 30 seconds per active session under contention, and
presence heartbeats — that workload is exactly what goroutines, channels and a real
type system handle well.

---

## 1. Scope

### In scope
- Astrologer onboarding, verification, profiles, specialisations
- Availability and presence (online/busy/offline)
- Matching and ranking
- Consultation booking: instant and scheduled
- Real-time chat consultation (WebSocket)
- Voice/video consultation (LiveKit)
- **Per-minute billing against the Phase 7 wallet**
- AI→human handoff with an explicit-consent context summary
- Reviews and ratings
- Astrologer earnings dashboard and payouts
- Trust and safety: reporting, moderation, suspension
- Admin: verification, dispute resolution, payout approval

### Out of scope
- Astrologer AI copilot (Phase 11)
- Group sessions, live streaming

---

## 2. Data model

```sql
CREATE TABLE astrologer_profiles (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               UUID NOT NULL UNIQUE REFERENCES users(id),  -- role=astrologer
    display_name          TEXT NOT NULL,
    photo_url             TEXT,
    bio                   TEXT NOT NULL,
    experience_years      SMALLINT NOT NULL,
    languages             TEXT[] NOT NULL,
    specializations       TEXT[] NOT NULL,
    qualifications        JSONB NOT NULL DEFAULT '[]',

    rate_per_minute_paise BIGINT NOT NULL,          -- integer. Always.
    currency              TEXT NOT NULL DEFAULT 'INR',

    verification_status   TEXT NOT NULL DEFAULT 'pending',
    verified_at           TIMESTAMPTZ,
    verification_notes    TEXT,

    presence              TEXT NOT NULL DEFAULT 'offline',
    last_seen_at          TIMESTAMPTZ,
    auto_accept_chat      BOOLEAN NOT NULL DEFAULT FALSE,

    rating_avg            REAL NOT NULL DEFAULT 0,
    rating_count          INTEGER NOT NULL DEFAULT 0,
    consult_count         INTEGER NOT NULL DEFAULT 0,
    completion_rate       REAL NOT NULL DEFAULT 1,
    avg_response_sec      INTEGER,
    is_active             BOOLEAN NOT NULL DEFAULT FALSE,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ap_discovery_idx ON astrologer_profiles (presence, is_active, rating_avg DESC);
CREATE INDEX ap_verification_idx ON astrologer_profiles (verification_status);

CREATE TABLE availability_slots (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    astrologer_id UUID NOT NULL REFERENCES astrologer_profiles(id) ON DELETE CASCADE,
    day_of_week   SMALLINT NOT NULL,      -- 0-6
    start_minute  SMALLINT NOT NULL,      -- minutes from midnight, astrologer's tz
    end_minute    SMALLINT NOT NULL,
    timezone      TEXT NOT NULL,
    is_recurring  BOOLEAN NOT NULL DEFAULT TRUE,
    specific_date DATE
);

CREATE TABLE consultations (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                  UUID NOT NULL REFERENCES users(id),
    astrologer_id            UUID NOT NULL REFERENCES astrologer_profiles(id),
    birth_profile_id         UUID NOT NULL REFERENCES birth_profiles(id),

    mode                     TEXT NOT NULL,   -- chat | voice | video
    status                   TEXT NOT NULL,   -- requested | accepted | rejected
                                              -- active | ended | cancelled
                                              -- expired | disputed
    rate_per_minute_paise    BIGINT NOT NULL, -- snapshotted at request time

    requested_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    accepted_at              TIMESTAMPTZ,
    started_at               TIMESTAMPTZ,
    ended_at                 TIMESTAMPTZ,
    ended_by                 TEXT,            -- user | astrologer
                                              -- system_low_balance | system_timeout

    billed_seconds           INTEGER NOT NULL DEFAULT 0,
    last_tick_number         INTEGER NOT NULL DEFAULT 0,
    amount_paise             BIGINT NOT NULL DEFAULT 0,
    platform_fee_paise       BIGINT NOT NULL DEFAULT 0,
    astrologer_earning_paise BIGINT NOT NULL DEFAULT 0,

    context_shared           BOOLEAN NOT NULL DEFAULT FALSE,
    context_summary          JSONB,           -- only if the user consented
    ai_conversation_id       UUID REFERENCES conversations(id) ON DELETE SET NULL,

    created_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cons_user_idx  ON consultations (user_id, requested_at DESC);
CREATE INDEX cons_astro_idx ON consultations (astrologer_id, requested_at DESC);
CREATE INDEX cons_active_idx ON consultations (status) WHERE status = 'active';

CREATE TABLE consultation_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consultation_id UUID NOT NULL REFERENCES consultations(id) ON DELETE CASCADE,
    sender_role     TEXT NOT NULL,           -- user | astrologer | system
    content         TEXT NOT NULL,
    attachment_url  TEXT,
    read_at         TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX cm_consultation_idx ON consultation_messages (consultation_id, created_at);

CREATE TABLE reviews (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    consultation_id   UUID NOT NULL UNIQUE REFERENCES consultations(id) ON DELETE CASCADE,
    user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    astrologer_id     UUID NOT NULL REFERENCES astrologer_profiles(id) ON DELETE CASCADE,
    rating            SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment           TEXT,
    astrologer_reply  TEXT,
    is_public         BOOLEAN NOT NULL DEFAULT TRUE,
    moderation_status TEXT NOT NULL DEFAULT 'pending',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payouts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    astrologer_id      UUID NOT NULL REFERENCES astrologer_profiles(id),
    period_start       TIMESTAMPTZ NOT NULL,
    period_end         TIMESTAMPTZ NOT NULL,
    gross_paise        BIGINT NOT NULL,
    platform_fee_paise BIGINT NOT NULL,
    tds_paise          BIGINT NOT NULL DEFAULT 0,
    net_paise          BIGINT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'pending',
    provider_payout_id TEXT,
    approved_by        UUID REFERENCES users(id),
    paid_at            TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (astrologer_id, period_start)
);
```

---

## 3. Per-minute billing — the hard part

This is where marketplaces lose money and trust. The failure modes are specific: the
user's balance runs out mid-call, the connection drops and both sides disagree about
when it ended, or a clock skew means the astrologer's timer and yours disagree.

### Rules

1. **The server owns the clock.** Never trust a client-reported duration. `started_at` and `ended_at` are set server-side from session lifecycle events.
2. **Pre-authorise before connecting.** At request time, verify the wallet holds at least `MIN_CONSULTATION_MINUTES × rate`.
3. **Bill in 30-second ticks, rounding up.** A ticker debits the wallet every 30 s of active session. Charging only at the end means a dropped connection loses the whole session's revenue.
4. **Warn, then end.** At 2 minutes of remaining balance, warn both parties in-session. At zero, end gracefully with a clear message — never mid-sentence with a generic error.
5. **Free grace period.** The first 60 seconds are unbilled. It covers connection setup and lets the user decide the match is right. Costs little; removes the biggest source of "I was charged for nothing" disputes.
6. **Every tick is a ledger entry.** Same Phase 7 rules — integer paise, append-only, idempotent per tick number.

### The Go implementation

```go
func (s *Session) runBilling(ctx context.Context) {
    ticker := time.NewTicker(billingTickInterval)   // 30s
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return                                   // session ended or cancelled
        case <-ticker.C:
            if err := s.billOneTick(ctx); err != nil {
                if errors.Is(err, ErrInsufficientBalance) {
                    s.endGracefully(ctx, EndedBySystemLowBalance)
                    return
                }
                s.log.Error("billing tick failed", "err", err, "consultation", s.id)
                // Do NOT end the session on a transient error — retry next tick.
                // Losing a tick costs you 30s of revenue; ending a paid call
                // because Postgres blipped costs you a customer.
            }
        }
    }
}

func (s *Session) billOneTick(ctx context.Context) error {
    tick := s.tickNumber.Add(1)
    return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
        q := dbgen.New(tx)

        // Idempotency: this tick number for this consultation, once only.
        if err := q.ClaimTick(ctx, s.id, tick); errors.Is(err, ErrAlreadyClaimed) {
            return nil
        }

        wallet, err := q.LockWallet(ctx, s.userID)   // SELECT ... FOR UPDATE
        if err != nil { return err }

        amount := s.rate * Paise(billingTickSeconds) / 60
        if wallet.BalancePaise < amount { return ErrInsufficientBalance }

        if err := q.InsertLedgerPair(ctx, ...); err != nil { return err }
        return q.AdvanceBilledSeconds(ctx, s.id, billingTickSeconds)
    })
}
```

One goroutine per active session, cancelled by context when the session ends. This is
the shape of code Go makes straightforward and that is genuinely awkward elsewhere.

**On a tick failing:** retry next tick rather than terminating. Dropping one tick costs
30 seconds of revenue; ending a paying customer's call because the database blipped
costs the customer.

### Revenue split

```go
amount      := Paise(billedSeconds) * ratePerMinute / 60
platformFee := amount * Paise(feePercent) / 100        // integer division, once
earning     := amount - platformFee                    // remainder goes to the astrologer
```

Compute the split once, at session end, write it to the row. Recomputing later from a
percentage that has since changed is how earnings disputes start. Subtracting rather
than computing both sides independently guarantees the parts sum exactly to the whole —
no rounding leak.

---

## 4. The AI → human handoff

The feature that justifies having both. Done badly it is a privacy incident; done well
it is the best moment in the product.

```
User: "I want to talk to someone about my marriage."
   ↓  intent: HUMAN_ASTROLOGER  (Phase 4 classifier)
   ↓
AI offers: [ Talk to an astrologer ]
   ↓
Go asks ai-service to draft a context summary
   ↓
╔════════════════════════════════════════════╗
║  Share context with your astrologer?       ║
║                                            ║
║  This is what they'll see:                 ║
║  ┌──────────────────────────────────────┐  ║
║  │ • Asking about marriage timing       │  ║
║  │ • Birth chart: Aries asc, Taurus Moon│  ║
║  │ • Current: Jupiter/Saturn dasha      │  ║
║  │ • 7th house: Libra, Venus in own sign│  ║
║  │ • Mentioned family pressure          │  ║
║  └──────────────────────────────────────┘  ║
║                                            ║
║  [ Share this ]   [ Share chart only ]     ║
║  [ Share nothing ]                         ║
╚════════════════════════════════════════════╝
```

### The rules

- **Show the user the exact text before sharing it.** Not a description — the literal content the astrologer will receive.
- **Three explicit choices**, defaulting to nothing. Never opt-out.
- **Never include long-term memories** unless separately consented. A memory extracted six weeks ago about a health worry is not something the user expects to arrive in a marriage consultation.
- **Never include AI internals** — system prompts, confidence scores, safety flags, intent labels. The astrologer sees user-facing facts only.
- Store what was shared on the `consultations` row so a later dispute is answerable.

Getting this wrong is the single most damaging privacy failure available in this
product. Getting it right visibly demonstrates that you take the user's side.

Enforce the exclusions in code, not prompt instructions: Go builds the summary request
and simply does not pass memories or internals to `ai-service`. A test asserts the
request payload contains neither.

---

## 5. Matching

```go
type MatchRequest struct {
    Intent      Intent
    Language    string
    MaxRatePaise *Paise
    Mode        ConsultMode
    NeedNow     bool
}
```

Ranking score:

```
0.30 · specialisationMatch(intent)
0.20 · languageMatch
0.20 · normalizedRating          (Bayesian-smoothed — see below)
0.15 · availability              (online now > online soon > scheduled)
0.10 · priceFit
0.05 · newAstrologerBoost        (decays after 20 consultations)
```

**Bayesian smoothing on ratings.** A new astrologer with one 5-star review must not
outrank someone with 4.8 over 300 reviews:

```go
smoothed := (ratingAvg*float64(ratingCount) + priorMean*priorWeight) /
            (float64(ratingCount) + priorWeight)
// priorMean = 4.2, priorWeight = 20
```

**The new-astrologer boost is a supply-side investment, not charity.** Without it, new
astrologers get no consultations, get no reviews, and churn — and your supply stops
growing. Cap it and decay it.

**Never rank by revenue-per-minute.** Steering users to expensive astrologers because
they earn you more is a trust-destroying pattern that users detect quickly and that
regulators increasingly notice. Rank by fit.

---

## 6. Real-time infrastructure

### Chat — WebSocket (`coder/websocket`)

Genuinely bidirectional, unlike the Phase 5 AI chat, so SSE is no longer the right tool.

```go
type Hub struct {
    mu       sync.RWMutex
    sessions map[uuid.UUID]*Session      // consultationID → session
    redis    *redis.Client               // cross-instance fan-out via pub/sub
}
```

Events:

```
consultation:request      consultation:accept       consultation:reject
consultation:start        message:send              message:delivered
message:read              typing:start / typing:stop
billing:tick              billing:low_balance       consultation:end
presence:update
```

Redis pub/sub for multi-instance fan-out — with more than one API replica, the two
parties to a consultation may be connected to different instances.

Authenticate on connect; **re-verify authorisation on every consultation-scoped event**.
A socket authorised once is not authorised forever — a user whose session is revoked
mid-consultation must stop receiving messages.

Read deadlines and ping/pong keepalives on every connection, and a bounded send buffer
per client: a slow consumer must be disconnected, not allowed to grow an unbounded
queue in memory.

### Voice / video — LiveKit

**LiveKit is open source and self-hostable, so development costs nothing.** The official
Go SDK mints room tokens server-side.

```yaml
livekit:
  image: livekit/livekit-server
  command: --dev
  ports: ["7880:7880", "7881:7881", "7882:7882/udp"]
```

Short-lived JWT room tokens scoped to a single consultation. Recording is **off by
default** — if you ever enable it, both parties must consent explicitly and visibly, and
the legal position on recording consultations differs by jurisdiction.

Self-host in production too, or move to LiveKit Cloud when scale justifies it. The
client code is identical either way.

### Presence

Redis with a heartbeat: `presence:{astrologerID}` → `{status, lastBeat}`, TTL 45 s,
heartbeat every 15 s. A missed heartbeat degrades to `offline` automatically — an
astrologer whose phone died must not keep receiving requests.

---

## 7. Astrologer experience

| Screen | Route | Contents |
|---|---|---|
| Onboarding | `/astrologer/apply` | Multi-step: identity, qualifications, specialisations, languages, rate, sample reading |
| Dashboard | `/astrologer` | Online toggle, today's earnings, pending requests, upcoming |
| Requests | `/astrologer/requests` | Incoming with a 60-second accept timer |
| Active session | `/astrologer/session/{id}` | Chat/call + shared context panel + live timer |
| Consultations | `/astrologer/consultations` | History, transcripts, ratings |
| Availability | `/astrologer/availability` | Weekly schedule + exceptions |
| Earnings | `/astrologer/earnings` | Per-consultation breakdown, payout schedule, TDS |
| Reviews | `/astrologer/reviews` | With a right of reply |
| Profile | `/astrologer/profile` | Edits re-enter review |

```
┌──────────────────────────────────────────┐
│  ● Online    [ Go offline ]              │
│                                          │
│  TODAY                                   │
│  ₹2,340 earned · 7 consultations · 94 min│
│                                          │
│  ┌────────────────────────────────────┐  │
│  │ ⚡ New request                      │  │
│  │ Marriage · Hindi · Chat            │  │
│  │ Context shared ✓                   │  │
│  │ Accept in 47s                      │  │
│  │ [ Accept ]        [ Decline ]      │  │
│  └────────────────────────────────────┘  │
│                                          │
│  THIS WEEK                               │
│  ₹14,820 · 43 consultations · ★ 4.7      │
│  Next payout: Mon 15 Sep · ₹13,338       │
└──────────────────────────────────────────┘
```

### Verification

Manual, human review. Identity document, claimed qualifications, a written sample
reading, and a short video interview. Automating this is a false economy — supply
quality is the entire value of a marketplace, and one fraudulent astrologer costs more
in refunds and reputation than a hundred manual reviews cost in time.

Store verification documents encrypted, access restricted and audit-logged, and delete
them once verification completes. You need the *decision* in perpetuity, not the
document.

---

## 8. User experience

| Screen | Route |
|---|---|
| Astrologer list | `/astrologers` — filters: specialisation, language, price, online now |
| Astrologer profile | `/astrologers/{id}` — bio, rate, reviews, availability, [Chat now] |
| Consent sheet | modal — the handoff dialog above |
| Active consultation | `/consultation/{id}` — chat/call, timer, balance, end button |
| History | `/consultations` — past sessions, transcripts, receipts |
| Review | `/consultations/{id}/review` — post-session prompt |

Active-session header — always visible, never hidden:

```
┌──────────────────────────────────────────┐
│  Pt. Sharma · ★4.8        ⏱ 04:32        │
│  ₹18/min · Balance: ₹340 (18 min left)   │
│                             [ End ]      │
└──────────────────────────────────────────┘
```

Showing remaining **minutes** continuously — not just a balance — is what prevents the
"I didn't realise I was being charged" dispute. Be relentlessly clear about money.

---

## 9. Payouts

Weekly, with a T+3 hold after session end to allow disputes.

```
Weekly asynq task
  → aggregate ended, undisputed consultations per astrologer
  → gross − platform fee − TDS = net
  → payouts(status=pending)
  → admin approval          ← human gate before money leaves
  → provider payout API (RazorpayX / bank transfer)
  → webhook → status=paid → notify
```

**TDS applies** to Indian astrologer payouts. Get this reviewed by an accountant before
the first payout, and generate the certificates the law requires. It is not optional and
not something to figure out retroactively.

Payouts use the same append-only ledger. The `UNIQUE (astrologer_id, period_start)`
constraint makes the weekly job idempotent — running it twice cannot double-pay.

---

## 10. Trust and safety

| Risk | Control |
|---|---|
| Fraudulent astrologer | Manual verification, video interview, probation period with lower ranking |
| Off-platform solicitation | Filter contact details in chat; warn; suspend on repeat |
| Harmful predictions (death, disease, "you're cursed") | Prohibited in the astrologer agreement; report button; transcript review on report; suspension |
| Exploitative upselling ("buy this ₹50,000 puja") | Prohibited; keyword monitoring on reported transcripts |
| User abuse of astrologers | Report flow for astrologers too, with the same weight |
| Payment disputes | T+3 hold, full transcripts, server-side timing, admin resolution |
| Minors | Age gate at signup; consultations 18+ |

Publish an astrologer code of conduct and require acceptance during onboarding. Both
sides need a reporting route — a marketplace that only protects buyers loses its
suppliers.

---

## 11. Environment variables added

```bash
FEATURE_HUMAN_ASTROLOGER_ENABLED=true

PLATFORM_FEE_PERCENT=25
TDS_PERCENT=10
MIN_CONSULTATION_MINUTES=3
BILLING_TICK_INTERVAL=30s
FREE_GRACE_SECONDS=60
LOW_BALANCE_WARN_MINUTES=2
REQUEST_ACCEPT_TIMEOUT=60s
MAX_SESSION_DURATION=120m
PAYOUT_HOLD_DAYS=3
PAYOUT_CRON=0 5 * * 1

LIVEKIT_URL=ws://localhost:7880
LIVEKIT_API_KEY=devkey
LIVEKIT_API_SECRET=secret
LIVEKIT_ROOM_TTL=120m
CONSULTATION_RECORDING_ENABLED=false

WS_READ_DEADLINE=60s
WS_PING_INTERVAL=25s
WS_SEND_BUFFER=64
PRESENCE_HEARTBEAT=15s
PRESENCE_TTL=45s
```

---

## 12. Task list

| # | Task | Done when |
|---|---|---|
| 8.1 | Marketplace migration + sqlc queries | |
| 8.2 | Onboarding flow + encrypted document upload | |
| 8.3 | Admin verification queue with approve/reject/notes | |
| 8.4 | Profile CRUD; edits re-enter review | |
| 8.5 | Availability + presence (Redis heartbeat) | Missed heartbeat degrades to offline |
| 8.6 | Matching with Bayesian smoothing + new-astrologer boost | Ranking unit-tested |
| 8.7 | Astrologer list and profile UI with filters | |
| 8.8 | Request → accept/reject → timeout | 60 s timer works both ways |
| 8.9 | **Consent sheet showing the literal shared text** | Three options; default nothing |
| 8.10 | Context summary via `ai-service`, memories/internals excluded in Go | Test asserts the request payload excludes them |
| 8.11 | WebSocket hub: auth, per-event authz, deadlines, bounded buffers, Redis fan-out | Slow consumer disconnected, not buffered |
| 8.12 | LiveKit self-hosted + Go token minting + call UI | Works locally at zero cost |
| 8.13 | **Billing goroutine: pre-auth, 30 s ticks, idempotent, warn, graceful end** | Concurrency + drop tests green |
| 8.14 | Revenue split computed once at session end | Parts sum exactly to the whole |
| 8.15 | Session end on every path: user, astrologer, low balance, timeout, disconnect | Each billed correctly |
| 8.16 | Reviews + moderation + right of reply | |
| 8.17 | Rating aggregation with smoothing | |
| 8.18 | Astrologer dashboard, requests, session, earnings, reviews | |
| 8.19 | Payout aggregation, TDS, admin approval, provider payout | Idempotent by unique constraint |
| 8.20 | Trust and safety: two-way reporting, contact filter, suspension | |
| 8.21 | Admin: verification, disputes, payouts, moderation | |

---

## 13. Testing

**Billing — the critical suite. Run these for real, under `-race`:**
- A 4m 37s session bills 5 minutes (round up), minus the 60 s grace
- Balance exhausts mid-session → warned at 2 min → ended gracefully at 0 → billed exactly to the tick
- Client disconnects → session ends server-side → billed to the last completed tick, not to zero and not to the timeout
- Duplicate tick number → charged once
- Concurrent ticks → no double charge
- A transient DB error on one tick → session continues, next tick succeeds
- Astrologer and user both press End simultaneously → one end event, one bill
- Revenue split sums exactly to `amount_paise` with no rounding leak

**Handoff** — consent sheet shows the literal text; "share nothing" shares nothing;
long-term memories never appear in a context summary; no AI internals leak. Assert on
the outbound request payload, not just the rendered output.

**Matching** — a new astrologer with one 5★ does not outrank 4.8/300; language and
specialisation filters are hard, not soft.

**Real-time** — reconnection resumes the session; messages queued while offline are
delivered; presence degrades on missed heartbeat; multi-instance fan-out via Redis
(run two API instances in the test); slow consumer is disconnected.

**E2E** — `AI chat → request astrologer → consent → matched → accepted → chat 5 min →
end → billed correctly → review → astrologer sees earnings → payout generated`.

**Load** — 100 concurrent consultations with billing ticks running. Assert no missed
ticks, no double bills, the ledger balances, and goroutine count returns to baseline
after all sessions end (a leaked billing goroutine per session is a real and easy bug).

---

## 14. Security checklist

- [ ] Astrologers see only what the user explicitly consented to share
- [ ] **Memories and AI internals excluded in Go, not by prompt instruction** — asserted on the request payload
- [ ] WebSocket authenticated on connect and **re-authorised per consultation event**
- [ ] WS read deadlines, ping/pong, bounded send buffers; slow consumers disconnected
- [ ] LiveKit tokens short-lived and scoped to one room
- [ ] Recording off by default; enabling requires visible two-party consent
- [ ] Verification documents encrypted, access-restricted, audit-logged, deleted after decision
- [ ] All money integer paise; ticks idempotent; ledger append-only
- [ ] Server-side timing only; client-reported durations never trusted
- [ ] Contact-detail filtering in consultation chat
- [ ] Astrologers cannot browse users; they see only their own consultations
- [ ] Rate limiting on consultation requests (prevents request spam at astrologers)
- [ ] Reviews moderated before public display
- [ ] Payouts require admin approval; every approval audit-logged
- [ ] Payout job idempotent (unique constraint proven under a double run)
- [ ] TDS computed and certificates generated
- [ ] Transcripts retained per a documented policy and included in data export

---

## 15. Risks

| Risk | Mitigation |
|---|---|
| **Billing disputes** | Server-side clock, 30 s ticks, visible countdown, grace period, full transcripts, T+3 payout hold |
| Leaked billing goroutines | Context cancellation on every end path; load test asserts goroutine count returns to baseline |
| Supply liquidity — nobody online when users want someone | Scheduled bookings, availability incentives, new-astrologer boost, "notify when online" |
| Supply quality | Manual verification, probation, continuous rating monitoring, fast suspension |
| Off-platform leakage | Contact filtering, and — more effectively — make the platform genuinely better: history, payment protection, dispute resolution |
| Marketplace complexity swamps the roadmap | 25–35 days. Do not start until the single-sided product has proven revenue. |
| Harmful predictions by humans | Code of conduct, reporting, transcript review, suspension. You cannot prompt-engineer a person — you need policy and enforcement. |
| TDS/GST non-compliance on payouts | Accountant review before the first payout, without exception |

---

## 16. Definition of Done

Global DoD **plus**:

- [ ] A user can go from AI chat to a live human consultation with explicit, literal consent
- [ ] Per-minute billing correct across every termination path, proven under concurrency
- [ ] Astrologers manually verified before going live
- [ ] Earnings and payouts accurate, TDS handled and accountant-reviewed
- [ ] Both sides can report; moderation and suspension work
- [ ] The whole stack runs locally on self-hosted LiveKit at zero cost

---

## 17. Phase Gate 🔒

- [ ] Astrologer onboarding and manual verification working end to end
- [ ] Availability and presence accurate; missed heartbeat degrades to offline
- [ ] Matching ranks sensibly; Bayesian smoothing prevents new-astrologer inflation
- [ ] Request → accept/reject → timeout works with a 60 s timer
- [ ] **Consent sheet shows the literal text; "share nothing" shares nothing (tested)**
- [ ] **Memories and AI internals never reach `ai-service` in the summary request (tested)**
- [ ] WebSocket chat with delivery, read receipts and typing indicators
- [ ] Per-event authorisation on the socket; revoked session stops receiving
- [ ] LiveKit voice and video working on the self-hosted dev server
- [ ] **Billing correct on every end path: user end, astrologer end, low balance, timeout, disconnect**
- [ ] **Billing concurrency and idempotency tests green under `-race`; ledger balances**
- [ ] Transient tick failure does not end the session
- [ ] Grace period, low-balance warning and graceful end all verified
- [ ] Revenue split computed once and sums exactly
- [ ] Reviews, moderation, right of reply, smoothed aggregation
- [ ] Astrologer dashboard, earnings and payout flow complete
- [ ] Payouts: TDS computed, admin approval gate, idempotent, accountant-reviewed
- [ ] Two-way reporting, contact filtering, suspension all working
- [ ] **Load test: 100 concurrent consultations, no missed or duplicate ticks, no goroutine leak**
- [ ] `task verify` and `task eval` green
- [ ] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
