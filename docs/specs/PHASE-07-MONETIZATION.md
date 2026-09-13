# Phase 7 — Subscriptions, Credits & Payments (Go)

| | |
|---|---|
| **Goal** | Charge money — correctly, auditably, and without ever losing or double-charging a rupee. |
| **Deliverable** | Subscriptions, AI credits, a wallet with a double-entry ledger, Razorpay/Stripe integration, usage limits and a billing portal. |
| **Depends on** | Phase 6 (MVP complete) |
| **Unlocks** | Phase 8 |
| **Estimated size** | 14–20 days |
| **Cost to run** | ₹0 in development — Razorpay and Stripe test modes are free and unlimited |

> **This is the highest-risk phase in the project.** Money bugs are not like UI bugs:
> they are discovered by angry users, they compound silently, and reconciling them costs
> more than building the feature correctly did. Every rule in §3 exists because skipping
> it has burned somebody.

Entirely a Go phase. Neither Python service is involved — which is exactly right, since
correctness under concurrency is what Go is here for.

---

## 1. Scope

### In scope
- Subscription plans, lifecycle, upgrade/downgrade/cancel
- AI credits: purchase, consumption, expiry, refund
- Wallet with a double-entry ledger
- Razorpay (UPI, cards, netbanking); Stripe for international
- Webhook handling with idempotency and replay safety
- Usage metering and limits, driven by Phase 4 telemetry
- Paywall UX, pricing page, billing portal
- Invoices with GST
- Reconciliation job and an admin finance view
- Refunds

### Out of scope
- Astrologer payouts (Phase 8)
- Per-minute consultation billing (Phase 8)

---

## 2. Monetisation model

Three revenue surfaces, launched in this order:

| Surface | What | Why first/later |
|---|---|---|
| **Free tier** | Kundli, daily astrology, 5 AI messages/day | The acquisition engine. Never cripple it — the Kundli and chart are what get shared. |
| **Subscription** | Unlimited AI chat, all personas, premium reports | Predictable revenue, best margin, simplest to operate |
| **AI credits** | Pay-as-you-go packs | For users who won't subscribe; also the rail Phase 8 consultations run on |

Indicative plans (validate with real pricing research before launch):

| Plan | Price | Includes |
|---|---|---|
| Free | ₹0 | Kundli, daily astrology, 5 messages/day, 1 birth profile |
| Plus | ₹199/mo | Unlimited chat, all personas, 3 profiles, compatibility, PDF reports |
| Pro | ₹499/mo | Plus + `deep` tier model, monthly report, voice (Phase 9), priority |
| Credits | ₹99 / ₹299 / ₹799 | 100 / 350 / 1000 credits |

Credits are priced from the Phase 4 `cost_micros` telemetry plus margin. Because that
telemetry already exists and is already integer-typed, credit pricing is a calculation
rather than a guess.

---

## 3. Money handling rules — non-negotiable

These are not stylistic preferences.

### 3.1 Integer base units only. Never floats.

```go
type Paise int64          // ₹199.00 → Paise(19900)
```

```sql
amount_paise BIGINT NOT NULL
```

`BIGINT` in Postgres, `int64` in Go. Never `NUMERIC`, never `float64`, never a float
crossing the JSON boundary. Every amount, everywhere — DB, API, domain logic — is an
integer count of paise. Format to rupees at the presentation layer only.

Define a distinct `Paise` type rather than using bare `int64`: the compiler then stops
you from accidentally passing a token count where an amount belongs.

**JSON note:** `int64` exceeding 2^53 loses precision in JavaScript. Paise amounts here
never come close, but serialise money as a string in any public API response if you want
to be safe against a future currency with more decimal places.

### 3.2 Double-entry ledger. Append-only.

```sql
CREATE TABLE ledger_entries (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transaction_id UUID NOT NULL,                  -- groups the debit and credit
    account_type   TEXT NOT NULL,                  -- user_wallet | revenue
                                                   -- gateway_receivable | promo | refund
    account_id     TEXT NOT NULL,
    direction      TEXT NOT NULL CHECK (direction IN ('debit','credit')),
    amount_paise   BIGINT NOT NULL CHECK (amount_paise > 0),
    currency       TEXT NOT NULL DEFAULT 'INR',
    description    TEXT NOT NULL,
    metadata       JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX le_account_idx ON ledger_entries (account_id, created_at DESC);
CREATE INDEX le_txn_idx     ON ledger_entries (transaction_id);

-- Append-only, enforced by the database rather than by discipline.
CREATE RULE ledger_no_update AS ON UPDATE TO ledger_entries DO INSTEAD NOTHING;
CREATE RULE ledger_no_delete AS ON DELETE TO ledger_entries DO INSTEAD NOTHING;
```

