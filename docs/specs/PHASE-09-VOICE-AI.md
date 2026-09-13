# Phase 9 — Voice AI (Python)

| | |
|---|---|
| **Goal** | Let a user *speak* to their AI astrologer, in Hindi or English, and be spoken back to. |
| **Deliverable** | Full-duplex voice conversation reusing the Phase 5/6 pipeline, with streaming STT, streaming TTS, barge-in and voice usage limits. |
| **Depends on** | Phase 6 (Phase 7 required for usage limits) |
| **Unlocks** | — (parallel with Phase 10) |
| **Estimated size** | 12–18 days |
| **Cost to run** | ₹0 in development — `faster-whisper` and Piper both run locally and free |

> Voice is not a UI skin on chat. It is a latency problem. A 4-second pause before the
> AI speaks feels broken in a way the identical pause in text does not. Every design
> decision here is about getting to first audio quickly.

**Python carries this phase almost entirely**, and that is the clearest single
vindication of the polyglot stack: `faster-whisper`, `silero-vad` and Piper are all
Python-native, free, and run locally. Doing this from Go would mean shelling out to
subprocesses or writing cgo bindings.

---

## 1. Scope

### In scope
- Streaming speech-to-text with voice activity detection
- Streaming text-to-speech with sentence chunking
- Barge-in (user interrupts the AI)
- Voice sessions reuse the same conversation, memory, chart context and safety pipeline
- Hindi, English and Hinglish code-switching
- Voice-specific prompt adjustments (spoken ≠ written language)
- Transcript persistence (Go)
- Voice usage limits and metering (Go)
- Accessibility: captions, transcript view, text fallback

### Out of scope
- Voice cloning or custom voices (licence and consent complexity; not worth it here)
- Voice consultations with human astrologers — that is Phase 8's LiveKit path
- Wake words

---

## 2. Architecture

```
Mic ──► VAD (browser) ──► Opus frames
                              │  WebSocket
                              ▼
                  api-service (Go) — thin binary proxy
                   auth · quota check · metering · transcript persistence
                              │  internal WebSocket
                              ▼
                  ai-service (Python)
                              │
                       faster-whisper (streaming)
                              │
                    partial transcript ──► back through Go ──► live captions
                              │
                     final transcript on endpoint
                              │
              ┌───────────────▼───────────────┐
              │  Phase 5/6 pipeline           │
              │  intent · chart context ·     │
              │  memory · RAG · safety        │
              └───────────────┬───────────────┘
                              │
                    LLM streaming tokens
                              │
                  sentence chunker (. ? ! ।)
                              │
                        Piper TTS per sentence
                              │
                    audio frames ──► Go ──► speaker
                              │
       barge-in: user speaks → cancel TTS queue + abort LLM
```

**Go proxies the audio rather than exposing Python publicly.** That is a deliberate
tradeoff: it keeps `ai-service` off the internet (the Phase 0 invariant), and Go is
genuinely good at forwarding bytes between two WebSockets. The added latency on a local
network is ~1–2 ms — measure it and confirm, but it is not where your latency budget
goes.

**Sentence chunking is the whole latency trick.** Don't wait for the complete response
before synthesising. As soon as the first sentence is complete, send it to TTS and start
playing. The user hears audio while the model is still generating the rest.

Latency budget to first audio:

| Stage | Target |
|---|---|
| VAD endpoint detection | 300 ms |
| STT final transcript | 200 ms |
| Go proxy hop (×2) | 5 ms |
| Context build (cached chart) | 50 ms |
| LLM first sentence | 800 ms |
| TTS first chunk | 300 ms |
| **Total to first audio** | **~1.65 s** |

Under 2 seconds feels like conversation. Over 3 feels like a slow machine. Instrument
every stage separately so you know which one regressed.

---

## 3. Free local stack

### Speech-to-text — `faster-whisper`

Whisper reimplemented on CTranslate2: ~4× faster than the reference, runs on CPU,
handles Hindi and Hinglish code-switching genuinely well, and is free.

