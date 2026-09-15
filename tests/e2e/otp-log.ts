import { readFileSync } from 'node:fs'

export const API_LOG = '.run/api.log'
export const OFFSET_FILE = '.run/e2e-log-offset'

/**
 * Where this run's log starts.
 *
 * The API log is not truncated between runs — the server holds the file
 * open — so it still contains OTPs from earlier runs. Reading from the
 * offset global-setup recorded is what stops a test picking up a stale
 * code that happens to share a masked identifier.
 */
function runStartOffset(): number {
  try {
    return Number(readFileSync(OFFSET_FILE, 'utf8')) || 0
  } catch {
    return 0
  }
}

/**
 * Reads an OTP out of the API's stderr, the way a developer does.
 *
 * ConsoleChannel is the real development delivery channel, so reading it
 * exercises the whole path rather than stubbing the part most likely to
 * break.
 *
 * Matched by IDENTIFIER, not "the most recent code". Playwright runs
 * specs in parallel, so "most recent" belongs to whichever test asked
 * last — which produced three intermittent failures that looked like a
 * broken auth flow and were not.
 */
export async function codeFor(
  email: string,
  opts: { notEqualTo?: string; timeoutMs?: number } = {},
): Promise<string> {
  const { notEqualTo, timeoutMs = 5000 } = opts
  const masked = maskEmail(email)

  // Escape before embedding in a pattern: the mask contains `***` and an
  // `@`, and an unescaped `*` is a quantifier that matches nothing here.
  const pattern = new RegExp(`OTP for ${escapeRegExp(masked)}: (\\d+)`, 'g')

  // Polls rather than reading once. The request has returned by the time
  // a test calls this, but the log line is written to stderr by a
  // different process and there is a small window before it lands. Under
  // parallel load that window produced an intermittent failure that
  // looked like a broken sign-in — the second login in the
  // returning-user test read the FIRST login's code.
  //
  // `notEqualTo` is what makes a re-login deterministic: it waits for a
  // code that is genuinely new rather than accepting whatever is there.
  const deadline = Date.now() + timeoutMs
  let lastSeen: string | undefined

  const offset = runStartOffset()

  while (Date.now() < deadline) {
    const log = readFileSync(API_LOG).subarray(offset).toString('utf8')
    const found = [...log.matchAll(pattern)].at(-1)?.[1]
    lastSeen = found

    if (found && found !== notEqualTo) return found
    await new Promise((resolve) => setTimeout(resolve, 50))
  }

  throw new Error(
    notEqualTo
      ? `no NEW OTP for ${email} within ${timeoutMs}ms (still seeing ${lastSeen ?? 'nothing'}). ` +
        `Was the second request rate limited?`
      : `no OTP for ${email} (masked ${masked}) in ${API_LOG} within ${timeoutMs}ms. ` +
        `Is AUTH_CHANNEL=console, and did the request succeed?`,
  )
}

/** Mirrors maskIdentifier in services/api/internal/auth/channel.go. */
function maskEmail(email: string): string {
  const at = email.lastIndexOf('@')
  return email.slice(0, 1) + '***' + email.slice(at)
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * A unique address whose MASK is also unique.
 *
 * The mask keeps only the first character, so two concurrent tests using
 * `signup-…` and `shot-…` would both log as `s***@example.com` and read
 * each other's codes. Each caller passes a distinct letter.
 */
export function uniqueEmail(distinctFirstLetter: string): string {
  return `${distinctFirstLetter}${Date.now()}-${Math.floor(Math.random() * 1e6)}@example.com`
}
