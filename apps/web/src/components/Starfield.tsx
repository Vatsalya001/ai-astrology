/**
 * Decorative starfield.
 *
 * Implemented as absolutely-positioned elements with **pixel** sizes
 * rather than an SVG with a unit viewBox.
 *
 * The SVG approach fails here: with `viewBox="0 0 100 100"` and
 * `preserveAspectRatio="slice"`, 100 user units are stretched across the
 * container, so a radius of 1 renders as a ~24px blob on a tall hero.
 * Pixel sizes are immune to container dimensions, which is what you want
 * for something that must look identical at every viewport.
 *
 * Positions are deterministic (seeded PRNG, not Math.random) so the
 * server render and the client hydration agree — a random layout would
 * produce a hydration mismatch.
 *
 * aria-hidden throughout: this is texture, and a screen reader
 * announcing ninety stars would be actively hostile.
 */

type Star = {
  left: number // %
  top: number // %
  size: number // px
  opacity: number
  delay: number // s
  gold: boolean
}

/** Deterministic PRNG (mulberry32). */
function makeRng(seed: number): () => number {
  let a = seed
  return () => {
    a |= 0
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function generateStars(count: number, seed: number): Star[] {
  const rng = makeRng(seed)

  return Array.from({ length: count }, () => {
    const roll = rng()
    return {
      left: Math.round(rng() * 1000) / 10,
      top: Math.round(rng() * 1000) / 10,
      // Mostly 1px pinpricks, occasionally 2px. Nothing larger — real
      // stars are points of light, not discs.
      size: roll > 0.88 ? 2 : 1,
      opacity: Math.round((rng() * 0.45 + 0.2) * 100) / 100,
      delay: Math.round(rng() * 60) / 10,
      gold: rng() > 0.86,
    }
  })
}

const STARS = generateStars(110, 20260913)

export function Starfield({ className = '' }: { className?: string }) {
  return (
    <div
      aria-hidden="true"
      className={`pointer-events-none absolute inset-0 overflow-hidden ${className}`}
    >
      {STARS.map((s, i) => (
        <span
          key={i}
          className="absolute rounded-full animate-twinkle"
          style={{
            left: `${s.left}%`,
            top: `${s.top}%`,
            width: `${s.size}px`,
            height: `${s.size}px`,
            opacity: s.opacity,
            backgroundColor: s.gold ? '#D4A857' : '#F2F3F8',
            // A faint halo on the larger stars stops them reading as
            // flat dots without costing a blur filter.
            boxShadow: s.size > 1 ? `0 0 4px ${s.gold ? '#D4A857' : '#F2F3F8'}` : undefined,
            animationDelay: `${s.delay}s`,
          }}
        />
      ))}
    </div>
  )
}