```python
from faster_whisper import WhisperModel

model = WhisperModel("small", device="cpu", compute_type="int8")
# "small" ≈ 460 MB, good Hindi/English. "medium" is better and ~3× slower.

segments, info = model.transcribe(audio, language=None, vad_filter=True)
# language=None → auto-detect, which is what makes Hinglish work
```

| Model | Size | Speed (CPU) | Hindi quality |
|---|---|---|---|
| `tiny` | 75 MB | very fast | poor |
| `base` | 142 MB | fast | usable |
| `small` | 460 MB | moderate | **good — start here** |
| `medium` | 1.5 GB | slow on CPU | very good |
| `large-v3` | 3 GB | GPU only | excellent |

VAD via `silero-vad` (free, MIT) to detect speech start and end. Endpoint on 700 ms of
silence — shorter cuts people off mid-thought, longer feels sluggish.

**Load the model once at startup**, in the FastAPI `lifespan`, not per request.
Model loading is seconds; inference is milliseconds. Run STT in a thread pool so it
does not block the event loop — this is the one place in `ai-service` where the GIL
genuinely matters, and `asyncio.to_thread` is the fix.

### Text-to-speech — Piper

Fast, local, free (MIT), with Hindi and Indian-English voices. Runs faster than
real-time on a modest CPU.

```bash
echo "आपका वर्तमान दशा काल..." | piper --model hi_IN-pratham-medium.onnx --output_raw
```

Alternatives worth knowing: **Kokoro TTS** (Apache-2.0, higher quality, heavier) and
**Coqui XTTS** — but check XTTS's licence carefully, as its terms restrict commercial
use. Piper is the safe free default.

⚠️ `edge-tts` and similar wrappers around a browser vendor's internal TTS endpoint are
free and tempting. They are also outside those services' terms of use. Don't build a
product feature on one.

### Production options

| Need | Option | Notes |
|---|---|---|
| STT | Deepgram, Google STT, AWS Transcribe | Streaming, low latency |
| STT (Indic) | Sarvam AI, AI4Bharat | Purpose-built for Indian languages and code-switching |
| TTS | ElevenLabs, Google, Azure | Highest quality |
| TTS (Indic) | Sarvam AI | Strong Hindi and regional voices |

Same pattern as Phase 4 — `STTProvider` and `TTSProvider` protocols, local free
implementations in dev, hosted in production, **and the PII guard extended to cover
them**:

```python
def assert_audio_provider_allowed(provider, env: str) -> None:
    if env == "production" and provider.tier != "paid":
        raise RuntimeError(
            f"Refusing to start: audio provider {provider.id} is tier {provider.tier}. "
            "Voice audio is biometric-adjacent data and must not reach a free tier."
        )
```

---

## 4. Voice-specific prompt changes

A written answer read aloud sounds wrong. The voice persona needs its own prompt module
(`personas/voice_*.v1.md`) with different instructions:

| Written | Spoken |
|---|---|
| Markdown headings, bullets, bold | None — they're unspeakable |
| 250–400 words | 60–120 words |
| "Your 10th house (Capricorn) contains…" | "Your tenth house — that's Capricorn — has…" |
| Lists of five points | Two or three, then "shall I go on?" |
| "Saturn at 19°47' in Aquarius" | "Saturn in Aquarius, about twenty degrees" |
| Ends with a summary | Ends with a question |

Degrees, symbols and abbreviations must be expanded before synthesis. A normaliser runs
between the LLM and TTS: `19°47'` → "nineteen degrees forty-seven minutes", `10th` →
"tenth", `₹199` → "one hundred ninety-nine rupees". Hindi needs its own normaliser —
numbers and ordinals do not transliterate.

Same facts, same safety rules, same chart context — only register and length change. The
eval harness gets a voice suite asserting exactly that: given identical input, the
*facts* in the voice response match the facts in the text response.

---

## 5. Barge-in

Non-negotiable for anything that feels like conversation. If the user starts speaking,
the AI stops immediately.

