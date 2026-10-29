import SwiftUI
import Shared

/// Everything drawn over the video.
///
/// The mirror of `ReelOverlay.kt`, and mirror is the right word: both files draw
/// the same skeleton — bar, badge, time pill, two lines, a price, one action —
/// from the same `ReelClock.Face` computed in `:shared`. Neither of them decides
/// whether a bar drains or fills, or what "now" is on a device with a wrong
/// clock. Those were the two places the platforms would have quietly disagreed,
/// so they are answered once and tested once.
///
/// There is no `switch item.subjectType` in this file. The moment one exists,
/// adding a vertical means shipping a mobile release.
struct ReelOverlay: View {
    let item: FeedItem
    /// Server clock minus device clock in milliseconds, sampled once when the
    /// page landed. See `ReelClock.skew` for why re-sampling it per tick freezes
    /// every countdown.
    ///
    /// Milliseconds rather than a `Duration` because `kotlin.time.Duration` is an
    /// inline value class and inline value classes are not exported to
    /// Objective-C — a Duration-typed parameter would simply be missing from the
    /// generated framework. The shared API is shaped around that.
    let skewMillis: Int64
    /// Space reserved for the bottom navigation bar, which RootView draws over
    /// this overlay. Same reasoning as the tab row on the other edge: the two are
    /// laid out independently and nothing else makes them agree — without it the
    /// price and the CTA sit underneath "Sell".
    var chromeBottom: CGFloat = 0
    let onAction: (ResolvedAction) -> Void

    private static let accent = Color(red: 0.706, green: 0.396, blue: 0.165)

    var body: some View {
        VStack(spacing: 0) {
            TimeBar(signal: item.timeSignal, skewMillis: skewMillis)

            HStack {
                if let badge = item.summary?.badge, !badge.isEmpty {
                    Pill(text: badge, filled: true)
                }
                Spacer()
                TimePill(signal: item.timeSignal, skewMillis: skewMillis)
            }
            .padding(16)

            Spacer()

            VStack(alignment: .leading, spacing: 6) {
                ProductLine(item: item)
                ActionRow(actions: item.actions, onAction: onAction)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(16)
            .padding(.bottom, chromeBottom)
        }
    }
}

// MARK: - the clock

/// Re-resolves a signal on a timer, and only when there is something to animate.
///
/// A freshness signal changes once a day; ticking it every second would keep the
/// whole feed awake to redraw "listed 3d ago". So the timer runs for the two
/// modes whose text actually moves and the others resolve once.
@MainActor
private final class ClockTicker: ObservableObject {
    @Published private(set) var face: ReelClock.Face?

    private var timer: Timer?

    func start(signal: TimeSignal?, skewMillis: Int64) {
        timer?.invalidate()
        guard let signal else {
            face = nil
            return
        }

        resolve(signal, skewMillis)

        let ticking = signal.mode == ReelClock.shared.MODE_COUNTDOWN
            || signal.mode == ReelClock.shared.MODE_WINDOW
        guard ticking else { return }

        timer = Timer.scheduledTimer(withTimeInterval: 1.0, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.resolve(signal, skewMillis) }
        }
    }

    func stop() {
        timer?.invalidate()
        timer = nil
    }

    private func resolve(_ signal: TimeSignal, _ skewMillis: Int64) {
        let now = Int64(Date().timeIntervalSince1970 * 1000)
        face = ReelClock.shared.face(
            signal: signal,
            deviceNowMillis: now,
            skewMillis: skewMillis
        )
    }
}

/// The thin bar at the very top.
///
/// `fraction` arrives already oriented — a countdown falls, a window rises — so
/// nothing here knows which way round a mode goes.
private struct TimeBar: View {
    let signal: TimeSignal?
    let skewMillis: Int64

    @StateObject private var ticker = ClockTicker()

    var body: some View {
        GeometryReader { geo in
            ZStack(alignment: .leading) {
                Rectangle().fill(.white.opacity(0.22))
                if let face = ticker.face {
                    Rectangle()
                        .fill(Color(red: 0.706, green: 0.396, blue: 0.165))
                        .frame(width: geo.size.width * CGFloat(face.fraction))
                }
            }
        }
        .frame(height: 3)
        .opacity(signal == nil ? 0 : 1)
        .onAppear { ticker.start(signal: signal, skewMillis: skewMillis) }
        .onDisappear { ticker.stop() }
    }
}

private struct TimePill: View {
    let signal: TimeSignal?
    let skewMillis: Int64

    @StateObject private var ticker = ClockTicker()

    var body: some View {
        Group {
            if let face = ticker.face {
                Pill(text: ReelLabels.text(face.label), filled: false)
            }
        }
        .onAppear { ticker.start(signal: signal, skewMillis: skewMillis) }
        .onDisappear { ticker.stop() }
    }
}

