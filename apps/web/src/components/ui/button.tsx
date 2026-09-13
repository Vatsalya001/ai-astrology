import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'

import { cn } from '@/lib/utils'

/**
 * shadcn/ui Button, rewritten onto this project's design tokens.
 *
 * The CLI generates these with `baseColor: slate` baked in as literal
 * classes (`bg-slate-900`, `text-slate-50`, `bg-white`). That renders a
 * light-grey control on a midnight-navy page, and it routes around
 * `tailwind.config.ts` entirely — which the frontend rules forbid. Every
 * colour here is a token, so a palette change reaches this component.
 *
 * No `dark:` variants: the app is dark-only (`color-scheme: dark` in
 * globals.css). Carrying a light mode nobody can reach is dead weight
 * that still has to be kept correct.
 */
const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors ' +
    'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background ' +
    'disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0',
  {
    variants: {
      variant: {
        // Gold is the call-to-action colour throughout the product.
        default: 'bg-primary text-primary-foreground shadow hover:bg-gold-soft',
        destructive:
          'bg-destructive text-destructive-foreground shadow-sm hover:bg-destructive/90',
        outline:
          'border border-border bg-transparent shadow-sm hover:bg-elevated hover:text-foreground',
        secondary:
          'border border-border bg-secondary text-secondary-foreground shadow-sm hover:bg-elevated',
        ghost: 'hover:bg-elevated hover:text-foreground',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-9 px-4 py-2',
        sm: 'h-8 rounded-md px-3 text-xs',
        lg: 'h-11 rounded-lg px-8',
        icon: 'h-9 w-9',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  },
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    )
  },
)
Button.displayName = 'Button'

export { Button, buttonVariants }
