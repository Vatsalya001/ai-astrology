# Golden chart fixtures

Empty until Phase 2, when the ephemeris exists to generate them.

Each of the twelve synthetic profiles in `tests/fixtures/charts/profiles.json` gains an
`expected_d1.json` and `expected_dasha.json` here. Those files then become the contract:
the Phase 2 gate requires all of them to match **exactly**.

**Cross-validate against an independent reference before freezing any of them.** A
golden file that encodes your own bug makes that bug permanent — strictly worse than
having no test, because it converts a bug into an assertion.

At least five of the twelve must be checked against a published ephemeris table or a
second implementation. The ones most likely to be got wrong are 003 (1943 Kolkata,
wartime UTC+06:30) and 009 (1901 Kolkata, pre-IST local mean time).