```
VAD detects speech while TTS is playing
  → stop audio playback within 100 ms (client-side, immediately)
  → send interrupt frame → Go → Python
  → Python: cancel the TTS task group, abort the in-flight LLM stream
  → Go: persist the partial assistant message, marked interrupted
  → begin capturing the new utterance
```

In Python this is `asyncio.TaskGroup` cancellation; in Go it is context cancellation on
the proxy. Both propagate, so an interrupt stops token generation rather than merely
muting audio that is still being paid for.

Persisting the partial matters: the conversation history should reflect what was
actually said, so the next turn's context is accurate. An interrupted response that
vanishes leaves the model confused about what it already covered.

Guard against the AI's own audio triggering VAD — echo cancellation
(`getUserMedia` with `echoCancellation: true`) plus a short suppression window keyed to
playback.

---

## 6. Voice UI

```
┌──────────────────────────────────────────┐
│  ←  Voice                    ⋮           │
│                                          │
│              ╭─────────╮                 │
│            ╭─┤    ✦    ├─╮               │
│          ╭─┤ ╰─────────╯ ├─╮             │
│          │ │   speaking  │ │             │
│          ╰─┤             ├─╯             │
│            ╰─────────────╯               │
│                                          │
│   "Your current period is traditionally  │
│    associated with steady growth in      │
│    your professional life…"              │
│                                          │
│   ⓘ Based on: 10th house · Saturn ·      │
│     Jupiter dasha                        │
│                                          │
│  ┌────────────────────────────────────┐  │
│  │  ⏸ Tap to interrupt                │  │
│  └────────────────────────────────────┘  │
│                                          │
│   [ 🎤 ]     [ ⌨ Type ]    [ 📄 ]        │
│                                          │
│  4:12 of 15:00 monthly voice remaining   │
└──────────────────────────────────────────┘
```

### States, each visually distinct

| State | Visual |
|---|---|
| Idle | Calm pulsing orb |
| Listening | Orb reacts to input amplitude; live partial transcript below |
| Thinking | Orbiting particles; "consulting your chart…" |
| Speaking | Waveform synced to audio; live captions |
| Interrupted | Brief acknowledgement, straight to listening |
| Error | "I couldn't hear that" + retry + type fallback |

### Accessibility — required, not optional

- **Live captions on by default.** A voice-only interface excludes deaf and hard-of-hearing users entirely.
- Full transcript view, scrollable during the session
- Text input always available as an equal alternative, never a hidden fallback
- All controls keyboard-reachable and screen-reader labelled
- Haptic feedback on state changes for users who can't see the orb
- Respect `prefers-reduced-motion` on the orb animation

---

## 7. Data model additions (Go)

```sql
CREATE TABLE voice_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    conversation_id  UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    ended_at         TIMESTAMPTZ,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    billed_seconds   INTEGER NOT NULL DEFAULT 0,
    language         TEXT,
    stt_provider     TEXT NOT NULL,
    tts_provider     TEXT NOT NULL,
    turn_count       INTEGER NOT NULL DEFAULT 0,
    interrupt_count  INTEGER NOT NULL DEFAULT 0,
    avg_latency_ms   INTEGER
);
CREATE INDEX vs_user_idx ON voice_sessions (user_id, started_at DESC);
```

Voice turns are stored as normal `messages` rows with `metadata.modality = 'voice'`, so
history, memory extraction and summarisation all work unchanged. That is the point of
reusing the pipeline rather than building a parallel one.

**Audio is not retained by default.** Transcripts are enough for the product to work.
Store audio only with explicit opt-in consent, a short retention window and a clear
purpose — voice recordings are among the most sensitive data you could hold, and they
are not needed for any feature here.

---

## 8. Usage limits

Voice costs more than text — more turns, plus STT and TTS. Meter in **seconds of
session**, not messages, using the Phase 7 machinery.

| Plan | Voice allowance |
|---|---|
| Free | Not available |
| Plus | 15 min/month |
| Pro | 120 min/month |
| Credits | 5 credits/minute |