**Never `UPDATE` a balance.** A balance is the sum of its ledger entries. Corrections are
new, opposing entries — never edits. This is what makes disputes resolvable: you can
always reconstruct exactly what happened and when.

An invariant test asserts that for every `transaction_id`, debits equal credits. Run it
in CI and as a nightly job.

### 3.3 Idempotency on every money-moving operation

```sql
CREATE TABLE idempotency_keys (
    key           TEXT PRIMARY KEY,
    user_id       UUID NOT NULL,
    operation     TEXT NOT NULL,
    request_hash  BYTEA NOT NULL,
    response_body JSONB,
    status        TEXT NOT NULL,     -- in_progress | completed | failed
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL
);
```

Client generates the key (`Idempotency-Key` header); the server returns the stored
response on replay. Webhooks use the provider's event ID as the key. Without this, a
network retry becomes a double charge — and mobile networks retry constantly.

Middleware in Go, applied to every money route, so it cannot be forgotten on a new
endpoint.

### 3.4 Row locks on balance mutations

```sql
-- name: LockWallet :one
SELECT * FROM wallets WHERE user_id = $1 FOR UPDATE;
```

```go
tx, err := pool.Begin(ctx)
defer tx.Rollback(ctx)

q := dbgen.New(tx)
wallet, err := q.LockWallet(ctx, userID)      // blocks concurrent writers
// ... check balance, insert ledger entries ...
return tx.Commit(ctx)
```

Two concurrent credit spends without the lock will both read the same balance and both
succeed — the classic double-spend. Test it with genuinely concurrent requests under
`-race`, not by reasoning about it.

### 3.5 Never store card data

PCI scope is not something to take on. Tokenised, provider-hosted checkout only. The
database stores a provider customer ID and the last four digits for display. Nothing
else, ever.

### 3.6 The provider is the source of truth

Local state is a cache of the provider's state. Reconcile nightly; when they disagree,
the provider wins and the discrepancy is logged for human review.

---

## 4. Data model

```sql
CREATE TABLE plans (
    id               TEXT PRIMARY KEY,              -- free | plus | pro
    name             TEXT NOT NULL,
    price_paise      BIGINT NOT NULL,
    currency         TEXT NOT NULL DEFAULT 'INR',
    interval         TEXT NOT NULL,                 -- month | year
    features         JSONB NOT NULL,
    limits           JSONB NOT NULL,                -- messages_per_day, profiles, …
    provider_plan_id TEXT,
    is_active        BOOLEAN NOT NULL DEFAULT TRUE,
    sort_order       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE subscriptions (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    plan_id              TEXT NOT NULL REFERENCES plans(id),
    status               TEXT NOT NULL,             -- trialing | active | past_due
                                                    -- cancelled | expired
    provider_sub_id      TEXT UNIQUE,
    current_period_start TIMESTAMPTZ NOT NULL,
    current_period_end   TIMESTAMPTZ NOT NULL,
    cancel_at_period_end BOOLEAN NOT NULL DEFAULT FALSE,
    cancelled_at         TIMESTAMPTZ,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX sub_status_idx ON subscriptions (status, current_period_end);

CREATE TABLE wallets (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    balance_paise  BIGINT NOT NULL DEFAULT 0,   -- DERIVED cache. Never authoritative.
    credit_balance INTEGER NOT NULL DEFAULT 0,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE transactions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type                TEXT NOT NULL,      -- subscription | credit_purchase
                                            -- wallet_topup | credit_spend | refund
                                            -- consultation (Phase 8)
    status              TEXT NOT NULL,      -- created | pending | succeeded
                                            -- failed | refunded
    amount_paise        BIGINT NOT NULL,
    currency            TEXT NOT NULL DEFAULT 'INR',
    gst_paise           BIGINT NOT NULL DEFAULT 0,
    provider            TEXT,
    provider_order_id   TEXT UNIQUE,
    provider_payment_id TEXT UNIQUE,
    idempotency_key     TEXT UNIQUE,
    failure_reason      TEXT,
    metadata            JSONB NOT NULL DEFAULT '{}',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX txn_user_idx   ON transactions (user_id, created_at DESC);
CREATE INDEX txn_status_idx ON transactions (status, created_at DESC);

CREATE TABLE credit_grants (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount         INTEGER NOT NULL,
    remaining      INTEGER NOT NULL,
    source         TEXT NOT NULL,          -- purchase | promo | refund | bonus
    transaction_id UUID REFERENCES transactions(id),
    expires_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- FIFO consumption by expiry
CREATE INDEX cg_user_expiry_idx ON credit_grants (user_id, expires_at NULLS LAST)
    WHERE remaining > 0;

CREATE TABLE usage_records (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date          DATE NOT NULL,
    job_type      TEXT NOT NULL,
    count         INTEGER NOT NULL DEFAULT 0,
    credits_spent INTEGER NOT NULL DEFAULT 0,
    cost_micros   BIGINT NOT NULL DEFAULT 0,   -- real cost vs revenue
    UNIQUE (user_id, date, job_type)
);

CREATE TABLE webhook_events (
    id           TEXT PRIMARY KEY,          -- provider event ID = idempotency key
    provider     TEXT NOT NULL,
    event_type   TEXT NOT NULL,
    payload      JSONB NOT NULL,
    signature    TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'received',  -- received | processed
                                                    -- failed | ignored
    attempts     INTEGER NOT NULL DEFAULT 0,
    processed_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX we_status_idx ON webhook_events (status, created_at);
```

