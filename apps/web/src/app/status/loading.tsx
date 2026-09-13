import { Panel, SectionLabel } from '@/components/ui'
import { Skeleton } from '@/components/ui/skeleton'

/**
 * Loading state for /status.
 *
 * This is the only route in the app that is `force-dynamic` — it fans out
 * to six dependency probes on every request — so it is the only one where
 * the user can watch a blank page.
 *
 * Shaped like the content it replaces, per the frontend rules: six rows
 * at the real row height, the banner at the real banner height. A
 * skeleton that matches the eventual layout means nothing jumps when the
 * data lands; a centred spinner guarantees it does.
 */
export default function StatusLoading() {
  return (
    <>
      <header className="border-b border-border/60">
        <div className="mx-auto flex h-[69px] max-w-4xl items-center px-6" />
      </header>

      <main
        id="main"
        className="mx-auto max-w-4xl px-6 py-14"
        aria-busy="true"
        aria-live="polite"
      >
        {/* Screen readers get a sentence; the skeleton itself is decorative. */}
        <span className="sr-only">Loading system status…</span>

        <div aria-hidden="true">
          <div className="mb-10">
            <SectionLabel>Operations</SectionLabel>
            <h1 className="font-serif text-4xl tracking-tight">System status</h1>
            <Skeleton className="mt-4 h-4 w-full max-w-2xl" />
            <Skeleton className="mt-2 h-4 w-2/3 max-w-xl" />
          </div>

          <Panel className="mb-8">
            <div className="flex items-center gap-3">
              <Skeleton className="h-2.5 w-2.5 rounded-full" />
              <Skeleton className="h-6 w-56" />
            </div>
          </Panel>

          <section className="mb-8">
            <SectionLabel>Dependencies</SectionLabel>
            <div className="space-y-2.5">
              {Array.from({ length: 6 }).map((_, i) => (
                <Panel key={i} className="p-4">
                  <div className="flex items-start justify-between gap-4">
                    <div className="flex min-w-0 flex-1 items-start gap-3">
                      <Skeleton className="mt-1.5 h-2.5 w-2.5 shrink-0 rounded-full" />
                      <div className="min-w-0 flex-1">
                        <Skeleton className="h-4 w-32" />
                        <Skeleton className="mt-2 h-3 w-full max-w-sm" />
                      </div>
                    </div>
                    <div className="shrink-0 space-y-1.5 text-right">
                      <Skeleton className="ml-auto h-4 w-8" />
                      <Skeleton className="ml-auto h-3 w-10" />
                    </div>
                  </div>
                </Panel>
              ))}
            </div>
          </section>

          <SectionLabel>Build</SectionLabel>
          <Panel>
            <div className="grid gap-6 sm:grid-cols-3">
              {Array.from({ length: 3 }).map((_, i) => (
                <div key={i}>
                  <Skeleton className="h-3 w-20" />
                  <Skeleton className="mt-2 h-4 w-24" />
                </div>
              ))}
            </div>
          </Panel>
        </div>
      </main>
    </>
  )
}
