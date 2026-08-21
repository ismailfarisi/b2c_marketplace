import AVFoundation
import PhotosUI
import SwiftUI

/// A control on the recorder's left rail: a symbol, or the control's own value
/// on the face when the value is the point, and a name under it.
///
/// The *value* is on the face rather than only the name, because these cycle on
/// tap. A "Length" button that does not say 30s is a button the seller has to
/// press in order to discover the state of.
struct RailControl: View {
    /// Nil for the controls whose value *is* the face — length and zoom. "30s"
    /// and "2x" say in two characters what a stopwatch or a magnifier can only
    /// hint at.
    var system: String?
    /// Optional, not empty-string: LocalizedStringKey compares by key and
    /// testing it against "" is a lookup nobody meant to perform.
    let label: LocalizedStringKey?
    let value: String
    var active: Bool = false
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            VStack(spacing: 3) {
                ZStack {
                    Circle()
                        .fill(active ? Palette.accent.opacity(0.85) : .black.opacity(0.45))
                        .frame(width: 40, height: 40)
                    if let system {
                        Image(systemName: system)
                            .font(.system(size: 17))
                            .foregroundStyle(.white)
                    } else {
                        Text(value)
                            .font(.system(size: 13).weight(.semibold))
                            .foregroundStyle(.white)
                    }
                }
                if system != nil && !value.isEmpty {
                    Text(value)
                        .font(.system(size: 11).weight(.semibold))
                        .foregroundStyle(.white)
                }
                if let label {
                    Text(label)
                        .font(.system(size: 10))
                        .foregroundStyle(.white.opacity(0.7))
                }
            }
        }
        .buttonStyle(.plain)
    }
}

/// Gallery and flip, either side of the shutter.
struct SideButton: View {
    let system: String
    let label: LocalizedStringKey
    let enabled: Bool
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            ZStack {
                // A disc, like the rail's controls. Two shapes for the same
                // kind of control on one viewfinder is a difference that
                // implies a distinction nobody meant.
                Circle()
                    .fill(.black.opacity(0.45))
                    .frame(width: 52, height: 52)
                Image(systemName: system)
                    .font(.system(size: 22))
                    .foregroundStyle(enabled ? .white : .white.opacity(0.35))
            }
        }
        .buttonStyle(.plain)
        .disabled(!enabled)
        .accessibilityLabel(Text(label))
    }
}

// ── gallery import ───────────────────────────────────────────────────────────

/// The outcome of reading a picked video, so the caller can say which thing was
/// wrong rather than "that did not work".
enum PickedClip {
    case ok(Data, Int64)
    case tooLong
    case tooLarge
    case unreadable
    case cancelled
}

/// Bytes are held whole until the draft exists to upload them to, which is
/// defensible for a sixty-second recording this app made and is not defensible
/// for an arbitrary file off the device. A 4K clip from a modern phone is
/// comfortably several hundred megabytes and would be terminated by the
/// watchdog before it ever reached the network.
private let maxClipBytes = 220 * 1024 * 1024

/// `PHPickerViewController`, restricted to videos.
///
/// The system picker rather than a `PhotosPicker`, because the filter has to be
/// `.videos` and the result has to be loaded as a *file* — `PhotosPickerItem`
/// hands back a `Data` transferable that materialises the whole clip in memory
/// before anything has had a chance to reject it for being too long.
struct VideoPicker: UIViewControllerRepresentable {
    let maxSeconds: Int
    let onResult: (PickedClip) -> Void

    func makeUIViewController(context: Context) -> PHPickerViewController {
        var config = PHPickerConfiguration(photoLibrary: .shared())
        config.filter = .videos
        config.selectionLimit = 1
        // The picker reads without the app holding library access at all,
        // which is why there is no permission prompt on this path.
        config.preferredAssetRepresentationMode = .current
        let controller = PHPickerViewController(configuration: config)
        controller.delegate = context.coordinator
        return controller
    }

    func updateUIViewController(_ controller: PHPickerViewController, context: Context) {}

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    final class Coordinator: NSObject, PHPickerViewControllerDelegate {
        private let parent: VideoPicker
        init(_ parent: VideoPicker) { self.parent = parent }

        func picker(_ picker: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
            guard let provider = results.first?.itemProvider,
                  provider.hasItemConformingToTypeIdentifier(UTType.movie.identifier) else {
                parent.onResult(.cancelled)
                return
            }

            provider.loadFileRepresentation(forTypeIdentifier: UTType.movie.identifier) { url, _ in
                // The URL is valid only inside this closure — the system
                // deletes the copy on return — so everything that reads it
                // happens here rather than being handed out.
                guard let url else {
                    DispatchQueue.main.async { self.parent.onResult(.unreadable) }
                    return
                }
                let outcome = Self.read(url, maxSeconds: self.parent.maxSeconds)
                DispatchQueue.main.async { self.parent.onResult(outcome) }
            }
        }

        private static func read(_ url: URL, maxSeconds: Int) -> PickedClip {
            let asset = AVURLAsset(url: url)
            let seconds = CMTimeGetSeconds(asset.duration)
            guard seconds.isFinite, seconds > 0 else { return .unreadable }

            // Checked before the bytes are read, not after. Reading first would
            // mean materialising the file to discover it is too long to use.
            //
            // One second of slack: a clip trimmed to exactly thirty seconds
            // routinely reports 30.01s, and refusing it would be
            // indistinguishable from a bug to the person who just trimmed it.
            if seconds > Double(maxSeconds) + 1 { return .tooLong }

            let attributes = try? FileManager.default.attributesOfItem(atPath: url.path)
            if let size = attributes?[.size] as? Int, size > maxClipBytes { return .tooLarge }

            guard let data = try? Data(contentsOf: url, options: .mappedIfSafe),
                  !data.isEmpty else { return .unreadable }
            // Re-checked against what was actually read: the attribute lookup
            // is optional and returns nil for more providers than it should,
            // so the cheap check above silently passes exactly the files it
            // exists to catch.
            if data.count > maxClipBytes { return .tooLarge }

            return .ok(data, Int64(seconds * 1000))
        }
    }
}
