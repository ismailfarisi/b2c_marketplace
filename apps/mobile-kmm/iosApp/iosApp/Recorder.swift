import AVFoundation
import SwiftUI

/// The viewfinder, and the one part of the sell flow that is genuinely
/// different on each platform.
///
/// Everything else — the step machine, the served field list, validation, the
/// upload phase, the trust document — is in `:shared` and identical to Compose.
/// A capture session is not: `AVCaptureMovieFileOutput` and CameraX have
/// nothing in common beyond the idea, and the parts worth getting right
/// (orientation, the duration cap, releasing the device) are platform-specific
/// in both.
///
/// # Why the clip is handed over as bytes
///
/// `SellViewModel.onClipRecorded` takes a `ByteArray` and a duration, and the
/// shared layer never learns what a file URL is. That looked wasteful — a
/// sixty-second clip is twenty megabytes in memory — and is the right trade: an
/// iOS URL and an Android `Uri` are different enough that a shared abstraction
/// over them would be a third thing neither platform uses, and the upload holds
/// the bytes anyway.
@MainActor
final class Recorder: NSObject, ObservableObject {

    /// The hard cap, matching the Android recorder and the server's probe gate.
    ///
    /// Enforced by the session rather than by a timer the user can outrun:
    /// `maxRecordedDuration` stops the write at exactly this point and delivers
    /// the file that exists. A clip cut short by a cap is usable; one rejected
    /// at the probe gate after a two-minute upload is not.
    static let maxDuration = CMTime(seconds: 60, preferredTimescale: 600)

    @Published private(set) var authorised = false
    /// The system will not ask again. Only Settings can change it now.
    @Published private(set) var blocked = false
    @Published private(set) var recording = false
    @Published private(set) var failure: String?

    /// The chosen cap, in seconds. Matches the Android rail: 15, 30 or 60.
    ///
    /// Applied to the output whenever it changes rather than read at start:
    /// `maxRecordedDuration` is a property of the output, and setting it only
    /// on the way into a take would leave a rail that says 15s recording for a
    /// minute.
    @Published var maxSeconds: Int = 60 {
        didSet { output.maxRecordedDuration = CMTime(seconds: Double(maxSeconds), preferredTimescale: 600) }
    }
    @Published private(set) var torchOn = false
    @Published private(set) var facingBack = true
    /// Whether the *current* camera can light anything. The front camera of
    /// most devices cannot, and a lit control that does nothing is worse than
    /// an absent one.
    @Published private(set) var hasTorch = false

    let session = AVCaptureSession()
    private let output = AVCaptureMovieFileOutput()
    private var onFinish: ((Data, Int64) -> Void)?
    private var startedAt: Date?
    private var videoInput: AVCaptureDeviceInput?

