# Mobile — Kotlin Multiplatform

The customer app. Replaced the Expo scaffold that was in `apps/mobile`, which is deleted.

```
shared/       Kotlin. Networking, paging, event batching, playback policy, state.
androidApp/   Compose + Media3/ExoPlayer.
iosApp/       SwiftUI + AVFoundation.
```

---

## 1. Why KMM, and what that decides

The app is a video feed. Playback performance is the product, which rules out a
JS bridge and rules out drawing video through a cross-platform UI toolkit.

But "native for performance" is usually taken to mean *everything* is written
twice, and that is the expensive misreading. The split this project takes:

| | Where | Why |
|---|---|---|
| Decoder, surface, list | **Platform** | This is the performance. ExoPlayer and AVPlayer have nothing in common and pretending otherwise costs frames. |
| **Which** rendition, **how many** to prefetch, what counts as a view, when to page, what to batch | **Shared** | Every one of these is attached to a number in the plan. Two implementations drift, and the drift shows up as a cloud bill or a corrupted ranking signal months later. |

`shared/playback/PlaybackPolicy.kt` is the clearest case. PLAN-VIDEO-FIRST §9
ranks *"cost per view with no conversion"* as the second-most-serious risk in the
whole plan and prescribes a mitigation in six words: **preload ≤2 ahead, cap
720p.** That mitigation is a constant and a function, tested once, and both
platforms are bound by it.

The Expo scaffold that was in `apps/mobile` is **deleted**, and with it the
`@marketplace/mobile` npm workspace. `packages/api-client` is now a web-only
package — see §4 for what that does to the OpenAPI contract.

---

## 2. Running it

Prerequisites: JDK 17+, Android SDK with platform 36. Xcode 15+ for iOS.

```bash
cd apps/mobile-kmm

./gradlew :shared:testDebugUnitTest      # the shared logic — 38 tests
./gradlew :androidApp:assembleDebug      # APK
./gradlew :shared:linkDebugFrameworkIosSimulatorArm64   # macOS only
```

The Android app points at `http://10.0.2.2:8080/v1` — the emulator's route to the
host, where `make dev` runs `cmd/api`. Cleartext is permitted for that host and
`localhost` only, through `network_security_config.xml`; a blanket
`usesCleartextTraffic` in the manifest would have shipped to release.

**iOS has not been compiled.** Kotlin/Native's iOS targets require a macOS host,
so on Windows those Gradle tasks do not run at all. `iosMain` and `iosApp/` are
written against the documented APIs and are unverified — treat the first
`linkDebugFrameworkIosArm64` on a Mac as the real review.

---

## 3. What is in `shared`

| File | What it decides |
|---|---|
| `playback/PlaybackPolicy.kt` | Rendition ceiling, metered downgrade, prefetch window, view threshold, completion threshold |
| `playback/PlaybackCoordinator.kt` | Which reel plays, which are warmed, what is reported and how it is labelled |
| `playback/ReelPlayer.kt` | The seven-method player interface, and the pool that bounds decoders |
| `feed/FeedPager.kt` | Opaque cursor paging, stale-cursor recovery, exhaustion, de-duplication |
| `events/EventQueue.kt` | Batching, flush triggers, bounded buffer, retry-preserving-order |
| `feed/FeedViewModel.kt` | State, optimistic likes, CTA clicks — observed by both Compose and SwiftUI |
| `network/ApiClient.kt` | One client, one error type, Ktor-managed token refresh |

The player interface is seven methods and that is the load-bearing constraint.
No frame callbacks, no surface handle, no codec configuration, no buffer tuning —
those belong to the platform, and exposing them would mean shared code acquiring
opinions about decoding it cannot hold correctly for both. **If it ever needs an
eighth method, that is worth a conversation rather than a commit.**

---

## 4. The OpenAPI contract

`shared/model/` is hand-written from `api/openapi.yaml`, the same trade
`apps/web/src/lib/api.ts` makes — and the consequence is larger here.

