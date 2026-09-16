import { execSync } from 'node:child_process'
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
 * Clears the per-IP OTP window, and only that one.
 *
 * The suite makes more OTP requests from one apparent address than any
 * person would, and it is now exactly at the ceiling: a full run makes
 * 31 requests against `OTPRequestPerIP`'s 30 per 15 minutes, so the last
 * spec to ask gets a 429 and fails with the page still sitting on
 * /auth — which reads as broken authentication, not as a limiter.
 *
 * ── Why clear rather than raise the limit ──
 *
 * 30 is a production number with a written rationale: it exists to catch
 * one source spraying hundreds of requests, and it is deliberately loose
 * because Indian carriers run carrier-grade NAT with thousands of
 * subscribers behind one address. Moving a security control because the
 * test suite grew is the wrong direction, and it would have to be moved
 * again at the next screen.
 *
 * The suite IS that NAT case: many independent users behind one address.
 * So the rule that does not model it is cleared, and the rules that DO
 * — per-identifier 3/15min and 10/day, which key on the thing actually
 * being attacked — stay fully in force for every test.
 *
 * ── An honest gap ──
 *
 * `OTPRequestPerIP` has no test anywhere: not in Go, not here, and none
 * before this change either. This makes a never-exercised guard slightly
 * less exercised, which is worth saying out loud rather than leaving for
 * somebody to discover.
 */
function clearIPWindow(): void {
  try {
    execSync(
      "docker compose exec -T redis redis-cli EVAL \"local k=redis.call('keys','rl:otp_req_ip:*'); " +
        'for i=1,#k do redis.call(\'del\',k[i]) end; return #k" 0',
      { stdio: 'pipe', timeout: 10_000 },
    )
  } catch {
    // Not fatal: a developer running against a stack started some other
    // way still gets a useful run, they may just hit the limiter.
  }
}

/**
 * Marks the current end of the log.
 *
 * Call this BEFORE the action that triggers a send — otherwise the
 * request may already have been written and there is nothing new to see.
 */
export function watchOTP(identifier: string): OTPWatcher {
  clearIPWindow()
  const from = logSize()
  const masked = maskIdentifier(identifier)
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
        `no OTP for ${identifier} (logged as ${masked}) appeared in ${API_LOG} ` +
          `within ${timeoutMs}ms of the request. Is AUTH_CHANNEL=console, and ` +
          `did the request return 200 rather than 429?`,
      )
    },
  }
}

/**
 * Mirrors maskIdentifier in services/api/internal/auth/channel.go.
 *
 * Both branches, because both channels go through the same log line. The
 * phone form keeps a three-character head and a three-digit tail, which
 * is a WEAKER discriminator than the email form's first letter plus
 * domain — every Indian mobile masks to `+91********`, so concurrent
 * phone tests must differ in their last three digits. `uniquePhone`
 * handles that.
 */
function maskIdentifier(identifier: string): string {
  const at = identifier.lastIndexOf('@')
  if (at > 0) {
    return identifier.slice(0, 1) + '***' + identifier.slice(at)
  }

  const keepHead = 3
  const keepTail = 3
  if (identifier.length <= keepHead + keepTail) {
    return '*'.repeat(identifier.length)
  }
  return (
    identifier.slice(0, keepHead) +
    '*'.repeat(identifier.length - keepHead - keepTail) +
    identifier.slice(-keepTail)
  )
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

/**
 * A unique E.164 number whose MASK is also unique.
 *
 * The phone mask keeps only three leading characters and three trailing
 * digits, so `+919812345000` and `+919899999000` are indistinguishable in
 * the log. The caller supplies a distinct three-digit TAIL for the same
 * reason `uniqueEmail` takes a distinct first letter.
 *
 * The middle is randomised so repeated runs do not collide on the
 * per-identifier rate limit, which keys on the whole number.
 */
export function uniquePhone(distinctTail: string): string {
  if (!/^\d{3}$/.test(distinctTail)) {
    throw new Error(`uniquePhone needs exactly three digits, got ${distinctTail}`)
  }
  const middle = String(Math.floor(Math.random() * 1e7)).padStart(7, '0')
  return `+91${middle}${distinctTail}`
}