Pre-check before starting, tick every 30 s from a Go goroutine (the same pattern as
Phase 8 consultations), warn at 2 minutes remaining, end gracefully at zero. Show
remaining time persistently in the UI.

---

## 9. Environment variables added

### `ai-service`
```bash
STT_PROVIDER=faster-whisper           # faster-whisper | deepgram | google | sarvam
STT_PROVIDER_TIER=local
STT_MODEL=small
STT_DEVICE=cpu
STT_COMPUTE_TYPE=int8
STT_LANGUAGE=auto
VAD_SILENCE_MS=700
VAD_MIN_SPEECH_MS=250

TTS_PROVIDER=piper                    # piper | kokoro | elevenlabs | google | sarvam
TTS_PROVIDER_TIER=local
TTS_VOICE_EN=en_IN-female-medium
TTS_VOICE_HI=hi_IN-pratham-medium
TTS_SPEED=1.0

VOICE_BARGE_IN_ENABLED=true
```

### `api-service`
```bash
FEATURE_VOICE_ENABLED=true
VOICE_MAX_SESSION=30m
VOICE_STORE_AUDIO=false               # opt-in only, never default
VOICE_CAPTIONS_DEFAULT=true
VOICE_BILLING_TICK=30s
```

---

## 10. Task list

| # | Service | Task | Done when |
|---|---|---|---|
| 9.1 | Py | `STTProvider` / `TTSProvider` protocols + PII guard extension | Mirrors the Phase 4 pattern; guard blocks free tiers in prod |
| 9.2 | Py | `faster-whisper` loaded in `lifespan`, run via `asyncio.to_thread` | Transcribes Hindi and English locally without blocking the loop |
| 9.3 | Py | Piper integration, streaming raw audio out | Speaks Hindi and English locally |
| 9.4 | Web | Silero VAD client-side + endpointing | Endpoints at 700 ms silence |
| 9.5 | Go | Binary WebSocket proxy with auth, quota, metering | Proxy hop measured < 5 ms |
| 9.6 | Py | Streaming STT with partial transcripts | Captions update live |
| 9.7 | Py | Sentence chunker between LLM and TTS | First audio < 2 s locally |
| 9.8 | Web | Audio playback queue, gapless sentences | No audible seams |
| 9.9 | All | Barge-in: stop, flush, cancel task group, abort LLM, persist partial | Interrupt within 100 ms |
| 9.10 | Web | Echo suppression | AI audio does not self-trigger VAD |
| 9.11 | Py | Text normaliser for TTS, en + hi | Degrees, ordinals, currency spoken correctly |
| 9.12 | Py | Voice persona prompt modules | Voice eval suite passes |
| 9.13 | Web | Voice UI: orb, states, captions, transcript, text fallback | |
| 9.14 | Web | Accessibility: captions on, keyboard, screen reader, haptics | |
| 9.15 | Go | `voice_sessions` + metering + limits + graceful end | |
| 9.16 | Py | Language auto-detection incl. Hinglish | |
| 9.17 | All | Latency instrumentation per stage, propagated by trace ID | Dashboard shows each stage separately |
| 9.18 | Py | Voice eval suite | Facts match text responses for identical input |

---

## 11. Testing

**Latency** — measure and assert each stage. Regression test at p95 against a fixed set
of recorded utterances. Latency regressions are invisible until a user complains.

**Accuracy** — ~50 recorded utterances (synthetic or consented, in Hindi, English and
Hinglish) with reference transcripts. Measure word error rate. Free to run against the
local model.

**Concurrency (Py)** — STT under load does not block the event loop; a second voice
session starting while the first is transcribing is not stalled. This is the GIL risk
and it needs a real test, not an assumption.

**Barge-in** — interrupt at various points; assert playback stops, the queue flushes,
the LLM aborts (token generation actually ceases, verified by provider call count), and
the partial message persists with the correct content.

**Voice eval suite** — the same questions as the text eval set, through voice. Assert
the facts match, the safety rules hold, and responses stay under the length budget.

