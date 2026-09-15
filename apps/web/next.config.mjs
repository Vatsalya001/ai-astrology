/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,

  // @ayana/types ships TypeScript source rather than build output,
  // so Next has to compile it alongside the app.
  transpilePackages: ['@ayana/types'],

  // Fail the build on type errors. Letting these through "temporarily"
  // is how a codebase ends up with hundreds of them.
  typescript: { ignoreBuildErrors: false },

  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'X-Frame-Options', value: 'DENY' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
          {
            key: 'Permissions-Policy',
            value: 'camera=(), microphone=(), geolocation=()',
          },
          { key: 'Content-Security-Policy', value: csp() },
        ],
      },
    ]
  },
}

/**
 * The Content Security Policy.
 *
 * This header was missing while the other four were present, which is the
 * usual shape of the mistake: the others are one line each and this one
 * requires deciding what the app is allowed to do.
 *
 * ── The script-src decision, stated rather than buried ──
 *
 * `script-src` carries `'unsafe-inline'`, which is the weakest part of
 * this policy and is a deliberate, dated choice rather than an oversight.
 *
 * Next's App Router streams the RSC payload through inline
 * `self.__next_f.push(...)` scripts. Under `script-src 'self'` those are
 * blocked, the app never hydrates, and the result is a page that renders
 * perfectly and does nothing when clicked. That was measured, not
 * assumed: tests/e2e/csp.spec.ts failed on a click timeout with the
 * strict policy in place.
 *
 * Next offers exactly two ways out and both cost something real:
 *
 *   • a per-request nonce via proxy.ts — strong, but it forces EVERY page
 *     into dynamic rendering. Static optimisation, ISR and CDN caching
 *     all stop, on an app whose audience is on Indian mobile networks
 *     where TTFB is the thing that hurts.
 *   • experimental SRI (`experimental.sri`) — keeps static rendering and
 *     is strict, but it is experimental, and this repo requires an ADR
 *     before adopting new technology.
 *
 * What makes `'unsafe-inline'` acceptable TODAY is that the app renders
 * no untrusted content at all: its own strings, and the user's own name,
 * through React, which escapes by default. There is nothing for an
 * injected script to arrive in.
 *
 * That changes in Phase 5, when the chat renders model output — an LLM
 * emitting `<img onerror=...>` is a real vector, not a hypothetical.
 * PHASE-05 therefore carries a blocking gate item to replace this with a
 * nonce or SRI BEFORE the first model response is rendered. It is
 * tracked as machinery rather than left as a comment, because a comment
 * is not a plan.
 *
 * Everything else here is strict, and the rest of the policy is not
 * decoration: `connect-src` is what stops a successful injection from
 * exfiltrating anywhere, `object-src 'none'` and `base-uri 'self'` close
 * two standard bypass routes, and `form-action 'self'` stops a form being
 * repointed at an attacker's collector.
 */
function csp() {
  const dev = process.env.NODE_ENV !== 'production'
  const api = process.env.NEXT_PUBLIC_API_URL ?? 'http://localhost:4000'

  return [
    "default-src 'self'",
    // No object, no embed, no applet. Nothing here needs them and each is
    // a bypass route for the rest of the policy.
    "object-src 'none'",
    // Stops a <base> tag rewriting every relative URL on the page.
    "base-uri 'self'",
    // Belt and braces with X-Frame-Options, which older browsers honour
    // and newer ones increasingly do not.
    "frame-ancestors 'none'",
    "form-action 'self'",
    // See the note above. `'unsafe-eval'` is development only — React
    // uses eval there to reconstruct server-side error stacks, and
    // neither React nor Next needs it in production.
    `script-src 'self' 'unsafe-inline'${dev ? " 'unsafe-eval'" : ''}`,
    // Tailwind and Next both inject inline styles. A style injection is
    // a defacement rather than a takeover, which is why this one is a
    // much smaller concession than the script-src equivalent.
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "font-src 'self' data:",
    // The API, and nothing else — this is the directive that turns a
    // hypothetical injection into a dead end, because it cannot phone
    // home. The web app talks only to the Go service; astro-service and
    // ai-service are internal and must not become browser-reachable by
    // accident.
    `connect-src 'self' ${api}${dev ? ' ws: wss:' : ''}`,
  ].join('; ')
}

export default nextConfig
