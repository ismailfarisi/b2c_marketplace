import SwiftUI

/// Start date as a picker that cannot go before the first day still open.
struct StartDatePicker: View {
    let date: String
    let earliest: String
    let onPick: (String) -> Void

    private static let dubai = TimeZone(identifier: "Asia/Dubai") ?? .current
    private static let iso: DateFormatter = {
        let f = DateFormatter()
        f.calendar = Calendar(identifier: .gregorian)
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = dubai
        f.dateFormat = "yyyy-MM-dd"
        return f
    }()

    var body: some View {
        let min = Self.iso.date(from: earliest) ?? Date()
        let selection = Binding<Date>(
            get: { Self.iso.date(from: date).map { max($0, min) } ?? min },
            set: { onPick(Self.iso.string(from: $0)) }
        )
        DatePicker(NSLocalizedString("meals_start", comment: ""), selection: selection, in: min..., displayedComponents: .date)
            .labelsHidden()
            .datePickerStyle(.compact)
            .environment(\.timeZone, Self.dubai)
            .tint(Warm.chilli)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Space.edge)
    }
}

func startReason(_ code: String?) -> String? {
    switch code {
    case "too_soon": return NSLocalizedString("meals_start_too_soon", comment: "")
    case "in_the_past": return NSLocalizedString("meals_start_past", comment: "")
    default: return nil
    }
}
