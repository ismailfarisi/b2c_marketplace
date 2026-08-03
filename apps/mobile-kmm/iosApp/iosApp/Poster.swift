import SwiftUI
import Shared

/// A poster, with the blurred approximation of itself underneath.
///
/// The decoder is `commonMain`'s — `BlurHash.decode` returns packed ARGB and is
/// tested once for both platforms. Nothing about the algorithm is reimplemented
/// here; what is here is the twelve lines that turn an `[Int32]` into a
/// `CGImage`, which is the part that genuinely differs.
///
/// The layering matters for the same reason it does on Android: the blur is
/// drawn *behind* rather than swapped for the image, so there is no frame where
/// the row is empty between the two, and nothing reflows — the box is the same
/// size throughout because its size never depended on what was inside it.
struct Poster: View {
    let url: String?
    let lqip: String?
    var contentMode: ContentMode = .fill
    var corner: CGFloat = 6

    var body: some View {
        ZStack {
            Palette.surface

            if let blur = Self.decode(lqip) {
                Image(decorative: blur, scale: 1)
                    .resizable()
                    // Interpolation does the blurring for free: the decode is
                    // 24×32 stretched over a much larger box. Turning it off
                    // would render visible squares.
                    .interpolation(.medium)
                    .aspectRatio(contentMode: .fill)
            }

            if let url, let parsed = URL(string: url) {
                AsyncImage(url: parsed) { phase in
                    switch phase {
                    case .success(let image):
                        image.resizable().aspectRatio(contentMode: contentMode)
                    default:
                        // Empty, not a placeholder. Anything drawn here would
                        // cover the blur, and a poster that 404s should leave a
                        // slightly soft thumbnail rather than a broken-image
                        // glyph — the row's job is to be chosen from, not to
                        // report on the object store's health.
                        Color.clear
                    }
                }
                .transition(.opacity)
            }
        }
        .clipShape(RoundedRectangle(cornerRadius: corner))
    }

    /// Decodes the hash to a `CGImage`, once per hash.
    ///
    /// 24×32 rather than square: reels are portrait and posters are cropped
    /// from them, so a square decode of a 9:16 frame is stretched wrongly in
    /// whichever direction it is scaled.
    private static let width = 24
    private static let height = 32

    private static var cache = NSCache<NSString, CGImage>()

    static func decode(_ hash: String?) -> CGImage? {
        guard let hash, !hash.isEmpty else { return nil }
        if let hit = cache.object(forKey: hash as NSString) { return hit }

        guard let pixels = BlurHash.shared.decode(
            hash: hash, width: Int32(width), height: Int32(height), punch: 1
        ) else { return nil }

        // KotlinIntArray → packed BGRA bytes. Kotlin gives ARGB in a 32-bit int;
        // Core Graphics wants the components in memory order, and on a
        // little-endian device `.byteOrder32Little` with premultipliedFirst is
        // exactly ARGB-as-an-integer.
        var buffer = [UInt32](repeating: 0, count: width * height)
        for i in 0..<(width * height) {
            buffer[i] = UInt32(bitPattern: pixels.get(index: Int32(i)))
        }

        let data = buffer.withUnsafeBufferPointer { Data(buffer: $0) }
        guard let provider = CGDataProvider(data: data as CFData) else { return nil }

        guard let image = CGImage(
            width: width,
            height: height,
            bitsPerComponent: 8,
            bitsPerPixel: 32,
            bytesPerRow: width * 4,
            space: CGColorSpaceCreateDeviceRGB(),
            bitmapInfo: CGBitmapInfo(rawValue:
                CGImageAlphaInfo.premultipliedFirst.rawValue |
                CGBitmapInfo.byteOrder32Little.rawValue),
            provider: provider,
            decode: nil,
            shouldInterpolate: true,
            intent: .defaultIntent
        ) else { return nil }

        // Cached because a list row is rebuilt on every scroll tick, and the
        // decode allocates a 3KB buffer each time. Sub-millisecond individually
        // and enough to cause collections during a fling.
        cache.setObject(image, forKey: hash as NSString)
        return image
    }
}
