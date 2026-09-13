# Phase 3 — Kundli UI & Chart Visualization

| | |
|---|---|
| **Goal** | Make the chart *visible*. A user sees their Kundli and understands it without knowing astrology. |
| **Deliverable** | A complete Kundli dashboard: birth chart in North/South Indian style, planetary table, houses, dasha timeline, yogas, current transits, and a downloadable PDF. |
| **Depends on** | Phase 2 |
| **Unlocks** | Phase 4 |
| **Estimated size** | 8–12 days |
| **Cost to run** | ₹0 — frontend plus a Go PDF worker |

> **Still zero AI in this phase.** Everything on screen renders the deterministic chart
> from Phase 2. That matters: it proves the product has standalone value before a single
> token is spent, and it gives Phase 5's "Why am I seeing this?" real UI to link into.

Mostly a TypeScript phase. Go contributes the PDF worker; `astro-service` is untouched.

---

## 1. Scope

### In scope
- North Indian and South Indian chart rendering (SVG, accessible, responsive)
- Planetary positions table with dignity, retrograde, combustion, nakshatra
- House-by-house view with lords and occupants
- Dasha timeline — Maha → Antar → Pratyantar, with a "you are here" marker
- Yoga cards with plain-language descriptions (static copy, not AI)
- Current transits panel, including Sade Sati status
- Divisional chart switcher (D1 / D9 / D10)
- PDF Kundli generation (Go worker) and download
- Share (image / link)
- Glossary — tap any astrology term for a definition
- Home dashboard shell

### Out of scope
- AI chat (Phase 5) — but the **entry points** are placed now, behind a feature flag
- Compatibility (Phase 6)
- Anything paid (Phase 7)

---

## 2. The two chart styles

Not cosmetic. North Indian users expect a diamond; South Indian users expect a grid.
Showing the wrong one makes the product feel foreign. Ship both; default from
`user_preferences.chart_style`, inferred from the birth state on first render.

### North Indian (diamond) — houses fixed, signs rotate

House 1 is always the top-centre diamond. The ascendant's sign number is written in it,
and signs proceed anticlockwise.

```
        ┌───────────────────────────┐
        │  ╲     12    │    2     ╱  │
        │    ╲         │        ╱    │
        │  11  ╲───────┼──────╱  3   │
        │       ╲  1   │  ...╱       │
        │  ────── ╲    │   ╱ ──────  │
        │  10      ╲   │  ╱      4   │
        │            ╲ │╱            │
        │  9     ╱     │     ╲    5  │
        │      ╱    7  │  6    ╲     │
        │    ╱         │        ╲    │
        │  8                        │
        └───────────────────────────┘
```

### South Indian (grid) — signs fixed, houses rotate

A 4×4 grid with a hollow centre. Signs are always in the same cells (Pisces top-left,
clockwise). The ascendant cell is marked, conventionally with a diagonal line.

```
┌────────┬────────┬────────┬────────┐
│ Pisces │ Aries  │ Taurus │ Gemini │
│        │  ╲ASC  │        │  Su Me │
├────────┼────────┴────────┼────────┤
│Aquarius│                 │ Cancer │
│   Sa   │                 │        │
├────────┤                 ├────────┤
│Capricorn│                │  Leo   │
│        │                 │  Ju    │
├────────┼────────┬────────┼────────┤
│ Sagitt.│Scorpio │ Libra  │ Virgo  │
│  Ma    │        │ Mo Ve  │  Ra    │
└────────┴────────┴────────┴────────┘
```

### Rendering approach

**SVG, built from the chart JSON. Not an image, not canvas.**

- Crisp at every size and DPI, and it prints correctly in the PDF
- Every glyph is a real DOM node → tappable, focusable, screen-reader labelled
- Small enough to server-render for fast first paint
- Themeable with CSS variables, so dark/light comes free

```tsx
<ChartSVG
  chart={chart}
  style="north" | "south"
  highlight={['Saturn', 'house:10']}   // Phase 5 uses this for explainability
  onPlanetTap={(p) => openPlanetSheet(p)}
  onHouseTap={(h) => openHouseSheet(h)}
/>
```

`highlight` is the hook that makes Phase 5's *"Why am I seeing this?"* work: tapping the
explanation highlights exactly those planets and houses. Build the prop now even though
nothing uses it yet — retrofitting it later means touching every render path.

### Chart geometry as a pure module

Put the geometry (house index → SVG polygon points, for both styles) in
`packages/astrology-geometry/` as pure TypeScript functions with no React. Two reasons:
it is unit-testable without rendering, and Phase 10 reuses it verbatim under
`react-native-svg` so the mobile chart is provably identical to the web chart.

