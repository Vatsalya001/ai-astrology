# Phase 10 — Mobile App (React Native / Expo)

| | |
|---|---|
| **Goal** | Ship iOS and Android apps on the same Go API, with push notifications as the retention engine. |
| **Deliverable** | An Expo app covering Kundli, chat, daily astrology, compatibility, voice and consultations, published to both stores. |
| **Depends on** | Phase 6 (Phase 7 for billing, 8 for consultations, 9 for voice — each gated by its flag) |
| **Estimated size** | 15–25 days plus store review time |
| **Cost to run** | ₹0 to build. **Store fees are the exception:** Apple Developer $99/year, Google Play $25 one-time. |

> Mobile is where this product category actually lives. Astrology is a daily,
> push-driven, phone-in-hand habit — web is for acquisition and sharing, the app is for
> retention. But build it *after* the product is proven: shipping a half-formed product
> to an app store earns you one-star reviews that outlive the bug.

A TypeScript phase. The backend is untouched — the app consumes the same Go API the web
app does, which is the payoff for having a single public API surface.

---

## 1. Scope

### In scope
- Expo (managed workflow) app for iOS and Android
- All core features: onboarding, Kundli, chat, daily, compatibility, memory, settings
- Push notifications with deep links
- Biometric app lock
- Offline reading of cached charts and daily predictions
- Native share sheet
- In-app purchase decision and implementation (see §4 — it needs an ADR)
- Voice (Phase 9) and consultations (Phase 8) behind flags
- OTA updates via EAS Update
- Store listings, screenshots, privacy declarations, submission

### Out of scope
- A separate astrologer mobile app (astrologers use responsive web from Phase 8; revisit once supply volume justifies it)
- Tablet-optimised layouts
- Wear OS / watchOS

---

## 2. Code sharing strategy

```
packages/
├── types/                  ✅ shared as-is
├── api-client/             ✅ shared — typed client for the Go API
├── astrology-geometry/     ✅ shared as-is — pure TS, no React (Phase 3 built it this way for exactly this)
├── analytics/              ✅ shared, platform adapters
├── content/                ✅ shared (glossary, yoga copy)
└── ui/                     ⚠  web only — RN needs its own primitives
```

**Don't force a universal UI layer.** `react-native-web` and similar "write once" setups
promise more than they deliver: you end up with components that are mediocre on both
platforms and a build system nobody understands. Share **logic, types and API access**;
write platform-native UI.

### The chart on mobile

`react-native-svg` has an API close enough to web SVG that the Phase 3 geometry module
ports directly — which is precisely why Phase 3 put it in a React-free package. Only the
rendering layer is platform-specific. That keeps the two charts provably identical, and
the same 30 golden fixtures test both.

A user who sees a different chart on phone than on web loses trust in both.

### Stack

| Concern | Choice |
|---|---|
| Framework | Expo SDK (managed), TypeScript |
| Navigation | Expo Router — file-based, mirrors Next.js routing |
| Styling | NativeWind (Tailwind for RN) — shares the design tokens |
| Data | TanStack Query + MMKV persistence |
| Storage | `expo-secure-store` for tokens, MMKV for cache |
| Push | `expo-notifications` + FCM/APNs |
| SVG | `react-native-svg` |
| Audio | `expo-av` + audio recording (Phase 9) |
| Real-time | `socket.io-client` or a raw WS client matching the Go hub (Phase 8) |
| Builds | EAS Build |
| OTA | EAS Update |

---

## 3. Push notifications — the retention engine

This is the reason the app exists. Daily horoscope push converts a one-time Kundli
viewer into a returning user.

| Notification | Timing | Deep link |
|---|---|---|
| Daily horoscope ready | 08:00 local | `/daily` |
| Weekly digest | Sunday 09:00 | `/reports/weekly` |
| Dasha period changing | 7 days before | `/kundli/dashas` |
| Major transit | On occurrence | `/kundli/transits` |
| Sade Sati phase change | On occurrence | `/kundli/transits` |
| AI replied (backgrounded) | Immediate | `/chat/{id}` |
| Astrologer accepted | Immediate | `/consultation/{id}` |
| Low balance during consultation | Immediate | `/settings/credits` |

### Rules

