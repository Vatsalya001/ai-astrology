import type { Metadata, Viewport } from 'next'
import { Inter, JetBrains_Mono, Cormorant_Garamond } from 'next/font/google'
import '@/styles/globals.css'

import { A11yInspectorMount } from '@/components/dev/A11yInspectorMount'
import { LocaleProvider } from '@/lib/i18n/context'
import { ProfileProvider } from '@/lib/profile-context'

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
      <body className="min-h-dvh bg-background text-ink">
        {/* Skip link: the first thing a keyboard user hits, letting them
            jump past the header instead of tabbing through it.

            Deliberately NOT inside LocaleProvider and deliberately not
            translated. It must render in the server HTML so it works
            before hydration — a keyboard user who tabs immediately on a
            slow connection is exactly who it is for. */}
        <a
          href="#main"
          /*
            `bg-primary` + `text-primary-foreground`, stated as a pair.

            This read `focus:bg-gold … focus:text-base`, and `text-base`
            was doing two jobs: the font size, and — back when the palette
            still had a colour named `base` — the navy that made the label
            readable on gold. Commit 020d742 removed that token to fix
            <Input>, whose text was being painted the page background by
            the same collision. It fixed `bg-base` on the <body> six lines
            above and did not notice line 64.

            So the class kept compiling, kept emitting
            `font-size: 1rem` and nothing else, and the label fell back to
            inheriting `text-ink` from the body: #F2F3F8 on #D4A857, which
            measures 1.99:1 against a 4.5:1 floor. The intended pairing is
            8.54:1. It was broken on every route, for the sighted keyboard
            user this element exists for, and axe cannot see it because
            the link is `sr-only` until focused — the failing state does
            not exist during a scan.

            Named through the semantic pair rather than `text-background`,
            so the foreground cannot drift from the surface again:
            `primary.foreground` is defined as the colour that goes ON
            `primary` (packages/ui/src/index.ts).
          */
          className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-lg focus:bg-primary focus:px-4 focus:py-2 focus:text-base focus:text-primary-foreground"
        >
          Skip to content
        </a>
        {/*
          ProfileProvider inside LocaleProvider: the switcher's own copy
          comes from the dictionary, and nothing about which chart is
          selected affects which language it is shown in. Both render
          their server snapshot first — English, no selection — and both
          correct during hydration.
        */}
        <LocaleProvider>
          <ProfileProvider>{children}</ProfileProvider>

          {/*
            The accessibility inspector, when the URL carries ?a11y=1.
            Lazily imported, so it stays out of the first-load bundle.
          */}
          <A11yInspectorMount />
        </LocaleProvider>
      </body>
    </html>
  )
}