---

## 5. Payment flow

```
Client                    api-service (Go)          Razorpay
  │  POST /payments/order   │                         │
  │  + Idempotency-Key      │                         │
  ├────────────────────────►│                         │
  │                         │ transaction(created)    │
  │                         ├────── create order ────►│
  │◄──── order_id, key ─────┤                         │
  │                                                   │
  │─────── provider-hosted checkout ─────────────────►│
  │◄────── signed payment response ───────────────────┤
  │                         │                         │
  │  POST /payments/verify  │                         │
  ├────────────────────────►│ verify HMAC signature   │
  │                         │ ⚠ provisional only      │
  │◄──── pending ───────────┤                         │
  │                         │◄──── webhook ───────────┤
  │                         │ verify signature        │
  │                         │ idempotency check       │
  │                         │ ledger entries (one tx) │
  │                         │ grant credits / activate│
  │◄──── realtime update ───┤                         │
```

**The webhook is authoritative, not the client callback.** A client can be intercepted,
replayed, or simply lost when the user closes the tab. The client response gives instant
UX feedback ("payment received, confirming…"); the webhook is what actually moves money
in your ledger. Conflating the two is the most common payment integration bug there is.

### Webhook handling

1. Verify the HMAC signature with `hmac.Equal` — **constant time**. Reject unsigned or mismatched; no exceptions, no debug bypass.
2. Persist the raw event **before** processing (`webhook_events`).
3. Idempotency check on the provider event ID — already processed → 200, no-op.
4. Process inside one `pgx` transaction: ledger entries + state change together, or neither.
5. Return 200 quickly; do slow work via `asynq`. Providers retry on timeout, and a slow handler creates duplicate deliveries.
6. Unknown event types → store with `status = 'ignored'`. Never 500 on an unrecognised event; that triggers endless retries.

```go
// Read the raw body BEFORE any JSON decoding — signature is over raw bytes.
body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBytes))
expected := hmacSHA256(body, secret)
if !hmac.Equal(expected, providedSig) {
    http.Error(w, "invalid signature", http.StatusUnauthorized)
    return
}
```

Decoding and re-encoding before verifying is a classic signature bug — key order and
whitespace change, and the HMAC no longer matches.

---

## 6. Usage limits and metering