- **Content in the payload, not just a ping.** "Moon enters your 10th house today — a good day for difficult conversations" gets opened. "You have a new horoscope" does not.
- **But no sensitive content.** Notifications render on a locked screen where anyone can read them. Never put consultation content, chat content, health or relationship specifics in a notification. Title and deep link only for anything personal.
- Respect quiet hours (Phase 6) — enforced in Go, before dispatch.
- Cap at 2/day. This category is notorious for notification spam; users punish it with uninstalls.
- Granular per-type opt-out in settings.
- Deep links must work cold-start, backgrounded and from web.

---

## 4. ⚠️ In-app purchase — an ADR you must write

Apple and Google require their in-app purchase systems for digital content sold inside
an app, and take a commission (commonly 30%, reduced to 15% for smaller developers and
for subscriptions after year one). The exact rules, rates and available exceptions vary
by platform, jurisdiction and year — and India has had active regulatory litigation on
exactly this point.

This is a **material business decision**, not a technical one:

| Option | Trade-off |
|---|---|
| Native IAP (`expo-in-app-purchases` / RevenueCat) | Best conversion, best store compliance, ~15–30% commission |
| Web checkout only, app read-only for purchases | Keeps the full margin; platform rules restrict how you may reference it in-app |
| Hybrid — IAP in app, web for the portal | Common; needs careful entitlement syncing across both |

`ADR-0NN-in-app-purchases.md` must be written **before** any store submission and
reviewed against the current versions of both platforms' guidelines — not against what
was true last year. Get it wrong and the app is rejected, or worse, removed after launch.

If you go native IAP, RevenueCat is worth the cost: cross-platform entitlement syncing,
receipt validation and subscription state reconciliation are genuinely fiddly, and this
has to reconcile with the Phase 7 ledger in Go.

**Entitlements are server-side.** Whatever the purchase rail, the Go API is the source of
truth for what a user is entitled to. Receipt validation happens in Go against Apple's
and Google's verification endpoints, and a successful validation writes ledger entries
exactly like a Razorpay webhook does. A client-side entitlement check is trivially
bypassed.

---

## 5. Offline

Astrology data is unusually offline-friendly: a chart doesn't change.

| Data | Offline behaviour |
|---|---|
| Birth chart | Cached indefinitely, fully readable offline |
| Dashas | Cached indefinitely |
| Today's + last 7 days' predictions | Cached |
| Conversation history | Last 50 messages per conversation |
| Knowledge/glossary | Bundled with the app |
| New AI messages | ❌ requires connection — queue with a clear indicator |
| Consultations | ❌ requires connection |

Offline mode shows a persistent, non-alarming banner and disables only what genuinely
needs the network. A user on the Mumbai metro should still be able to read their chart.

---

## 6. Mobile-specific UX

**Biometric app lock** (`expo-local-authentication`), opt-in. Given what people discuss
here — fertility, illness, marital trouble, money — a shared phone is a real privacy
risk. Small feature, outsized trust value.

**Haptics** on meaningful moments: chart reveal, response complete, consultation
connect. Restrained, not constant.

**Native share** for the chart image and PDF — the primary organic growth loop in this
category. One tap from the Kundli screen.

**Widgets** (worth it if time allows): a home-screen daily horoscope widget is sticky
retention with no notification-fatigue cost.

**Permissions requested in context, never at launch.** Ask for notifications after the
first Kundli is generated, with an explanation of what they'll get — not on the splash
screen. The difference in opt-in rate is large.

---

## 7. Store submission

### Requirements checklist

- [ ] Privacy policy and terms URLs, live and accurate
- [ ] Apple privacy nutrition labels — declare **every** data type collected, honestly
- [ ] Google Play Data Safety form — same
- [ ] Account deletion reachable **in-app** (both stores require this; Phase 1 built it)
- [ ] Age rating; astrology content is generally fine, but consultations are 18+
- [ ] Screenshots for all required device sizes
- [ ] App preview video (optional, meaningfully improves conversion)
- [ ] Description, keywords, localised for Hindi
- [ ] Test account credentials for reviewers — **with a pre-populated chart and credits**, or they cannot evaluate the app
- [ ] Demo video if any feature is hard to reach in review