With mobile on Kotlin, `packages/api-client` (TypeScript) can never be the mobile
client. **`api/openapi.yaml` stops being a description of the contract and
becomes the contract**, the only artefact both clients generate from. Drift is no
longer caught by a shared TypeScript type; it is caught by a runtime
deserialisation failure on whichever platform noticed first.

So the generator is not optional polish:

```
make gen-kotlin   # openapi-generator, kotlin + multiplatform library → shared/build/generated
```

Neither `make gen-ts` nor `make gen-kotlin` exists yet. Until they do,
`shared/model/Feed.kt` and `Events.kt` are copies that can drift, which is why
they are two short files rather than a package. When the generator lands they are
deleted, not migrated.

---

## 5. Things the tests are actually protecting

Not coverage — these are the specific ways a video feed goes wrong.

- **`prefetches_exactly_two_ahead`** — the constant is a direct multiplier on
  egress and is exactly the kind of thing raised to "make scrolling smoother"
  without seeing the bill.
- **`never_plays_above_the_cap`** — if the server ever starts producing 1080p, an
  old build must not opt itself into double the egress. An unrecognised rendition
  label ranks *above* the ladder and is filtered out.
- **`the_pool_is_bounded_however_far_you_scroll`** — hardware decoders are
  genuinely scarce on Android; exhausting them throws rather than degrades.
- **`a_short_look_is_reported_as_a_skip`** — Phase 4 weights `skip` at −1.5.
  Reporting everything as a view deletes the strongest negative signal the ranker
  has.
- **`watch_time_survives_a_fast_scroll`** — the outgoing reel must be closed out
  before its player is recycled, or fast scrolling silently loses watch time on
  exactly the reels people flick past.
- **`completion_uses_the_player_position_not_wall_clock`** — a viewer who leaves
  the app open on a reel for a minute has not watched it four times.
- **`a_failed_flush_keeps_the_events`** — ingest is idempotent, so re-sending is
  safe and dropping is not. Dropping would under-report on precisely the
  connections where engagement is already worst.
- **`an_exhausted_feed_stays_exhausted`** — PLAN-VIDEO-FIRST §10 promises a feed
  that *can* be exhausted. A client that looped back to the top would quietly
  rewrite the product promise.

Two of these were written before the code and failed first; one of them found a
real bug in the tests themselves (§6).

---

## 6. Two things the build found

**`actual typealias PlatformContext = Context` does not compile.** It is the
pattern most KMM tutorials show. `Context` is abstract and an `expect class` is
final, so the modalities disagree. Widening the expect to `abstract` fixes
Android and leaves iOS with a class nobody can instantiate. The wrapper class
costs one constructor call at launch.

**A test built its own `HttpClient` and therefore tested nothing.** The
stale-cursor recovery in `FeedPager` is written against `ApiException`, which is
produced by the `HttpResponseValidator` installed in `ApiClientFactory`. A
hand-built client in the test had no validator, so failures arrived as Ktor's
`ClientRequestException` and the recovery path never ran — the test passed while
exercising a client that could not produce the error it recovers from.
`ApiClientFactory.create` now takes an optional engine so tests use the real
configuration.

---

## 7. Not built

1. **Location permission.** Both apps hard-code Dubai Marina. `lat`/`lng` are
   required by the API — *"a feed without a location is a different product"* —
   so a fixed point is more honest than a silent nationwide feed, but it is the
   first thing to wire.
2. **Sign-in.** The token store, refresh and the anonymous → signed-in identity
   split all exist; there is no OTP screen. Anonymous browsing is a supported
   mode (`GET /feed` has `security: []`), so the app works without it.
3. **Poster / LQIP rendering.** `FeedItem.lqip` is carried and unused. It is what
   makes the feed paint before any network fetch resolves — the difference
   between a scroll that feels instant and one that flashes black.
4. **Comments, saves, follows, the CTA destination screens.** The feed reports
   the click and records the funnel; it does not yet navigate anywhere.
5. **iOS compilation** (§2).
6. **`iosApp.xcodeproj`.** The Swift sources are here; the project file is
   generated on a Mac (`open iosApp` after `./gradlew :shared:embedAndSignAppleFrameworkForXcode`)
   rather than hand-written into the repo, where it would be a 400-line XML file
   nobody can review.