```go
func (s *Service) CheckQuota(ctx context.Context, userID uuid.UUID, job JobType) (QuotaResult, error) {
    plan := s.planFor(ctx, userID)                 // FREE_PLAN if no subscription
    used := s.todayUsage(ctx, userID, job)

    if used < plan.Limits.MessagesPerDay {
        return QuotaResult{Allowed: true, Source: SourcePlan}, nil
    }
    if s.creditBalance(ctx, userID) >= CreditCost[job] {
        return QuotaResult{Allowed: true, Source: SourceCredits}, nil
    }
    return QuotaResult{Allowed: false, Reason: QuotaExceeded}, nil
}
```

Checked **before** the AI call, and the credit deducted **after** a successful response.
Charging for a failed generation is the fastest way to lose a paying user's trust. If a
response fails validation and is regenerated, the user pays once.

Credits consume FIFO by expiry date so the soonest-to-expire grant is used first —
fairer to the user and simpler to reason about at year end.

---

## 7. Paywall UX

The paywall is a product surface, not a wall. It should arrive at a moment of
demonstrated value.

```
┌──────────────────────────────────────────┐
│  You've used your 5 free questions today │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  ✦  Plus                ₹199/month │  │
│  │                                    │  │
│  │  ✓ Unlimited AI conversations      │  │
│  │  ✓ All four guide personas         │  │
│  │  ✓ Compatibility reports           │  │
│  │  ✓ Up to 3 birth profiles          │  │
│  │  ✓ Downloadable PDF reports        │  │
│  │                                    │  │
│  │        [ Start Plus ]              │  │
│  └────────────────────────────────────┘  │
│                                          │
│  Or buy credits — no subscription        │
│  [ 100 credits · ₹99 ]                   │
│                                          │
│  Your free questions reset in 6h 22m.    │
└──────────────────────────────────────────┘
```

Principles that matter:

- **Never paywall the Kundli, the chart or the daily horoscope.** Those are the shared artifacts that bring new users in. Paywalling acquisition is self-harm.
- Always show when free access resets. "Come back tomorrow" is a legitimate, non-hostile option and users respect being told it plainly.
- Always offer credits alongside subscription. Many Indian users prefer one-time payments over recurring mandates.
- UPI first in the payment method ordering. It is the dominant rail.
- Never dark-pattern the cancel flow. Cancellation is two taps from the billing portal, and the subscription stays active to period end.

### Other screens

| Screen | Route |
|---|---|
| Pricing | `/pricing` — public, comparison table, FAQ |
| Checkout | `/checkout/{planId}` |
| Billing portal | `/settings/billing` — plan, next charge, method, invoices, cancel |
| Credits | `/settings/credits` — balance, expiry, purchase, history |
| Usage | `/settings/usage` — what you've used this period |
| Invoice | `/invoices/{id}` — GST-compliant, downloadable |

---

## 8. Free development setup

| Need | Free option |
|---|---|
| Razorpay | **Test mode** — free, unlimited, full webhook support, test UPI/card/netbanking |
| Stripe | **Test mode** — free, unlimited, excellent CLI for local webhook forwarding |
| Webhook tunnelling | `stripe listen --forward-to localhost:4000/webhooks/stripe` (free) · ngrok free tier |
| Invoice PDFs | Reuse the Phase 3 `chromedp` worker |
| Failure simulation | Both providers give test cards for decline, insufficient funds, 3DS challenge, network timeout |

**Test every failure path, not just the happy one.** Declines, timeouts mid-payment,
duplicate webhooks, out-of-order webhooks, webhooks for unknown users, and a webhook
arriving before the client callback. All free to simulate; all happen in production.

---

## 9. Environment variables added

```bash
FEATURE_PAYMENTS_ENABLED=true
FEATURE_PREMIUM_AI_ENABLED=true

PAYMENT_PROVIDER=razorpay             # razorpay | stripe
RAZORPAY_KEY_ID=
RAZORPAY_KEY_SECRET=
RAZORPAY_WEBHOOK_SECRET=
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SECRET=

CURRENCY=INR
GST_PERCENT=18
GSTIN=
COMPANY_LEGAL_NAME=
COMPANY_ADDRESS=

FREE_MESSAGES_PER_DAY=5
CREDIT_COST_CHAT=1
CREDIT_COST_DEEP=5
CREDIT_COST_REPORT=25
CREDIT_EXPIRY_DAYS=365

IDEMPOTENCY_KEY_TTL=24h
MAX_WEBHOOK_BYTES=1048576
RECONCILIATION_CRON=0 4 * * *
```

