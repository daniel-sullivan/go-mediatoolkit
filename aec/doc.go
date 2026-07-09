// Package aec provides realtime acoustic echo cancellation (AEC) for
// go-mediatoolkit: removing a known far-end (loudspeaker) signal
// from a near-end (microphone) capture, tolerating the wide and
// drifting far-end↔mic latency real-world audio rigs exhibit (device
// buffers, Bluetooth, OS audio stacks: anywhere from ~20ms to over
// 1s), as a complement to the toolkit's VAD-based ducking.
//
// # Status
//
// Complete: internal/aec3 is a full 1:1 Go port of WebRTC's AEC3 (see
// internal/aec3's own file-by-file doc comments for what's ported vs.
// deliberately dropped), and Canceller (canceller.go) is the public Go
// API in front of it. See internal/parity_tests for the per-component
// bit-exact parity slices proving the port against a fetched (not
// vendored) C++ oracle build of the real AEC3 implementation.
//
// # A two-stream processor
//
// Unlike every other Processor in this toolkit (which transforms one
// signal in place — see mutations.Processor), an echo canceller
// fundamentally needs *two* signals: the render (far-end) signal that
// went to the loudspeaker, and the capture (near-end) signal recorded
// by the microphone, which contains that same signal reflected back
// acoustically, delayed and distorted, mixed with whatever the near
// end actually wants to say. The public API reflects that directly:
//
//   - FeedFarEnd(samples) — the render path: call this with whatever
//     is being (or about to be) played out of the speaker, e.g. tapped
//     from the mixer's master output or a device's render buffer.
//   - Process(samples) — the capture path: removes the estimated echo
//     from samples in place, implementing mutations.Processor so it
//     composes with the rest of the toolkit's streaming effect chain
//     (timeline.EffectSource, mixer hooks) exactly like any other
//     Processor once wired up.
//
// Unlike an earlier draft of this design, the two calls are NOT
// independent: FeedFarEnd and Process must be externally serialized
// onto a single goroutine (never called concurrently with each other),
// a hard requirement of the underlying AEC3 port — see Canceller's own
// doc comment for the full concurrency contract, including the two
// calls (SetAudioBufferDelay, Metrics) that ARE safe from any
// goroutine at any time.
//
// # Tuning
//
// CancellerConfig.Tuning exposes AEC3's full internal tuning surface
// (filter length/rate, suppressor aggressiveness, echo audibility
// thresholds, ...) via the public github.com/daniel-sullivan/go-mediatoolkit/aec/config
// package: nil selects config.DefaultConfig(), upstream's own
// defaults and this package's only behaviour before Tuning existed;
// a non-nil *config.Config is validated against config.Validate's
// range rules and rejected with an error wrapping ErrBadArg if it
// would need clamping. See config's package doc for the full field
// set (mirroring EchoCanceller3Config field-for-field) and the
// README's Tuning section for a worked example.
//
// # Implementation
//
// A 1:1 port of WebRTC's AEC3 (github.com freedesktop
// webrtc-audio-processing v2.1, tracking WebRTC M131), chosen over
// simpler alternatives (e.g. Speex MDF) specifically because AEC3's
// built-in continuously-adapting delay estimation and nonlinear
// residual-echo suppression are needed to meet the wide/drifting
// latency requirement above — see the governing plan doc for the full
// options analysis. Verification follows this toolkit's usual
// bit-exact port discipline: per-component parity slices against a
// fetched (not vendored — see aec/oracle/VERSION) C++ oracle build of
// the real AEC3 implementation, gated behind opt-in build tags and
// mise tasks, entirely excluded from default CI and from
// CGO_ENABLED=0 builds.
//
// # Licensing note
//
// AEC3 is BSD-3-Clause, but WebRTC additionally ships a PATENTS grant
// file granting a license to Google's related patents conditioned on
// not asserting patent claims against WebRTC implementations — the
// same shape as libfvad's grant already accepted for this toolkit's
// VAD package. This applies to the opt-in C++ parity oracle (fetched
// into a local cache, never vendored or compiled into a default
// build) and to the aec package's own AEC3-derived Go code alike; see
// the root LICENSING.md for the full statement and file-by-file map.
package aec