### Accessibility (genuinely required, not a checkbox)

An SVG chart is meaningless to a screen reader unless you make it meaningful:

- `role="img"` on the chart with a full `aria-label` summarising it
- Each planet glyph is a `<g role="button" tabindex="0">` with
  `aria-label="Saturn in Aquarius, 10th house, 12 degrees, retrograde"`
- A visually-hidden `<table>` duplicating the chart data — screen readers get a table, sighted users get the diagram
- Never encode meaning in colour alone; retrograde also gets an `℞` glyph, combustion also gets a ring
- Minimum 4.5:1 contrast for all glyphs
- Honour `prefers-reduced-motion` on the reveal animation

---

## 3. Screens

| Screen | Route | Purpose |
|---|---|---|
| Home | `/home` | Daily snapshot + entry points |
| Kundli dashboard | `/kundli` | The main event |
| Chart detail | `/kundli/chart` | Full-screen chart, style switcher, D1/D9/D10 |
| Planets | `/kundli/planets` | Full table + per-planet detail sheets |
| Houses | `/kundli/houses` | House-by-house |
| Dashas | `/kundli/dashas` | Timeline + drill-down |
| Yogas | `/kundli/yogas` | Detected yogas with explanations |
| Transits | `/kundli/transits` | Current sky vs natal chart, Sade Sati |
| Profiles | `/kundli/profiles` | Switch between self / partner / child charts |

### Home dashboard

```
┌──────────────────────────────────────────┐
│  Good morning, Vatsalya  ✦               │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  TODAY                             │  │
│  │  Moon in Rohini · Taurus           │  │
│  │  A steady day for practical work.  │  │
│  │                        [ More → ]  │  │
│  └────────────────────────────────────┘  │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  Ask your AI Astrologer            │  │
│  │  ┌──────────────────────────────┐  │  │
│  │  │ What's on your mind?         │  │  │
│  │  └──────────────────────────────┘  │  │
│  │  [Career] [Love] [Money] [Marriage]│  │
│  └────────────────────────────────────┘  │
│                                          │
│  YOUR CURRENT PERIOD                     │
│  ┌────────────────────────────────────┐  │
│  │  Jupiter Mahadasha                 │  │
│  │  Mar 2019 ──────●──────── Mar 2035 │  │
│  │  Antardasha: Saturn (to Nov 2026)  │  │
│  └────────────────────────────────────┘  │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  ⚠ Sade Sati — rising phase        │  │
│  │  Started Jan 2026 · ends Apr 2033  │  │
│  └────────────────────────────────────┘  │
│                                          │
│  [ View my full Kundli → ]               │
│  [ Talk to an astrologer ]  (flagged)    │
└──────────────────────────────────────────┘
```

In Phase 3 the "Today" card shows **deterministic** content (Moon sign, nakshatra,
tithi, a static rule-based line). It becomes AI-generated in Phase 6. The AI chat box is
present but disabled behind `FEATURE_AI_CHAT_ENABLED` — placing it now means Phase 5
ships a working feature rather than a working feature *plus* a navigation redesign.

### Kundli dashboard

```
┌──────────────────────────────────────────┐
│  My Kundli            [ Self ▾ ] [⤓ PDF] │
│                                          │
│  ┌──────────┬──────────┬──────────┐      │
│  │ Ascendant│ Moon sign│ Sun sign │      │
│  │  Aries   │  Taurus  │   Leo    │      │
│  └──────────┴──────────┴──────────┘      │
│  Nakshatra: Rohini, pada 2               │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │                                    │  │
│  │        [ CHART SVG ]               │  │
│  │                                    │  │
│  │   [North ▾]  [D1 ▾]   [ Expand ]   │  │
│  └────────────────────────────────────┘  │
│                                          │
│  ▸ Planetary positions           (9)     │
│  ▸ Houses                       (12)     │
│  ▸ Dasha periods                         │
│  ▸ Yogas                         (4)     │
│  ▸ Current transits                      │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  💬  Ask about your Kundli         │  │
│  └────────────────────────────────────┘  │
└──────────────────────────────────────────┘
```

### Planetary table

Dense on desktop, card-stacked on mobile. Never a horizontally-scrolling table on a
phone — it reads as broken.

