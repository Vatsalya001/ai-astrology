'use client'

import Link from 'next/link'
import { usePathname, useRouter } from 'next/navigation'

import { Wordmark } from '@/components/Logo'
import { Button } from '@/components/ui/button'
import { useLocale } from '@/lib/i18n/context'
import { resetAnalytics } from '@/lib/analytics'
import { usersApi } from '@/lib/users-api'
import { cn } from '@/lib/utils'

/**
 * Shared shell for the settings screens.
 *
 * A layout rather than four copies of the same header: the nav is the
 * thing most likely to drift, and four copies drift.
 */
export default function SettingsLayout({ children }: { children: React.ReactNode }) {
  const { t } = useLocale()
  const pathname = usePathname()
  const router = useRouter()

  const tabs = [
    { href: '/settings/profile', label: t.settings.profile },
    { href: '/settings/preferences', label: t.settings.preferences },
    { href: '/settings/sessions', label: t.settings.sessions },
    { href: '/settings/delete', label: t.settings.deleteAccount },
  ]

  return (
    <>
      <header className="border-b border-border/60">
        <div className="mx-auto flex max-w-3xl items-center justify-between px-6 py-5">
          <Link href="/home" aria-label={t.nav.home}>
            <Wordmark />
          </Link>
          <Button
            variant="secondary"
            size="sm"
            onClick={async () => {
              await usersApi.signOut()
              resetAnalytics()
              router.replace('/')
            }}
          >
            {t.nav.signOut}
          </Button>
        </div>
      </header>

      <main id="main" className="mx-auto max-w-3xl px-6 py-12">
        <h1 className="font-serif text-4xl tracking-tight">{t.settings.title}</h1>

        {/* A real nav landmark, so a screen reader can jump straight to
            it rather than tabbing through the header every time. */}
        <nav aria-label={t.settings.title} className="mt-8 border-b border-border/60">
          <ul className="flex flex-wrap gap-1">
            {tabs.map((tab) => {
              const active = pathname === tab.href
              return (
                <li key={tab.href}>
                  <Link
                    href={tab.href}
                    // aria-current is what tells assistive technology which
                    // tab you are on. Colour alone does not.
                    aria-current={active ? 'page' : undefined}
                    className={cn(
                      'inline-block rounded-t-lg px-4 py-3 text-sm transition-colors',
                      active
                        ? 'border-b-2 border-gold text-ink'
                        : 'text-ink-muted hover:text-ink',
                    )}
                  >
                    {tab.label}
                  </Link>
                </li>
              )
            })}
          </ul>
        </nav>

        <div className="mt-8">{children}</div>
      </main>
    </>
  )
}

