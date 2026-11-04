import SwiftUI
import CoreLocation

/// One location fix for pinning a new delivery address at the door. The browse
/// location is an area centre, and service-area eligibility and driver routing
/// both read the saved point, so checkout does not save an address without one.
@MainActor
final class CheckoutLocator: NSObject, ObservableObject, CLLocationManagerDelegate {
    enum Status { case idle, locating, denied, failed }

    @Published var status: Status = .idle
    @Published var fix: CLLocationCoordinate2D?
    private let manager = CLLocationManager()

    override init() {
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyNearestTenMeters
    }

    func locate() {
        switch manager.authorizationStatus {
        case .denied, .restricted:
            status = .denied
        case .notDetermined:
            status = .locating
            manager.requestWhenInUseAuthorization()
        default:
            status = .locating
            manager.requestLocation()
        }
    }

    nonisolated func locationManagerDidChangeAuthorization(_ m: CLLocationManager) {
        let auth = m.authorizationStatus
        Task { @MainActor in
            guard self.status == .locating else { return }
            switch auth {
            case .authorizedWhenInUse, .authorizedAlways: self.manager.requestLocation()
            case .denied, .restricted: self.status = .denied
            default: break
            }
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard let c = locations.last?.coordinate else { return }
        let lat = c.latitude, lng = c.longitude
        Task { @MainActor in
            self.fix = CLLocationCoordinate2D(latitude: lat, longitude: lng)
            self.status = .idle
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didFailWithError error: Error) {
        Task { @MainActor in self.status = .failed }
    }
}

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
