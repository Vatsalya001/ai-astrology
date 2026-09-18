'use client'

import * as React from 'react'
import * as DialogPrimitive from '@radix-ui/react-dialog'
import { X } from 'lucide-react'

import { cn } from '@/lib/utils'

/**
 * shadcn/ui Dialog on this project's tokens. See button.tsx for why the
 * generated slate palette was replaced.
 */
/**
 * The last element focused outside any dialog.
 *
 * ── Why a document listener and not a prop ──
 *
 * Every dialog in this app is CONTROLLED: a plain button calls
 * `setOpen(true)` and `<Dialog open={open}>` re-renders. Radix's
 * `onOpenChange` fires only for ITS OWN dismissal paths — Escape, the
 * overlay, the close button — so it never sees the opening at all.
 * Measured, after three wrong guesses: the handler logged a CLOSE with
 * no matching OPEN.
 *
 * The three that failed, so nobody repeats them. `onCloseAutoFocus`
 * loses a race with Radix's focus-scope teardown. A `useState`
 * initialiser inside `DialogContent` captures `<body>` at page load,
 * because that component renders on every pass and only its portal is
 * gated on `open`. An effect in this wrapper runs after the child's, by
 * which time focus has already moved.
 *
 * Tracking focus continuously sidesteps all of it.
 */
let lastFocusOutsideDialog: HTMLElement | null = null

if (typeof document !== 'undefined') {
  document.addEventListener(
    'focusin',
    (event) => {
      const target = event.target as HTMLElement | null
      // Focus moving WITHIN a dialog is not a new opener — recording it
      // would make "restore" mean "stay where you are".
      if (!target || target.closest('[role="dialog"]')) return
      lastFocusOutsideDialog = target
    },
    true,
  )
}

/**
 * Dialog that gives focus back to whatever opened it.
 *
 * Radix restores focus to its `DialogTrigger`. There is no trigger here
 * — see above — so closing dropped focus to `<body>`. For a keyboard
 * user that means re-traversing the whole screen after every glance at a
 * definition; for a screen-reader user the reading position is lost and
 * they resume at the top of the document with nothing announced.
 *
 * All six dialogs in this app had it.
 */
const Dialog = ({
  onOpenChange,
  ...props
}: React.ComponentPropsWithoutRef<typeof DialogPrimitive.Root>) => (
  <DialogPrimitive.Root
    onOpenChange={(next) => {
      if (!next) {
        /*
          Read at CLOSE time, from the tracker.

          An earlier version stashed it in a ref during renders where
          `open` was false — one render too early, because focusing the
          trigger does not re-render and the ref still held whatever came
          before. The tracker only records elements OUTSIDE a dialog, so
          while one is open it still holds the control that opened it.
        */
        const target = lastFocusOutsideDialog
        if (target) {
          /*
            A frame later. Radix's focus-scope teardown runs after this
            callback and would otherwise be the last writer — measured
            landing on <body> when restored synchronously.
          */
          requestAnimationFrame(() => {
            if (document.contains(target)) target.focus()
          })
        }
      }
      onOpenChange?.(next)
    }}
    {...props}
  />
)

const DialogTrigger = DialogPrimitive.Trigger

const DialogPortal = DialogPrimitive.Portal

const DialogClose = DialogPrimitive.Close

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      'fixed inset-0 z-50 bg-base/80 backdrop-blur-sm',
      'data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
      className,
    )}
    {...props}
  />
))
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPortal>
    <DialogOverlay />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        'fixed left-[50%] top-[50%] z-50 grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4',
        'border border-border bg-popover p-6 text-popover-foreground shadow-lg duration-200 sm:rounded-lg',
        'data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0',
        'data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95',
        'data-[state=closed]:slide-out-to-left-1/2 data-[state=closed]:slide-out-to-top-[48%]',
        'data-[state=open]:slide-in-from-left-1/2 data-[state=open]:slide-in-from-top-[48%]',
        className,
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close
        className={cn(
          'absolute right-4 top-4 rounded-sm opacity-70 transition-opacity hover:opacity-100',
          'focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 focus:ring-offset-background',
          'disabled:pointer-events-none data-[state=open]:bg-elevated data-[state=open]:text-muted-foreground',
        )}
      >
        <X className="h-4 w-4" />
        <span className="sr-only">Close</span>
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPortal>
))
DialogContent.displayName = DialogPrimitive.Content.displayName

const DialogHeader = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      'flex flex-col space-y-1.5 text-center sm:text-left',
      className,
    )}
    {...props}
  />
)
DialogHeader.displayName = 'DialogHeader'

const DialogFooter = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      'flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2',
      className,
    )}
    {...props}
  />
)
DialogFooter.displayName = 'DialogFooter'

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn('font-serif text-lg leading-none tracking-tight', className)}
    {...props}
  />
))
DialogTitle.displayName = DialogPrimitive.Title.displayName

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn('text-sm text-muted-foreground', className)}
    {...props}
  />
))
DialogDescription.displayName = DialogPrimitive.Description.displayName

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogTrigger,
  DialogClose,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
}