/// Renders a resolved label.
///
/// The switch is over `ReelClock.Label`, a closed set of shapes, and never over
/// the mode string — so a mode this build has not heard of arrives as a nil face
/// and draws nothing, rather than falling into a default that guesses.
enum ReelLabels {
    static func text(_ label: ReelClockLabel) -> String {
        switch label {
        case let remaining as ReelClockLabelRemaining:
            if remaining.hours > 0 {
                return String(
                    format: NSLocalizedString("time_remaining_h", comment: ""),
                    Int(remaining.hours), Int(remaining.minutes)
                )
            }
            return String(
                format: NSLocalizedString("time_remaining_ms", comment: ""),
                Int(remaining.minutes), Int(remaining.seconds)
            )

        case let window as ReelClockLabelElapsedOf:
            let remaining = Int(window.remainingMinutes)
            return String(
                format: NSLocalizedString("time_window", comment: ""),
                remaining / 60,
                remaining % 60,
                Int(window.totalMinutes) / 60
            )

        case let ago as ReelClockLabelAgo:
            // Compared with `==` rather than matched with `case`. A Kotlin enum
            // crosses the Objective-C boundary as a class with static members,
            // not as a Swift enum, so pattern matching on it does not compile
            // and `allCases` does not exist.
            let key: String
            if ago.unit == ReelClockLabelUnit.minute { key = "ago_minutes" }
            else if ago.unit == ReelClockLabelUnit.hour { key = "ago_hours" }
            else if ago.unit == ReelClockLabelUnit.week { key = "ago_weeks" }
            else { key = "ago_days" }
            return String(format: NSLocalizedString(key, comment: ""), Int(ago.amount))

        case let by as ReelClockLabelBy:
            return String(
                format: NSLocalizedString("time_order_by", comment: ""),
                Int(by.hour), Int(by.minute)
            )

        default:
            return NSLocalizedString("time_closed", comment: "")
        }
    }
}

// MARK: - chrome

private struct Pill: View {
    let text: String
    let filled: Bool

    var body: some View {
        Text(text)
            .font(.system(size: 11, weight: .medium))
            .foregroundStyle(.white)
            .lineLimit(1)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .background {
                if filled {
                    Capsule().fill(.black.opacity(0.55))
                } else {
                    Capsule().stroke(.white.opacity(0.5), lineWidth: 1)
                }
            }
    }
}

/// Title, qualifier line, price.
///
/// Falls back to caption and author when a subject has no registered provider,
/// which is most reels today. That path is not degraded — it is what Phase 2
/// shipped and it still reads fine — so it gets the same layout rather than an
/// empty gap where a title would be.
private struct ProductLine: View {
    let item: FeedItem

    var body: some View {
        let summary = item.summary

        Text(titleText(summary))
            .foregroundStyle(.white)
            .font(.system(size: 16, weight: .semibold))
            .lineLimit(1)

        let line = (summary?.line).flatMap { $0.isEmpty ? nil : $0 } ?? item.caption
        if !line.isEmpty {
            Text(line)
                .foregroundStyle(.white.opacity(0.8))
                .font(.system(size: 13))
                .lineLimit(2)
        }

        // A nil currency means this subject has no price to show, which is not
        // the same as free — rendering it as 0 would put every car in the feed
        // at AED 0.00.
        if let summary, let currency = summary.currency {
            Text(Self.price(summary.priceMinor?.int64Value ?? 0, currency) + summary.priceSuffix)
                .foregroundStyle(Color(red: 0.706, green: 0.396, blue: 0.165))
                .font(.system(size: 18, weight: .semibold))
        }
    }

    private func titleText(_ summary: SubjectSummary?) -> String {
        if let title = summary?.title, !title.isEmpty { return title }
        return item.author.name.isEmpty ? "—" : item.author.name
    }

    /// Minor units to a displayable amount.
    ///
    /// Trailing `.00` dropped: every price in this catalogue is whole dirhams,
    /// and "AED 189,000.00" is four characters of noise on a line that has to be
    /// read in about a second.
    private static func price(_ minor: Int64, _ currency: String) -> String {
        let formatter = NumberFormatter()
        formatter.numberStyle = .decimal
        let major = minor / 100
        let cents = minor % 100
        let grouped = formatter.string(from: NSNumber(value: major)) ?? "\(major)"
        return cents == 0
            ? "\(currency) \(grouped)"
            : "\(currency) \(grouped).\(String(format: "%02d", cents))"
    }
}

private struct ActionRow: View {
    let actions: [ResolvedAction]
    let onAction: (ResolvedAction) -> Void

    var body: some View {
        HStack(spacing: 8) {
            ForEach(actions.sorted { $0.position < $1.position }, id: \.id) { action in
                VStack(alignment: .leading, spacing: 2) {
                    Button {
                        onAction(action)
                    } label: {
                        // Rendered exactly as the vertical's handler gave it.
                        Text(action.label)
                            .lineLimit(1)
                            .frame(maxWidth: .infinity)
                    }
                    .buttonStyle(.borderedProminent)
                    .tint(Color(red: 0.706, green: 0.396, blue: 0.165))
                    .disabled(!action.available)

                    // Always shown when a CTA is unavailable. A greyed-out
                    // button with no explanation reads as a broken app, and the
                    // vendor gets blamed for a platform decision.
                    if !action.available, let reason = action.reason {
                        Text(reason)
                            .foregroundStyle(.white.opacity(0.6))
                            .font(.caption2)
                    }
                }
            }
        }
        .padding(.top, 6)
    }
}
