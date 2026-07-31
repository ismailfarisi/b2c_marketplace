import Foundation

/// A named point the feed can be served for.
///
/// Hard-coded, and that is a statement about the schema rather than a shortcut:
/// there is no place taxonomy anywhere in the product — no emirate, district or
/// area table — so a district list has to come from somewhere, and geography is
/// the one thing here that is not a product decision.
///
/// `label` is canonical English and travels to the server as the `place` on a
/// sell draft; `key` is what a person reads, through `Localizable.strings`. One
/// string for both would file an Arabic seller's listing under a different
/// place name from an English seller's in the same building, splitting every
/// geo-grouped report down the middle. The Android `Place` makes the same split
/// for the same reason.
struct Place {
    let label: String
    let lat: Double
    let lng: Double
    let key: String

    var displayName: String { NSLocalizedString(key, comment: "") }
}

let PLACES = [
    Place(label: "Dubai Marina", lat: 25.0805, lng: 55.1403, key: "place_dubai_marina"),
    Place(label: "Downtown Dubai", lat: 25.1972, lng: 55.2744, key: "place_downtown_dubai"),
    Place(label: "Business Bay", lat: 25.1857, lng: 55.2766, key: "place_business_bay"),
    Place(label: "Al Quoz", lat: 25.1420, lng: 55.2330, key: "place_al_quoz"),
    Place(label: "Al Barsha", lat: 25.1120, lng: 55.1960, key: "place_al_barsha"),
    Place(label: "Deira", lat: 25.2710, lng: 55.3120, key: "place_deira"),
]

/// The name to show for a point, given whatever label came back with it.
///
/// Matches on the canonical form, because that is what a listing is stored under
/// and what the feed echoes. Anything unrecognised renders as itself.
func displayPlace(_ label: String) -> String {
    PLACES.first { $0.label == label }?.displayName ?? label
}
