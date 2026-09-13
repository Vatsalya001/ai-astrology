import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

/**
 * Merge class names, resolving Tailwind conflicts.
 *
 * twMerge is what makes `<Button className="px-8" />` actually override
 * the variant's `px-5` instead of producing both and letting CSS source
 * order decide. This is the shadcn/ui convention and every component
 * below depends on it.
 */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
