# Frontend rules — apps/web

## Stack
Next.js App Router, React, Tailwind, TypeScript strict. Server Components by default;
`'use client'` only where interactivity genuinely requires it.

## Design tokens
Defined once in `apps/web/tailwind.config.ts`. **Never hardcode a hex value in a
component.** The palette is midnight navy / deep purple / gold, and it should read as a
premium technology product that happens to be about astrology — not a cheap astrology app.

Nothing in the build catches a violation. Tailwind drops a class naming a colour it
doesn't know **silently**: `tsc` sees a valid string, `next build` succeeds, and the
element renders with no background. The only gate that notices is a computed-style
assertion in `tests/e2e/smoke.spec.ts`. Add one for any component whose colour matters.

### After `npx shadcn add`
The CLI bakes its `baseColor` in as literal classes — `bg-slate-900`, `text-slate-50`,
`bg-white` — which is a light-grey control on a midnight-navy page, and routes around
this file entirely. **Rewrite every generated component onto the semantic tokens**
(`bg-primary`, `text-muted-foreground`, `border-input`, …) before committing it, and
drop the `dark:` variants: the app is dark-only, so a light mode nobody can reach is
dead weight that still has to be kept correct.

## Every screen needs four states
Loading, error, empty, and populated. A skeleton that looks like the eventual content
beats a spinner on a blank page. This is in the Definition of Done and it is enforced at
review.

## Accessibility is not optional
- Focus rings stay visible. Never `outline: none` without a replacement.
- Colour never carries meaning alone — pair it with a glyph or a label. Roughly 8% of
  men have red-green colour blindness, and they read status pages too.
- Honour `prefers-reduced-motion`.
- The chart SVG (Phase 3) needs per-element `aria-label`s and a visually-hidden table
  duplicating the data. An SVG is meaningless to a screen reader otherwise.

## Determinism in rendering
No `Math.random()` or `Date.now()` during render — the server and client outputs must
agree or React reports a hydration mismatch. `Starfield.tsx` uses a seeded PRNG for
exactly this reason.

## Data
The web app talks **only** to the Go API. It has no knowledge that `astro-service` or
`ai-service` exist. Fetch through `src/lib/api.ts`; never hardcode a URL in a component.

## Never render untrusted markdown as HTML
From Phase 5 the chat renders model output. Sanitise on an allowlist. An LLM emitting
`<img onerror=...>` is a real XSS vector, not a hypothetical.
