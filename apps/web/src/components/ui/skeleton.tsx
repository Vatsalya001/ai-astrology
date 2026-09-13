import { cn } from '@/lib/utils'

/**
 * shadcn/ui Skeleton on this project's tokens. See button.tsx for why the
 * generated slate palette was replaced.
 *
 * `motion-reduce:animate-none` is added to the generated version: a
 * pulsing block is exactly the kind of repetitive motion
 * `prefers-reduced-motion` exists to suppress, and skeletons often cover
 * the whole viewport during a load.
 */
function Skeleton({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      // Decorative by definition. Hiding it here rather than at each call
      // site means a skeleton cannot be announced by accident — thirty
      // grey rectangles read out one at a time is worse than no loading
      // state at all. Real text in a loading view stays announced.
      aria-hidden="true"
      className={cn(
        'animate-pulse rounded-md bg-elevated motion-reduce:animate-none',
        className,
      )}
      {...props}
    />
  )
}

export { Skeleton }