All payment secrets required at startup in production, never logged. The Phase 0 `slog`
redaction config gains `key_secret`, `webhook_secret`, `signature`.

---

## 10. Task list

| # | Task | Done when |
|---|---|---|
| 7.1 | Money migration; every amount `BIGINT`; append-only ledger rules | A CI check fails on any float money column |
| 7.2 | `Paise int64` domain type + ledger service | Debits == credits invariant test green |
| 7.3 | Idempotency middleware | Replaying a key returns the stored response, creates nothing |
| 7.4 | Wallet with `FOR UPDATE` inside a `pgx` transaction | Concurrent-spend test cannot overdraw |
| 7.5 | `PaymentProvider` interface + Razorpay adapter | Test-mode order → checkout → verify works |
| 7.6 | Stripe adapter | Same interface |
| 7.7 | Webhook endpoint: raw-body HMAC, persist-first, idempotency, `asynq` | Duplicate delivery is a no-op |
| 7.8 | Subscription lifecycle incl. proration and `past_due` grace | All transitions tested |
| 7.9 | Credit grants, FIFO consumption, expiry worker | Soonest-expiring grant consumed first |
| 7.10 | Quota check before generation; deduct after success | Failed generation charges nothing |
| 7.11 | `usage_records` aggregation from `ai_request_logs` | Revenue vs real cost visible per user |
| 7.12 | Pricing, checkout, billing portal, credits, usage screens | |
| 7.13 | Paywall component with reset countdown | |
| 7.14 | GST invoice generation (`chromedp` worker) | Invoice is GST-compliant |
| 7.15 | Refund flow with reversing ledger entries | Balance returns exactly; no prior row mutated |
| 7.16 | Nightly reconciliation vs provider | Discrepancies logged, not auto-corrected |
| 7.17 | Admin finance view: MRR, churn, failed payments, discrepancies | |
| 7.18 | Dunning for failed renewals (3 retries + email) | |

---

## 11. Testing

**Unit** — `Paise` arithmetic; proration; GST rounding (round once, at the invoice, in
paise); credit FIFO ordering; quota evaluation; ledger balancing.

**Concurrency — run these for real, under `-race`, with genuinely parallel requests:**
- Two simultaneous credit spends with balance for one → exactly one succeeds
- Two simultaneous subscription purchases → exactly one subscription
- Duplicate webhook delivered twice concurrently → processed once
- Concurrent refund and spend → ledger still balances

Go's `-race` and `errgroup` make these tests easy to write and they are the single
highest-value tests in the phase. Reasoning about race conditions is not testing for
them.

**Integration (testcontainers)** — full order → checkout → verify → webhook → credits
granted; webhook arriving before the client callback; webhook for an unknown order;
malformed signature rejected; refund reverses correctly; `past_due` → grace → recovery;
`past_due` → expiry → downgrade.

**Idempotency** — every money endpoint called twice with the same key produces one
effect and two identical responses.

**E2E** — `hit free limit → paywall → buy credits (test UPI) → continue chatting →
credits decrement correctly → view invoice`, and `subscribe → use Pro features → cancel
→ access persists to period end → downgrades cleanly`.

**Reconciliation** — seed a deliberate mismatch; assert it is detected and reported and
that nothing is auto-corrected.

---

## 12. Security checklist

- [ ] No card data stored. Provider-hosted checkout only.
- [ ] Webhook signatures verified with `hmac.Equal` over the **raw body**; unsigned rejected; no debug bypass
- [ ] Webhook body size-limited (`io.LimitReader`)
- [ ] All payment secrets from env; required in prod; added to log redaction
- [ ] Idempotency on every money-moving endpoint, enforced by middleware
- [ ] `SELECT … FOR UPDATE` on all balance mutations, inside a transaction
- [ ] All amounts `int64`/`BIGINT` paise; CI check prevents float money columns
- [ ] Ledger append-only, enforced by Postgres rules — no `UPDATE` path exists
- [ ] Rate limiting on payment endpoints (carding attempts are real)
- [ ] Amounts recomputed server-side from the plan — **never trusted from the client**
- [ ] Users can only see their own transactions, invoices and ledger
- [ ] Refunds `ADMIN`-only, reason-required, audit-logged
- [ ] Invoices exclude unnecessary PII; access by signed URL
- [ ] Failure reasons generic to the client; detail server-side
- [ ] Reconciliation discrepancies alert a human; nothing auto-corrects
- [ ] Account deletion retains financial records as legally required — **document the retention basis and exclude them from the hard-delete cascade deliberately, not accidentally**

