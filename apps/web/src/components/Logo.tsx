/**
 * Product mark: a stylised orbit with a gold body.
 *
 * Inline SVG rather than an image file so it inherits currentColor,
 * scales without artefacts, and adds no network request.
 */
export function Logo({
  size = 32,
  className = '',
}: {
  size?: number
  className?: string
}) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      className={className}
      role="img"
      aria-label="AI Astrology Companion"
    >
      {/* Outer orbit */}
      <ellipse
        cx="16"
        cy="16"
        rx="14"
        ry="6"
        stroke="#6B4FBB"
        strokeWidth="1.4"
        transform="rotate(-28 16 16)"
        opacity="0.85"
      />
      {/* Inner orbit */}
      <ellipse
        cx="16"
        cy="16"
        rx="10.5"
        ry="4.2"
        stroke="#8B6FD8"
        strokeWidth="1"
        transform="rotate(22 16 16)"
        opacity="0.55"
      />
      {/* Central body */}
      <circle cx="16" cy="16" r="4.2" fill="#D4A857" />
      <circle cx="16" cy="16" r="4.2" fill="url(#logoGlow)" />
      {/* Companion star */}
      <circle cx="27" cy="7" r="1.5" fill="#D4A857" opacity="0.9" />

      <defs>
        <radialGradient id="logoGlow" cx="0.35" cy="0.3" r="0.8">
          <stop offset="0%" stopColor="#FFE9B8" stopOpacity="0.9" />
          <stop offset="100%" stopColor="#D4A857" stopOpacity="0" />
        </radialGradient>
      </defs>
    </svg>
  )
}

export function Wordmark({ className = '' }: { className?: string }) {
  return (
    <div className={`flex items-center gap-2.5 ${className}`}>
      <Logo size={28} />
      <span className="font-serif text-lg tracking-tight text-ink">
        Antara
      </span>
    </div>
  )
}
