import { execSync } from 'node:child_process'
import { statSync, writeFileSync } from 'node:fs'

import { API_LOG, OFFSET_FILE } from './otp-log'

/**
 * Prepares a known starting state for the suite.
 *
 * Two things, both about isolating a run from the one before it.
 *
 * **The rate-limit window.** The suite makes more OTP requests than a
 * real person would, from one apparent IP. Without a flush, a run
 * inherits the previous run's window and fails on the limiter rather
 * than on anything under test — and that failure looks exactly like a
 * broken auth flow. It resets the window, not the limits: the rules
 * under test are the production rules.
 *
 * **The log offset.** `.run/api.log` is never truncated — the server
 * holds it open — so it still contains OTPs from earlier runs. A test
 * that reads before its own request has been written would otherwise
 * find a stale code sharing the same masked identifier and use it. That
 * was a one-in-four flake presenting as "wrong code".
 */
export default function globalSetup(): void {
  let offset = 0
  try {
    offset = statSync(API_LOG).size
  } catch {
    // No log yet — the stack was just started. Zero is correct.
  }
  writeFileSync(OFFSET_FILE, String(offset))

  try {
    execSync('docker compose exec -T redis redis-cli FLUSHDB', {
      stdio: 'pipe',
      timeout: 10_000,
    })
  } catch {
    // Not fatal. A developer running against a stack started some other
    // way still gets a useful run; they may just hit the limiter.
    console.warn(
      '[e2e] could not flush Redis — rate-limit windows carry over from the previous run',
    )
  }
}