### Rejection risks specific to this app

| Risk | Mitigation |
|---|---|
| Purchases bypassing IAP | Resolve via the ADR in §4 before submitting |
| "Objectionable content" / fortune-telling claims | Frame consistently as entertainment and guidance. The disclaimer must be visible in-app, not buried in the terms. |
| Incomplete privacy declarations | Audit what you actually send to third parties — analytics, Sentry, the LLM provider — and declare all of it |
| Account deletion not findable | It's in `/settings/delete`, two taps from the profile tab |
| Reviewer can't test the core feature | Provide a test account with a chart and credits already populated |

Budget 1–2 weeks for review iterations on the first submission. Plan for at least one
rejection; it is normal.

---

## 8. Environment variables added

```bash
EXPO_PUBLIC_API_URL=http://localhost:4000      # the Go API — the only backend the app knows
EXPO_PUBLIC_WS_URL=ws://localhost:4000
EXPO_PUBLIC_ENV=development

EAS_PROJECT_ID=
APPLE_TEAM_ID=
GOOGLE_SERVICES_JSON=
FCM_SENDER_ID=

IAP_PROVIDER=                        # revenuecat | native | none — set by the ADR
REVENUECAT_API_KEY_IOS=
REVENUECAT_API_KEY_ANDROID=

FEATURE_BIOMETRIC_LOCK=true
FEATURE_OFFLINE_MODE=true
FEATURE_WIDGETS=false
```

The app talks only to the Go API. It has no knowledge that `astro-service` or
`ai-service` exist — which is exactly the property that makes the polyglot backend
invisible to clients.

---

## 9. Task list

| # | Task | Done when |
|---|---|---|
| 10.1 | Expo scaffold, Expo Router, NativeWind with shared tokens | Runs on both simulators |
| 10.2 | `packages/api-client` consumed by both web and mobile | One typed client, two platforms |
| 10.3 | Auth: OTP, `expo-secure-store` token storage, refresh rotation | |
| 10.3a | **Google OAuth** — deferred here from Phase 1 by owner decision | Round trip creates or links an identity, on web and device |
| 10.3b | **Apple OAuth** — needs the $99/yr developer account | Same. Effectively mandatory: Apple requires Sign in with Apple wherever a third-party social login is offered |
| 10.4 | Onboarding: birth details, place search, computing screen | |
| 10.5 | `ChartSVG` in `react-native-svg` using the shared geometry module | Matches web for all 30 fixtures |
| 10.6 | Kundli screens: planets, houses, dashas, yogas, transits | |
| 10.7 | Chat with SSE streaming on RN | Streaming works on a real device over cellular |
| 10.8 | Daily astrology + widget (if in scope) | |
| 10.9 | Compatibility | |
| 10.10 | Memory settings | |
| 10.11 | Push: registration, handling, deep links (cold/warm/background) | All three entry paths tested on device |
| 10.12 | **IAP per the ADR**; Go-side receipt validation + ledger entries | Purchases reconcile server-side |
| 10.13 | Offline caching with MMKV + TanStack persistence | Chart readable in airplane mode |
| 10.14 | Biometric lock | |
| 10.15 | Native share for chart image and PDF | |
| 10.16 | Voice (Phase 9) behind a flag | |
| 10.17 | Consultations (Phase 8) behind a flag | |
| 10.18 | EAS Build + EAS Update pipelines | |
| 10.19 | Store listings, assets, privacy declarations, submission | Approved on both stores |

---

## 10. Testing

**Device matrix** — do not test only on simulators. Minimum: a low-end Android (2–3 GB
RAM, the realistic majority device in India), a mid-range Android, an older iPhone, and
a current iPhone. Performance problems only appear on the low-end device, and that's
most of your users.

**Chart parity** — render all 30 golden fixtures on mobile and compare against the web
screenshots. The shared geometry module should make this pass trivially; if it doesn't,
something diverged and you want to know immediately.

**Network conditions** — 3G, flaky connection, airplane mode, and mid-request network
loss. Streaming chat over a degrading connection is the case that breaks.

**Push** — cold start, background and foreground, for every deep link, on both
platforms. Deep links are a classic source of "works in dev, broken in production."