```
Planet   Sign        Deg      House  Nakshatra        Status
─────────────────────────────────────────────────────────────
☉ Sun    Leo        15°14'      5    P. Phalguni 1    Own sign
☽ Moon   Taurus      8°42'      2    Rohini 2         Exalted
♂ Mars   Sagittarius 22°03'     9    P. Ashadha 3     —
☿ Merc   Leo         2°55'      5    Magha 1          Combust ⊙
♃ Jup    Leo        28°30'      5    U. Phalguni 1    —
♀ Venus  Libra      11°18'      7    Swati 1          Own sign
♄ Sat    Aquarius   19°47'     11    Shatabhisha 3    Own sign ℞
☊ Rahu   Virgo      14°02'      6    Hasta 2          ℞
☋ Ketu   Pisces     14°02'     12    U. Bhadrapada 4  ℞
```

Every cell with astrological meaning is tappable → a bottom sheet with a plain-English
explanation. "Combust" means nothing to a new user; a one-tap definition is the
difference between a chart that impresses and a chart that intimidates.

### Dasha timeline

The single most compelling non-AI screen in the product. People care intensely about
"what period am I in."

```
┌──────────────────────────────────────────────────────┐
│  Dasha periods                                       │
│                                                      │
│  Ketu  Venus   Sun   Moon  Mars  Rahu  JUPITER  Sat  │
│  ├──┤  ├────┤  ├──┤  ├───┤ ├──┤  ├───┤ ├══●══┤  ├──┤ │
│  1994  2001    2021  2027  2037  2044  2019     2035 │
│                                    ▲ you are here    │
│                                                      │
│  ── Jupiter Mahadasha ──────────── Mar 2019–Mar 2035 │
│     ▸ Jupiter / Jupiter    Mar 2019 – May 2021       │
│     ▸ Jupiter / Saturn     May 2021 – Nov 2026  ← now│
│         ▸ Jup/Sat/Mercury  Feb 2026 – Aug 2026  ← now│
│     ▸ Jupiter / Mercury    Nov 2026 – Feb 2029       │
└──────────────────────────────────────────────────────┘
```

Zoomable (decade ↔ year ↔ month), current period pinned. Tapping a period opens a
detail sheet — and, from Phase 5, an "Ask about this period" button that pre-fills chat.

### Transits panel

```
┌──────────────────────────────────────────┐
│  Right now in the sky                    │
│                                          │
│  ♄ Saturn    Pisces    → your 12th house │
│  ♃ Jupiter   Gemini    → your 3rd house  │
│  ☊ Rahu      Aquarius  → your 11th house │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  Sade Sati — rising phase          │  │
│  │  ●───────○───────○                 │  │
│  │  Rising  Peak    Setting           │  │
│  │  Jan 2026 ────────────── Apr 2033  │  │
│  │  [ What does this mean? ]          │  │
│  └────────────────────────────────────┘  │
└──────────────────────────────────────────┘
```

---

## 4. PDF Kundli — a Go worker

A genuinely valuable artifact — people print these, share them with family, and send
them to astrologers. It is also the most effective organic-sharing surface the product
has.

**Generated by an `asynq` worker in Go, never in the request path.**

```
POST /api/v1/charts/{id}/pdf        → 202 { job_id }
GET  /api/v1/charts/{id}/pdf/{jobID} → { status, url? }
```

```
asynq task → chromedp drives headless Chrome
          → renders a print-styled Next.js route with a signed one-time token
          → PDF bytes → MinIO/S3 → signed URL (24 h)
          → notification when ready
```

`chromedp` is the Go CDP driver — free, and it keeps the job in the same worker process
as every other background task, so there is one queue, one retry policy and one
dashboard rather than a separate Python or Node service just for PDFs.

Reusing a print-styled web route means the PDF and the site can never drift apart — a
real maintenance win over a separate PDF templating system.

Contents: cover page with name and birth details · D1 and D9 charts · planetary table ·
house table · Vimshottari dasha table · detected yogas · current transits · glossary ·
a clear "for guidance purposes" disclaimer on every page footer.

**Signed URLs, short expiry, no PII in the filename.** `kundli-{uuid}.pdf`, never
`kundli-vatsalya-1994.pdf`.

The print route authenticates with a single-use token minted by the worker, so headless
Chrome can render a private page without holding a user session.

---

## 5. Component inventory (`packages/ui`)

| Component | Notes |
|---|---|
| `ChartSVG` | North/South, D1/D9/D10, highlight prop, full a11y |
| `PlanetGlyph` | Unicode + custom SVG fallback, retrograde/combust markers |
| `PlanetTable` | Responsive table → cards below `md` |
| `HouseList` | Lord, occupants, aspects |
| `DashaTimeline` | Zoomable, current-period marker, drill-down |
| `DashaCard` | Compact "current period" card for home |
| `YogaCard` | Name, strength, involved planets, plain-language description |
| `TransitPanel` | Current positions vs natal |
| `SadeSatiIndicator` | Three-phase progress |
| `AstroTerm` | Inline glossary term → bottom sheet |
| `ChartStyleSwitcher` | North / South / East |
| `ProfileSwitcher` | Self / partner / child |
| `ShareSheet` | Image, link, PDF |
| `SkeletonChart` | Loading state that looks like a chart, not a grey box |