That last one is a genuine conflict between "delete everything" (Phase 1) and tax and
accounting retention obligations. Resolve it explicitly in an ADR, implement it as a
documented exception with personal identifiers stripped from retained records, and state
it in the privacy policy. Discovering the conflict during an audit is much worse than
deciding it now.

---

## 13. Analytics

```
pricing_page_viewed       { source }
paywall_shown             { trigger, messages_used }
plan_selected             { plan_id }
checkout_started          { plan_id, amount_bucket }
payment_method_selected   { method }
payment_succeeded         { plan_id, amount_bucket }
payment_failed            { reason_code }
subscription_started      { plan_id }
subscription_cancelled    { plan_id, days_active }
credits_purchased         { pack, amount_bucket }
credits_exhausted         { user_id }
quota_exceeded            { job_type }
invoice_downloaded        {}
```

Amounts bucketed, never raw. No payment identifiers in analytics.

---

## 14. Risks

| Risk | Mitigation |
|---|---|
| Double charging | Idempotency keys everywhere + webhook dedup + real concurrency tests |
| Lost payments (webhook missed) | Persist-first, `asynq` retry, nightly reconciliation |
| Float rounding in money | `Paise int64` + `BIGINT` enforced by a CI schema check |
| Race conditions on balance | `FOR UPDATE` + genuine concurrent tests under `-race` |
| Client-tampered amounts | Server always recomputes from the plan record |
| Signature verification on re-encoded JSON | Verify over the raw body, before decoding; tested |
| Deletion vs financial retention conflict | Resolved in an ADR before launch, documented in the privacy policy |
| GST compliance | Get an accountant to review one real invoice before launch. Not a place to self-certify. |
| Paywall kills growth | Kundli, chart and daily stay free forever; measure `paywall_shown` → `payment_succeeded` and the retention of users who hit the wall and didn't pay |

---

## 15. Definition of Done

Global DoD **plus**:

- [ ] All amounts integer paise, end to end
- [ ] Ledger double-entry, append-only, balancing invariant CI-tested
- [ ] Idempotency proven on every money endpoint
- [ ] Concurrency tests prove no double-spend and no duplicate subscription
- [ ] Webhooks verified, deduplicated, replay-safe, order-independent
- [ ] Failed generations never consume credits
- [ ] Reconciliation runs nightly and reports discrepancies without auto-correcting
- [ ] GST invoices reviewed by an accountant
- [ ] Cancellation is two taps and honours the paid period

---

## 16. Phase Gate 🔒

- [ ] Subscription purchase works end to end in test mode
- [ ] Credit purchase and FIFO consumption work end to end
- [ ] Webhook is authoritative; duplicate and out-of-order deliveries handled
- [ ] **Concurrency tests green under `-race`: no double-spend, no duplicate subscription, no duplicate webhook processing**
- [ ] **Idempotency verified on every money-moving endpoint**
- [ ] Ledger balances; debits == credits invariant green in CI
- [ ] Append-only enforced by Postgres rules (an `UPDATE` attempt changes nothing)
- [ ] Every money value is `int64`/`BIGINT` paise; CI check prevents regressions
- [ ] Webhook HMAC verified over the raw body; malformed signature rejected
- [ ] Quota enforced before generation; credits deducted only after success
- [ ] Failed and declined payments handled gracefully with clear user messaging
- [ ] Refund reverses via new ledger entries; no prior row mutated
- [ ] Subscription upgrade, downgrade, cancel and expiry correct incl. proration
- [ ] Dunning retries failed renewals and notifies the user
- [ ] GST invoices generated and accountant-reviewed
- [ ] Nightly reconciliation implemented; seeded mismatch is detected
- [ ] Admin finance view shows MRR, churn, failures, discrepancies
- [ ] Payment secrets redacted from all logs
- [ ] Financial-retention exception documented in an ADR and the privacy policy
- [ ] `task verify` and `task eval` green
- [ ] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