**IAP** — sandbox purchase, restore purchases, subscription renewal, cancellation,
refund, and cross-platform entitlement (buy on iOS, use on web). Assert the Go ledger
balances after each.

**Offline** — chart readable offline; queued messages send on reconnect; no data loss on
app kill.

**E2E** — Maestro or Detox for the critical flows: signup → birth details → Kundli →
chat → purchase.

**Accessibility** — VoiceOver and TalkBack passes on every core screen; dynamic type;
sufficient contrast.

---

## 11. Security checklist

- [ ] Tokens in `expo-secure-store` (Keychain / Keystore), never `AsyncStorage`
- [ ] Certificate pinning on the API (worth it given the data sensitivity)
- [ ] Biometric lock available and functional
- [ ] No PII in push notification bodies
- [ ] No sensitive data in MMKV without encryption
- [ ] Deep links validated — never trust link parameters to grant access
- [ ] **Entitlements verified in Go**, never trusted from the client
- [ ] **IAP receipts validated server-side** against Apple/Google, writing ledger entries
- [ ] Screenshot protection on consultation and chat screens (Android `FLAG_SECURE`)
- [ ] Debug logging stripped from release builds
- [ ] Privacy declarations match what the app actually does
- [ ] Account deletion reachable in-app in two taps
- [ ] **OAuth `state` validated (CSRF on the OAuth flow)** — carried from Phase 1 §11 with
      the feature. Without it the callback accepts a code from anywhere, which silently
      signs a victim into the attacker's account. PR 8c's implementation and its tests
      are recoverable by reverting PR 9a; do not rewrite them from scratch.

---

## 12. Risks

| Risk | Mitigation |
|---|---|
| **App store rejection over IAP** | Resolve the ADR against current guidelines before submitting |
| Store commission destroys unit economics | Model 15–30% into pricing *before* launch, not after |
| Low-end Android performance | Test on a real 2 GB device from day one; budget bundle size |
| Push permission denial kills retention | Ask in context after the first Kundli, with a clear benefit statement |
| Divergence between web and mobile charts | Shared pure geometry module; identical fixtures tested on both |
| Maintaining two UIs | Accepted cost. Share logic, not pixels — trying to share pixels costs more. |
| Review delays block launch | Submit early to TestFlight / internal testing; budget two weeks |

---

## 13. Definition of Done

Global DoD **plus**:

- [ ] Both apps published and approved
- [ ] Push notifications deliver and deep-link correctly from all three app states
- [ ] Charts render identically to web across all fixtures
- [ ] Offline chart reading works
- [ ] IAP (or the chosen alternative) works and reconciles with the Go ledger
- [ ] Usable on a 2 GB Android device
- [ ] VoiceOver and TalkBack passes complete
- [ ] Privacy declarations accurate on both stores

---

## 14. Phase Gate 🔒

- [ ] Expo app builds and runs on iOS and Android via EAS
- [ ] Full auth and onboarding flow works on device
- [ ] **Google OAuth completes and links an identity** — the Phase 1 gate item, moved
      here. Requires real credentials: a Google Cloud OAuth client with
      `openid email` scope only, and an authorised redirect URI matching the
      deployment byte for byte
- [ ] **Sign in with Apple works** — Apple rejects apps offering Google sign-in without it
- [ ] **Chart renders in both styles, matching web for all 30 fixtures**
- [ ] All Kundli screens complete
- [ ] Streaming chat works on a real device over cellular
- [ ] Daily astrology and compatibility working
- [ ] Push registered, delivered, deep-linked from cold/background/foreground
- [ ] No PII in any notification payload
- [ ] **IAP ADR written and implemented; receipts validated in Go; ledger balances**
- [ ] Offline chart reading verified in airplane mode
- [ ] Biometric lock working
- [ ] Native share working for chart and PDF
- [ ] Tested on a low-end Android, mid-range Android and two iPhones
- [ ] Tested on 3G and flaky networks
- [ ] VoiceOver and TalkBack passes on all core screens
- [ ] Account deletion reachable in-app
- [ ] Privacy labels and Data Safety forms accurate
- [ ] **Approved on both App Store and Play Store**
- [ ] `task verify` and the mobile E2E suite green
- [ ] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
