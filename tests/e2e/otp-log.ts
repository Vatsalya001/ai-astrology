import { readFileSync, statSync } from 'node:fs'

export const API_LOG = '.run/api.log'
export const OFFSET_FILE = '.run/e2e-log-offset'

/**
 * Reads OTPs out of the API's stderr, the way a developer does.
 *
 * `ConsoleChannel` is the real development delivery channel, so reading
 * it exercises the whole path rather than stubbing the part most likely
 * to break.
 *
 * ── Why a watcher rather than "find the code for this email" ──
 *
 * The log only ever shows a MASKED identifier — `a***@example.com` —
 * because logging the real one would put PII in a log file. Masks
 * collide by construction: every address starting with `a` looks
 * identical. Three attempts at making that work failed in a different
 * way each time:
 *
 *   1. "the most recent code"   — belonged to whichever parallel test
 *                                 asked last.
 *   2. "the last code for this  — the log is never truncated, so it
 *      mask"                      matched a run from ten minutes ago.
 *   3. "…since this run began"  — CI retries generate a NEW address that
 *                                 masks the same way, so a retry read the
 *                                 first attempt's code and submitted it
 *                                 against a different account.
 *
 * The answer needs BOTH halves, because each removes a different source
 * of ambiguity and neither is sufficient alone:
 *
 *   • a byte offset captured immediately before the request excludes
 *     history and earlier retry attempts;
 *   • the masked identifier excludes tests running concurrently in other
 *     workers, whose codes land inside the same window.
 *
 * Offset alone takes "the first new code", which under six parallel
 * workers is frequently somebody else's. Mask alone matches a code from
 * ten minutes ago. Together they are exact.
 *
 * This does require concurrently-running tests to use distinct FIRST
 * LETTERS, since that is all the mask preserves. See `uniqueEmail`.
 */

export interface OTPWatcher {
  /** Resolves with the first code logged after the watcher was created. */
  next(timeoutMs?: number): Promise<string>
}

/**
 * Marks the current end of the log.
 *
 * Call this BEFORE the action that triggers a send — otherwise the
 * request may already have been written and there is nothing new to see.
 */
export function watchOTP(email: string): OTPWatcher {
  const from = logSize()
  const masked = maskEmail(email)
  // The mask contains `***` and `@`; an unescaped `*` is a quantifier.
  const pattern = new RegExp(`── OTP for ${escapeRegExp(masked)}: (\\d+)`)

  return {
    async next(timeoutMs = 10_000): Promise<string> {
      const deadline = Date.now() + timeoutMs

      while (Date.now() < deadline) {
        // Byte offset, not line count: the log is UTF-8 with multi-byte
        // characters in it, so slicing the decoded string would drift.
        const fresh = readFileSync(API_LOG).subarray(from).toString('utf8')
        const match = pattern.exec(fresh)
        if (match?.[1]) return match[1]

        await new Promise((resolve) => setTimeout(resolve, 50))
      }

      throw new Error(
        `no OTP for ${email} (logged as ${masked}) appeared in ${API_LOG} ` +
          `within ${timeoutMs}ms of the request. Is AUTH_CHANNEL=console, and ` +
          `did the request return 200 rather than 429?`,
      )
    },
  }
}

/** Mirrors maskIdentifier in services/api/internal/auth/channel.go. */
function maskEmail(email: string): string {
  const at = email.lastIndexOf('@')
  return email.slice(0, 1) + '***' + email.slice(at)
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function logSize(): number {
  try {
    return statSync(API_LOG).size
  } catch {
    // No log yet. Zero is correct — everything in it will be new.
    return 0
  }
}

/**
 * A unique address whose MASK is also unique.
 *
 * Uniqueness of the address keeps accounts and per-identifier rate limits
 * separate. Uniqueness of the mask is what lets `watchOTP` tell one
 * concurrent test from another — the log keeps only the first character,
 * so `signup-…` and `shot-…` would be indistinguishable.
 *
 * Callers therefore pass a DISTINCT FIRST LETTER. Only tests that can run
 * at the same time need to differ, but keeping every one distinct is
 * cheaper than reasoning about which ones can.
 */
export function uniqueEmail(distinctFirstLetter: string): string {
  return `${distinctFirstLetter}${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`
}