Every component gets a Storybook story with loading, error, empty and RTL variants.
Storybook is free and it is what keeps Phases 5 and 10 from re-inventing all of this.

---

## 6. Content: the static interpretation layer

Yoga descriptions, planet meanings, house meanings and the glossary are **written
content**, not AI output. They live in `packages/content` as typed JSON with locales:

```jsonc
{
  "yoga.gajakesari": {
    "en": {
      "name": "Gajakesari Yoga",
      "short": "Jupiter and the Moon in mutual angles",
      "long": "This combination is traditionally associated with good judgement, respect from others, and steady growth in reputation…"
    },
    "hi": { "...": "..." }
  }
}
```

Two reasons to do this properly rather than punt to the LLM later:

1. It is **free and instant** — no token cost, no latency, no hallucination risk on
   content that never changes.
2. In Phase 5 this same corpus becomes the seed of the RAG knowledge base. Writing it as
   structured, sourced content now saves rebuilding it later.

Traditional framing throughout ("is traditionally associated with"), never predictive
framing ("you will"). The safety posture starts in the static copy, not at the LLM.

---

## 7. Performance

| Metric | Target |
|---|---|
| Kundli dashboard LCP | < 2.0 s on a mid-range Android over 4G |
| Chart SVG render | < 100 ms |
| Route transition | < 200 ms |
| PDF generation | < 15 s p95 (async — user isn't blocked) |
| JS bundle (kundli route) | < 180 KB gzipped |

Server-render the chart SVG (Go already has the data), stream the rest, lazy-load the
dasha timeline, use `next/font` to avoid layout shift, and cache the chart response
with `stale-while-revalidate`.

---

## 8. Environment variables added

```bash
# api-service (Go)
FEATURE_AI_CHAT_ENABLED=false        # chat box renders disabled until Phase 5
FEATURE_PDF_ENABLED=true
FEATURE_COMPATIBILITY_ENABLED=false

PDF_QUEUE=pdf-generation
PDF_SIGNED_URL_TTL=24h
PDF_CONCURRENCY=2
PDF_RENDER_TIMEOUT=60s
CHROME_PATH=/usr/bin/chromium        # chromedp target

# web
NEXT_PUBLIC_DEFAULT_CHART_STYLE=north
```

---

## 9. Task list

| # | Task | Done when |
|---|---|---|
| 3.1 | `packages/astrology-geometry` — pure geometry for both styles | Unit tested without rendering |
| 3.2 | `ChartSVG` — North Indian | Renders all 30 golden fixtures without visual error |
| 3.3 | `ChartSVG` — South Indian | Same; ascendant cell correctly marked |
| 3.4 | Chart a11y: labels, hidden table, keyboard nav | Axe reports zero violations; keyboard-navigable |
| 3.5 | `highlight` prop + tap handlers | `['Saturn','house:10']` visibly marks both |
| 3.6 | D1/D9/D10 switcher | |
| 3.7 | `PlanetTable` + detail sheets | Responsive; no horizontal scroll at 360px |
| 3.8 | `HouseList` + detail sheets | |
| 3.9 | `DashaTimeline` — zoom, current marker, 3-level drill-down | Current period correct against fixtures |
| 3.10 | `YogaCard` + content corpus | ≥10 yogas described in en + hi |
| 3.11 | `TransitPanel` + `SadeSatiIndicator` | Phase and dates match the engine |
| 3.12 | `AstroTerm` glossary (~60 terms, en + hi) | Every jargon term in the UI resolves |
| 3.13 | Home dashboard shell | AI box present, disabled by flag |
| 3.14 | Profile switcher | Switching re-renders everything correctly |
| 3.15 | Go: `asynq` PDF worker with `chromedp` + print route + signed URLs | Downloads a correct, complete PDF |
| 3.16 | Share sheet — image, link, PDF | |
| 3.17 | Storybook for all components incl. states | |
| 3.18 | Loading / error / empty states everywhere | |
| 3.19 | Performance budget enforced in CI | Bundle check fails the build if exceeded |

---

## 10. Testing

**Unit (TS)** — chart geometry (house → SVG cell mapping for both styles); dasha
timeline date→pixel maths; glyph selection incl. retrograde and combust; responsive
breakpoint logic.

**Visual regression** — Playwright screenshots of both chart styles across all 30
fixtures, at three viewport widths, in light and dark. Highest-value suite in the phase:
chart rendering bugs are visual, and visual bugs are invisible to assertions.
Playwright's screenshot comparison is free.

**Unit (Go)** — PDF job payload validation; signed-URL generation and expiry; one-time
print-token minting and single use.

**Integration** — chart API → render pipeline; PDF enqueue → complete → signed URL
valid; PDF job for another user's chart is rejected; profile switch refetches correctly.

**E2E**
```
signup → birth details → kundli renders → switch to South Indian →
open dasha → drill to Pratyantar → download PDF → PDF contains the right ascendant
```

**Accessibility** — `@axe-core/playwright` on every route in CI, zero violations;
manual screen-reader pass on the chart; keyboard-only navigation of the full dashboard.

---

## 11. Security checklist

- [ ] Chart routes enforce ownership; another user's chart returns 404
- [ ] PDF URLs signed, short-lived, no PII in path or filename
- [ ] PDF job authorises the requester against the chart **before** rendering
- [ ] Print-route token is single-use, short-lived, and scoped to one chart
- [ ] `chromedp` runs sandboxed with no network access beyond the print route
- [ ] No birth data in client-side analytics, error reports or URL query strings
- [ ] Share links resolve server-side against the viewer's permissions — they do not embed birth details
- [ ] All rendered content escaped — a user-supplied profile label cannot inject markup
- [ ] CSP allows inline SVG without allowing inline script
- [ ] Rate limit PDF generation (CPU-expensive and trivially abusable)

---

## 12. Analytics events

```
kundli_viewed             { user_id, chart_type }
chart_style_switched      { from, to }
divisional_chart_viewed   { chart_type }
planet_detail_opened      { planet }
house_detail_opened       { house }
dasha_timeline_opened     { user_id }
dasha_period_expanded     { level }
yoga_viewed               { yoga_key }
transits_viewed           { user_id }
sade_sati_explained       { phase }
glossary_term_opened      { term_key }
pdf_requested             { user_id }
pdf_downloaded            { user_id, duration_ms }
kundli_shared             { method }
ai_chat_box_tapped        { enabled: false }   ← measures Phase 5 demand before building it
```

That last event is worth having: it tells you how much latent demand exists for chat
before you spend a rupee on tokens.

---

## 13. Definition of Done

Global DoD **plus**:

- [ ] Both chart styles render correctly for all 30 fixtures
- [ ] Visual regression suite green at 3 viewports × 2 themes
- [ ] Zero axe violations on every Kundli route
- [ ] Chart fully usable with keyboard only and comprehensible via screen reader
- [ ] Every astrology term in the UI has a glossary definition in en and hi
- [ ] PDF generates asynchronously and contains correct data
- [ ] Performance budgets met and CI-enforced

---

## 14. Risks

| Risk | Mitigation |
|---|---|
| North Indian chart geometry is fiddly | Build as pure geometry functions with unit tests before any styling; visual regression locks it |
| Chart unreadable on a 360px screen | Design mobile-first; the chart gets a dedicated full-screen route rather than being squeezed |
| Static content becomes a bottleneck | Scope to ~10 yogas + ~60 glossary terms. It does not need to be exhaustive to be useful. |
| Headless Chrome bloats the API image | Run the worker as a **separate container** from the API binary, with Chromium installed only there. The Go binary stays small; only the worker image is large. |
| PDF worker starves the API | Separate process, bounded concurrency, rate limited per user |
| Users don't understand the chart | Glossary and detail sheets exist for this; measure `glossary_term_opened` and expand where it clusters |

---

## 15. Phase Gate 🔒

- [ ] Kundli dashboard renders a complete, correct chart for all 30 fixtures
- [ ] North and South Indian styles both correct; switcher persists to preferences
- [ ] D1, D9 and D10 all viewable
- [ ] Chart geometry lives in a pure, React-free package (Phase 10 depends on this)
- [ ] Planetary table and house view complete, responsive, with detail sheets
- [ ] Dasha timeline shows correct current Maha/Antar/Pratyantar and drills down
- [ ] Yogas render with written descriptions in en and hi
- [ ] Transits and Sade Sati display with correct phase and dates
- [ ] Glossary covers every term used in the UI
- [ ] PDF generates via the Go `asynq` worker and downloads with a signed URL
- [ ] PDF request for another user's chart is rejected
- [ ] Visual regression suite green
- [ ] Zero accessibility violations; keyboard and screen-reader pass done manually
- [ ] Performance budgets met
- [ ] `task verify` green
- [ ] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