    /// Asks for the camera, and for the microphone separately.
    ///
    /// Separately because they are refused separately, and a viewer who granted
    /// the camera and declined the microphone should get a silent video rather
    /// than a dead screen. Plenty of reels on this platform are shot muted.
    func authorise() async {
        // Asked only while the system will actually ask. `requestAccess`
        // returns the stored answer immediately once a decision exists, and it
        // never prompts twice — so "Allow camera" was a button that did
        // nothing, permanently, on the tab this product gets its supply from.
        // The only way back is Settings, which the screen never mentioned.
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            authorised = true
            blocked = false
            configure()
        case .notDetermined:
            let camera = await AVCaptureDevice.requestAccess(for: .video)
            // Separately, because they are refused separately and a seller who
            // declined the microphone should get a silent clip rather than a
            // dead screen. Plenty of reels here are shot muted.
            _ = await AVCaptureDevice.requestAccess(for: .audio)
            authorised = camera
            blocked = !camera
            if camera { configure() }
        default:
            // .denied or .restricted. Restricted cannot be changed by the
            // person holding the phone at all, but Settings is still where
            // they would find that out.
            authorised = false
            blocked = true
        }
    }

    /// Re-reads the decision, for a return from Settings.
    ///
    /// Without it the seller grants the camera, comes back, and is looking at
    /// the same refusal screen — from which the obvious conclusion is that the
    /// setting did not take.
    func refreshAuthorisation() async {
        if AVCaptureDevice.authorizationStatus(for: .video) == .authorized, !authorised {
            await authorise()
        }
    }

    private func configure() {
        guard session.inputs.isEmpty else { return }

        session.beginConfiguration()
        // 1280×720 rather than the highest the device offers. The ladder caps
        // at 720p server-side, so capturing 4K would upload sixteen times the
        // bytes for a stream nobody is served — on a cellular connection, at
        // the seller's expense.
        session.sessionPreset = .hd1280x720

        if let camera = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: .back),
           let input = try? AVCaptureDeviceInput(device: camera),
           session.canAddInput(input) {
            session.addInput(input)
            // Kept, because flipping has to remove exactly this input and
            // searching the session for "the video one" later is how the
            // microphone gets detached instead.
            videoInput = input
            hasTorch = camera.hasTorch
        } else {
            failure = String(localized: "sell_camera_needed")
            session.commitConfiguration()
            return
        }

        // Audio is optional by design — see `authorise`.
        if let mic = AVCaptureDevice.default(for: .audio),
           let input = try? AVCaptureDeviceInput(device: mic),
           session.canAddInput(input) {
            session.addInput(input)
        }

        if session.canAddOutput(output) {
            session.addOutput(output)
            output.maxRecordedDuration = CMTime(seconds: Double(maxSeconds), preferredTimescale: 600)
        }
        session.commitConfiguration()

        // Off the main thread: `startRunning` blocks until the first frame, and
        // on an older device that is long enough to drop the animation into the
        // Sell tab.
        Task.detached { [session] in session.startRunning() }
    }

    func start(onFinish: @escaping (Data, Int64) -> Void) {
        guard authorised, !recording else { return }
        self.onFinish = onFinish

        let url = FileManager.default.temporaryDirectory
            .appendingPathComponent(UUID().uuidString)
            .appendingPathExtension("mov")

        // Portrait, always. The reel is a 9:16 surface and a landscape capture
        // would be letterboxed into it — the single most obvious sign of a feed
        // built by someone who did not watch it.
        output.connection(with: .video)?.videoRotationAngle = 90

        startedAt = Date()
        recording = true
        output.startRecording(to: url, recordingDelegate: self)
    }

    func stop() {
        guard recording else { return }
        output.stopRecording()
    }

    /// Swaps the camera.
    ///
    /// Refused mid-take: reconfiguring the session during a recording ends it,
    /// and a seller who taps this halfway round the car would lose the half
    /// they had.
    func flip() {
        guard !recording, let current = videoInput else { return }
        let wanted: AVCaptureDevice.Position = facingBack ? .front : .back
        guard let camera = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: wanted),
              let input = try? AVCaptureDeviceInput(device: camera) else { return }

        session.beginConfiguration()
        session.removeInput(current)
        if session.canAddInput(input) {
            session.addInput(input)
            videoInput = input
            facingBack = (wanted == .back)
        } else {
            // Put the old one back rather than leaving the session with no
            // camera at all, which renders as a viewfinder frozen on its last
            // frame with nothing to explain it.
            session.addInput(current)
        }
        session.commitConfiguration()

        // The new device has its own torch state, and its own answer to
        // whether it has one.
        hasTorch = videoInput?.device.hasTorch ?? false
        if torchOn { setTorch(true) }
    }

    func toggleTorch() { setTorch(!torchOn) }

    private func setTorch(_ on: Bool) {
        guard let device = videoInput?.device, device.hasTorch else {
            torchOn = false
            return
        }
        // The lock is mandatory; without it this throws and the torch silently
        // never comes on.
        guard (try? device.lockForConfiguration()) != nil else { return }
        device.torchMode = on ? .on : .off
        device.unlockForConfiguration()
        torchOn = on
    }

    /// Zoom, clamped to what this camera actually offers.
    ///
    /// A 2× request on a device whose wide-angle maxes below that throws, and
    /// the throw is the kind that takes the session down rather than the
    /// gesture.
    func setZoom(_ factor: CGFloat) {
        guard let device = videoInput?.device else { return }
        guard (try? device.lockForConfiguration()) != nil else { return }
        device.videoZoomFactor = max(1, min(factor, device.activeFormat.videoMaxZoomFactor))
        device.unlockForConfiguration()
    }

    /// Releases the camera.
    ///
    /// Called when the seller leaves the tab, not when the view disappears. A
    /// session left running holds the device, keeps the torch warm and shows in
    /// the status bar — and on iOS it will be taken away by any other app that
    /// asks, which surfaces as a viewfinder that silently freezes.
    func teardown() {
        if session.isRunning {
            Task.detached { [session] in session.stopRunning() }
        }
    }
}

extension Recorder: AVCaptureFileOutputRecordingDelegate {
    nonisolated func fileOutput(
        _ output: AVCaptureFileOutput,
        didFinishRecordingTo outputFileURL: URL,
        from connections: [AVCaptureConnection],
        error: Error?
    ) {
        // A duration-limit stop arrives here *as an error*, and the file is
        // perfectly good. Treating every error as a failure would throw away
        // exactly the sixty-second clip the cap exists to produce — the same
        // trap the Android recorder fell into with
        // ERROR_DURATION_LIMIT_REACHED.
        let hitTheCap = (error as? NSError)?.code == AVError.maximumDurationReached.rawValue
        let usable = error == nil || hitTheCap

        Task { @MainActor [weak self] in
            guard let self else { return }
            self.recording = false

            defer { try? FileManager.default.removeItem(at: outputFileURL) }

            guard usable, let data = try? Data(contentsOf: outputFileURL), !data.isEmpty else {
                self.failure = error?.localizedDescription ?? String(localized: "err_unknown")
                return
            }

            let elapsed = self.startedAt.map { Date().timeIntervalSince($0) } ?? 0
            self.onFinish?(data, Int64(elapsed * 1000))
        }
    }
}

/// The camera preview, wrapped for SwiftUI.
struct ViewfinderView: UIViewRepresentable {
    let session: AVCaptureSession

    func makeUIView(context: Context) -> PreviewView {
        let view = PreviewView()
        view.layer.session = session
        // Fill and crop, matching the player. A preview that letterboxes shows
        // the seller a different frame from the one they are recording, which
        // they only discover after publishing.
        view.layer.videoGravity = .resizeAspectFill
        return view
    }

    func updateUIView(_ view: PreviewView, context: Context) {}

    final class PreviewView: UIView {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        // swiftlint:disable:next force_cast
        var layer: AVCaptureVideoPreviewLayer { super.layer as! AVCaptureVideoPreviewLayer }
    }
}
