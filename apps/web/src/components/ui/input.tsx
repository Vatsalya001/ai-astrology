import * as React from 'react'

import { cn } from '@/lib/utils'

/**
 * shadcn/ui Input on this project's tokens. See button.tsx for why the
 * generated slate palette was replaced.
 *
 * `text-base` below the `md` breakpoint is deliberate and must not be
 * reduced: iOS Safari zooms the viewport on focus for any input under
 * 16px, and the user is then stuck at that zoom level.
 */
const Input = React.forwardRef<HTMLInputElement, React.ComponentProps<'input'>>(
  ({ className, type, ...props }, ref) => {
    return (
      <input
        type={type}
        className={cn(
          'flex h-9 w-full rounded-md border border-input bg-transparent px-3 py-1 text-base shadow-sm transition-colors',
          /*
            An EXPLICIT colour and weight, not inherited.

            This field used to set neither. `text-base` was intended as a
            font size and Tailwind also read it as the colour token named
            `base` — #0B1026, the page background — so typed text was the
            same colour as the page behind it. See tailwind.config.ts.

            Stating both here means the field no longer depends on what
            it happens to inherit, which is what allowed the bug to reach
            a user in the first place. `font-medium` because a value
            somebody typed should sit visually above its own label.
          */
          'text-foreground font-medium',
          'file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground',
          'placeholder:text-ink-faint placeholder:font-normal',
          'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
          'disabled:cursor-not-allowed disabled:opacity-50 md:text-sm',
          className,
        )}
        ref={ref}
        {...props}
      />
    )
  },
)
Input.displayName = 'Input'

export { Input }