**Accessibility** — captions on by default; full flow completable with text input alone;
screen-reader pass on every state.

**E2E** — `open voice → speak a career question → hear a grounded answer → interrupt →
ask a follow-up → end → transcript in history → memory extracted`.

---

## 12. Security checklist

- [ ] Audio not stored unless explicitly opted in; short retention when it is
- [ ] **Voice data never sent to a free-tier hosted provider** — the PII guard extended to STT/TTS
- [ ] `ai-service` remains internal; the audio socket is proxied by Go, not exposed
- [ ] Microphone permission requested in context, with a clear explanation
- [ ] Visible recording indicator whenever the mic is live
- [ ] Audio WebSocket authenticated and encrypted (WSS)
- [ ] Transcripts inherit conversation access controls
- [ ] Voice sessions metered server-side; client duration never trusted
- [ ] Same safety pipeline as text — crisis short-circuit verified in the voice path
- [ ] No PII spoken aloud that the user didn't provide
- [ ] Account deletion removes voice sessions, transcripts and any stored audio

---

## 13. Risks

| Risk | Mitigation |
|---|---|
| Latency makes it feel broken | Sentence chunking, per-stage instrumentation, hard p95 budgets |
| Hindi/Hinglish STT accuracy | `small` minimum; evaluate Sarvam AI for production Indic accuracy |
| Python GIL blocking under concurrent voice sessions | STT in `asyncio.to_thread`; concurrency test asserts no stall; scale `ai-service` replicas horizontally |
| Local STT too slow on dev hardware | `base` model for dev, `small`+ in CI; hosted provider as a documented fallback |
| Go proxy adds meaningful latency | Measure it; budget is 5 ms for both hops. If it exceeds that, the proxy implementation is wrong, not the architecture. |
| Barge-in echo loops | Echo cancellation plus a playback-keyed suppression window |
| Voice costs blow out unit economics | Metered in seconds, plan-limited, warned and ended gracefully |
| Voice excludes deaf users | Captions on by default; text input is an equal path, not a fallback |
| Audio retention creates liability | Off by default; the product works fine on transcripts alone |

---

## 14. Definition of Done

Global DoD **plus**:

- [ ] Time to first audio < 2 s locally, < 2.5 s p95 in production
- [ ] Barge-in interrupts within 100 ms and actually stops token generation
- [ ] Hindi, English and Hinglish all work
- [ ] Voice responses carry the same facts and safety posture as text
- [ ] Captions on by default; text input always available
- [ ] Entire stack runs locally on `faster-whisper` + Piper at zero cost
- [ ] Metering accurate; limits enforced with graceful termination

---

## 15. Phase Gate 🔒

- [ ] Streaming STT with live partial transcripts working locally
- [ ] Streaming TTS with sentence chunking; audio starts before generation completes
- [ ] Time to first audio < 2 s locally, instrumented per stage
- [ ] Go audio proxy measured; both hops under 5 ms
- [ ] **Barge-in stops playback, cancels the TTS group, aborts the LLM, persists the partial**
- [ ] Echo suppression prevents self-triggering
- [ ] Hindi, English and Hinglish transcribed and spoken correctly
- [ ] Text normaliser handles degrees, ordinals and currency in both languages
- [ ] Voice persona keeps responses short and unformatted
- [ ] **Voice eval suite: facts identical to text for the same inputs**
- [ ] **Concurrent voice sessions do not block the Python event loop (tested)**
- [ ] Crisis short-circuit verified in the voice path
- [ ] PII guard blocks free-tier STT/TTS in production
- [ ] Captions on by default; full flow completable via text alone
- [ ] Screen-reader pass on every voice state
- [ ] Metering accurate; limits warn and end gracefully
- [ ] Audio not stored by default; opt-in path tested
- [ ] Account deletion removes sessions, transcripts and audio
- [ ] `task verify` and `task eval` green
- [ ] `docs/PROJECT_STATUS.md` and `.claude/state/current-phase.md` updated
