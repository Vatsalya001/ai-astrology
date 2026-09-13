import type { Metadata, Viewport } from 'next'
import { Inter, JetBrains_Mono, Cormorant_Garamond } from 'next/font/google'
import '@/styles/globals.css'

/**
 * next/font self-hosts these at build time, so there is no request to
 * Google at runtime and no layout shift when they load.
 */
const sans = Inter({
  subsets: ['latin'],
  variable: '--font-sans',
  display: 'swap',
})

const serif = Cormorant_Garamond({
  subsets: ['latin'],
  weight: ['400', '500', '600'],
  variable: '--font-serif',
  display: 'swap',
})

const mono = JetBrains_Mono({
  subsets: ['latin'],
  variable: '--font-mono',
  display: 'swap',
})

export const metadata: Metadata = {
  title: 'Ayana — Your personal AI astrologer',
  description:
    'A personal AI astrologer that understands your birth chart, your life context and your history — and connects you to a human astrologer when you need one.',
  robots: { index: false, follow: false }, // pre-launch
}

export const viewport: Viewport = {
  themeColor: '#0B1026',
  colorScheme: 'dark',
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html
      lang="en"
      className={`${sans.variable} ${serif.variable} ${mono.variable}`}
    >
      <body className="min-h-dvh bg-base text-ink">
        {/* Skip link: the first thing a keyboard user hits, letting them
            jump past the header instead of tabbing through it. */}
        <a
          href="#main"
          className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-lg focus:bg-gold focus:px-4 focus:py-2 focus:text-base"
        >
          Skip to content
        </a>
        {children}
      </body>
    </html>
  )
}
