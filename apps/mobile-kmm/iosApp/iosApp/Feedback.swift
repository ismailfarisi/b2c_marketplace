import SwiftUI
import Shared

/// Words for a `Failure`, and the three things a list can be other than a list.
///
/// The classification lives in `commonMain` and is tested there; only the words
/// are here, so an Arabic build translates them through `Localizable.strings`
/// without a view model changing.
///
/// SKIE turns the sealed `Failure` into a Swift enum, which is why this is an
/// exhaustive `switch` rather than a chain of `is` casts ending in a `default`
/// that silently swallows whatever gets added next.
extension Failure {
    var words: String {
        switch onEnum(of: self) {
        case .offline:
            return String(localized: "err_offline")
        case .timeout:
            return String(localized: "err_timeout")
        case .unauthorized:
            return String(localized: "err_unauthorized")
        case .forbidden:
            return String(localized: "err_forbidden")
        case .notFound:
            return String(localized: "err_not_found")
        case .rateLimited:
            return String(localized: "err_rate_limited")
        case .rejected(let rejected):
            // The server's own sentence when it wrote one for a human. A 422
            // from this API usually carries a detail better than anything a
            // client could invent — "This offer has ended", "Outside this
            // kitchen's delivery area" — and discarding it for "Invalid
            // request" would throw away the most useful string in the response.
            return rejected.detail ?? String(localized: "err_rejected")
        case .server:
            return String(localized: "err_server")
        case .unknown:
            return String(localized: "err_unknown")
        }
    }

    /// Whether offering Retry makes sense.
    ///
    /// Read from the shared extension rather than re-decided here: a button
    /// that cannot work teaches the viewer that tapping things in this app does
    /// nothing, and the two platforms must not disagree about which those are.
    var canRetry: Bool { FailureKt.isRetryable(self) }
}

/// A failed load, with a way out.
struct FailureBlock: View {
    let failure: Failure
    var onRetry: (() -> Void)?

    var body: some View {
        VStack(spacing: Space.large) {
            Text(failure.words)
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Palette.textMuted)
                .multilineTextAlignment(.center)

            if let onRetry, failure.canRetry {
                Button(action: onRetry) {
                    Text("action_retry")
                        .font(.system(size: TypeScale.secondary))
                        .foregroundStyle(Palette.text)
                        .padding(.horizontal, Space.xl)
                        .padding(.vertical, Space.medium)
                        .frame(minHeight: Space.touch)
                        .overlay(
                            RoundedRectangle(cornerRadius: 10)
                                .stroke(Palette.line, lineWidth: 1)
                        )
                }
            }

            // The request id, quietly, for a 500 only. It is the one thing that
            // makes a support conversation about a server error tractable.
            if case .server(let server) = onEnum(of: failure), let id = server.requestId {
                Text(id)
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.textFaint)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, Space.xxl)
        .padding(.vertical, Space.xxl)
    }
}

/// A failure that must not take the screen with it.
///
/// The right shape when a *refresh* fails: the list on screen is still the best
/// information available, and replacing it with an error destroys something
/// useful to report something the viewer can already see.
struct FailureStrip: View {
    let failure: Failure
    var onRetry: (() -> Void)?

    var body: some View {
        HStack(spacing: Space.medium) {
            Text(failure.words)
                .font(.system(size: TypeScale.caption))
                .foregroundStyle(Palette.textMuted)
                .frame(maxWidth: .infinity, alignment: .leading)

            if let onRetry, failure.canRetry {
                Button(action: onRetry) {
                    Text("action_retry")
                        .font(.system(size: TypeScale.caption).weight(.semibold))
                        .foregroundStyle(Palette.accent)
                        .frame(minHeight: Space.touch)
                }
            }
        }
        .padding(.horizontal, Space.edge)
        .padding(.vertical, Space.medium)
        .background(Palette.surface)
    }
}

/// Nothing here, and that is not an error.
///
/// `hint` says what would put something here. An empty state that only says
/// "Nothing saved" is a dead end; one that says how to save something is the
/// cheapest onboarding in the product.
struct EmptyBlock: View {
    let message: String
    var hint: String?

    var body: some View {
        VStack(spacing: Space.small) {
            Text(message)
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Palette.textMuted)
                .multilineTextAlignment(.center)
            if let hint {
                Text(hint)
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.textFaint)
                    .multilineTextAlignment(.center)
            }
        }
        .frame(maxWidth: .infinity)
        .padding(.horizontal, Space.xxl)
        .padding(.vertical, Space.xxl)
    }
}

struct LoadingBlock: View {
    var body: some View {
        ProgressView()
            .tint(Palette.accent)
            .frame(maxWidth: .infinity)
            .padding(.vertical, Space.xxl)
    }
}
