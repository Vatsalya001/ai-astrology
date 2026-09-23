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

So the numbers were out until a human had dialled each one.
`findahelpline.com` is maintained by people whose job that is, covers every
country rather than one, and cannot go stale here — so it stays, alongside them.

**They came back on 2026-09-23.** See the log below.

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

**2026-09-23 — the three numbers, dialled by Vatsalya from India.**

| number | service | confirmed |
|---|---|---|
| **14416** | Tele-MANAS, Government of India | connects, free, 24x7, multilingual |
| **9820466726** | AASRA | connects, 24x7, confidential |
| **9999666555** | Vandrevala Foundation | connects, free, 24x7, phone and WhatsApp |

All three are now in `crisis.en.md` and `crisis.hi.md`, **identical in both** —
`test_both_languages_offer_the_same_numbers` fails if they drift, because the
likeliest version of that is somebody updating one file and leaving the Hindi
reader, who is likelier to need an Indian line, on the stale list.

The directory stays underneath them. A local number is the better answer for
someone in distress; the directory covers everyone this product does not.

### Where the verification marker lives, and why you will not see it

`verified:` is an HTML comment inside each response file. It has to be in the file —
`test_no_unverified_phone_number_creeps_back` reads the raw bytes and fails on a
phone number with no dial date beside it, which is what stops an un-dialled number
being pasted in.

`load_crisis_response` strips comments before returning. Without that, somebody in
crisis would receive a note about re-verification schedules underneath their helpline
numbers. Two tests hold the pair together: one asserts the marker is in the file,
the other asserts it is not in what gets sent.

**Re-verify this.** A directory can change what it lists for a country, and a
helpline can lose funding, without a single byte changing in this repository. This
log entry has a date on it for that reason. Treat it as stale after roughly six
months, or immediately if anyone reports the link not resolving.
