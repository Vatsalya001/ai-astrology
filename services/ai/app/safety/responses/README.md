# Crisis responses

The exact bytes a person wrote. Loaded from disk, never composed by a model —
see the reasoning in `../crisis.py`.

## Why there are no phone numbers here

There were three Indian helplines in this file: Tele-MANAS, AASRA and the
Vandrevala Foundation. They were **transcribed, not dialled**, and a transcribed
phone number is a claim about the world that goes stale without anything in this
repository changing.

A wrong helpline number is worse than no number. It costs someone in crisis the
one attempt they were willing to make, and they do not try again. No test can
check it: `test_it_carries_a_helpline_number` proved a *number* was present, not
that it *connects*.

So the numbers are out until a human has dialled each one.
`findahelpline.com` is maintained by people whose job that is, covers every
country rather than one, and cannot go stale here.

## Putting them back

1. Dial each number. Confirm it connects, is free, and is staffed 24x7.
2. Add it with the date it was verified.
3. Re-verify on a schedule — services change numbers and lose funding.

A local number is a better answer than a directory lookup for someone in
distress, so this is worth doing. It is just not worth guessing at.

## Verification log

**2026-09-23 — `findahelpline.com`, verified by Vatsalya from India.**

Checked from a normal Indian connection, not a VPN. The directory resolved to India
and offered valid, working numbers. The link in `crisis.en.md` and `crisis.hi.md`
therefore does what the response promises.

This closes the open item that has sat on the crisis path since the numbers were
removed: the response's one external claim had never been checked by a person, and
now has been.

### What this does NOT close

The directory is verified; **no local number has been added back.** Steps 1–3 above
still apply if you want one inline — and a local number is still the better answer
for someone in distress than a lookup, so it is still worth doing.

**Re-verify this.** A directory can change what it lists for a country, and a
helpline can lose funding, without a single byte changing in this repository. This
log entry has a date on it for that reason. Treat it as stale after roughly six
months, or immediately if anyone reports the link not resolving.
