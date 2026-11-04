import CoreLocation
import SwiftUI

/// A fresh location fix for pinning a delivery address: never a cached point,
/// the feed's area or a fallback, because the pin is where the driver goes.
/// Needs NSLocationWhenInUseUsageDescription in the app's Info.plist.
@MainActor
final class FreshFix: NSObject, ObservableObject, CLLocationManagerDelegate {
    private let manager = CLLocationManager()
    private var done: @MainActor ((Double, Double)?) -> Void = { _ in }
    private var waiting = false

    override init() {
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyBest
    }

    /// Calls back once: the coordinate, or nil when refused, off or failed.
    func request(_ completion: @escaping @MainActor ((Double, Double)?) -> Void) {
        done = completion
        waiting = true
        switch manager.authorizationStatus {
        case .notDetermined: manager.requestWhenInUseAuthorization()
        case .authorizedWhenInUse, .authorizedAlways: manager.requestLocation()
        default: finish(nil)
        }
    }

    nonisolated func locationManagerDidChangeAuthorization(_ m: CLLocationManager) {
        let status = m.authorizationStatus
        Task { @MainActor in
            guard self.waiting else { return }
            switch status {
            case .authorizedWhenInUse, .authorizedAlways: self.manager.requestLocation()
            case .notDetermined: break
            default: self.finish(nil)
            }
        }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        let point = locations.last.map { ($0.coordinate.latitude, $0.coordinate.longitude) }
        Task { @MainActor in self.finish(point) }
    }

    nonisolated func locationManager(_ m: CLLocationManager, didFailWithError error: Error) {
        Task { @MainActor in self.finish(nil) }
    }

    private func finish(_ point: (Double, Double)?) {
        guard waiting else { return }
        waiting = false
        done(point)
    }
}
