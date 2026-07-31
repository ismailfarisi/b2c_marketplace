package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.android.R
import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.camera.core.CameraSelector
import androidx.camera.core.Preview
import androidx.camera.lifecycle.ProcessCameraProvider
import androidx.camera.video.FileOutputOptions
import androidx.camera.video.Quality
import androidx.camera.video.QualitySelector
import androidx.camera.video.Recorder
import androidx.camera.video.Recording
import androidx.camera.video.VideoCapture
import androidx.camera.video.VideoRecordEvent
import androidx.camera.view.PreviewView
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.content.ContextCompat
import java.io.File
import java.util.concurrent.Executor

/**
 * MaxClipMs caps a reel at sixty seconds.
 *
 * Not a storage decision. A walkaround of a car is interesting for about twenty
 * seconds and a two-minute one is a video nobody finishes, which costs the
 * seller the impression they were given. The cap is also what makes holding the
 * whole file in memory to upload it a defensible choice.
 */
private const val MaxClipMs = 60_000L

/**
 * The Sell tab's first screen: a live viewfinder, nothing else.
 *
 * Camera first and form last. A seller who has already filmed has sunk thirty
 * seconds into it and will finish four fields; a seller shown four fields first
 * closes the app. Nothing here asks what they are selling — that is a question
 * about the video, and it is asked after the video exists.
 *
 * There is no gallery import. It is the obvious next feature and it is
 * deliberately absent for now: the whole wedge of this product is that its
 * supply is filmed rather than photographed, and the first thing an import
 * button gets used for is a slideshow of the same stills every classifieds site
 * already has.
 */
@Composable
fun RecordScreen(
    onRecorded: (bytes: ByteArray, durationMs: Long) -> Unit,
    onClose: () -> Unit,
    tip: String,
    modifier: Modifier = Modifier,
) {
    val context = LocalContext.current
    val lifecycleOwner = LocalLifecycleOwner.current
    val executor = remember { ContextCompat.getMainExecutor(context) }

    var hasCamera by remember { mutableStateOf(context.hasPermission(Manifest.permission.CAMERA)) }
    // Tracked separately and never gating the recorder. A seller who refuses the
    // microphone gets a silent clip, which is a worse listing and still a
    // listing — blocking them would trade the supply for the audio.
    var hasAudio by remember { mutableStateOf(context.hasPermission(Manifest.permission.RECORD_AUDIO)) }

    val permissions = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) { granted ->
        hasCamera = granted[Manifest.permission.CAMERA] ?: hasCamera
        hasAudio = granted[Manifest.permission.RECORD_AUDIO] ?: hasAudio
    }

    LaunchedEffect(Unit) {
        if (!hasCamera) {
            permissions.launch(arrayOf(Manifest.permission.CAMERA, Manifest.permission.RECORD_AUDIO))
        }
    }

    var recording by remember { mutableStateOf<Recording?>(null) }
    var elapsedMs by remember { mutableStateOf(0L) }
    var capture by remember { mutableStateOf<VideoCapture<Recorder>?>(null) }
    var facingBack by remember { mutableStateOf(true) }

    Box(modifier.fillMaxSize().background(Color.Black)) {

        if (!hasCamera) {
            CameraDenied(
                onAsk = {
                    permissions.launch(arrayOf(Manifest.permission.CAMERA, Manifest.permission.RECORD_AUDIO))
                },
                modifier = Modifier.align(Alignment.Center),
            )
        } else {
            AndroidView(
                modifier = Modifier.fillMaxSize(),
                factory = { ctx ->
                    PreviewView(ctx).also { view ->
                        // FILL_CENTER, matching the feed. A viewfinder that
                        // letterboxes what the reel will crop shows the seller a
                        // frame they are not going to get, and they compose for
                        // the wrong one.
                        view.scaleType = PreviewView.ScaleType.FILL_CENTER
                    }
                },
                update = { view ->
                    bindCamera(view, lifecycleOwner, facingBack, executor) { capture = it }
                },
            )
        }

        // Close and the elapsed counter, over the frame.
        Row(
            Modifier
                .fillMaxWidth()
                .statusBarsPadding()
                .padding(horizontal = 16.dp, vertical = 10.dp),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = "✕",
                color = Color.White,
                fontSize = 22.sp,
                modifier = Modifier.clickable(onClick = onClose).padding(8.dp),
            )
            Box(
                Modifier
                    .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(50))
                    .padding(horizontal = 12.dp, vertical = 5.dp)
            ) {
                Text(
                    text = "${clock(elapsedMs)} / ${clock(MaxClipMs)}",
                    color = if (recording != null) Accent else Color.White,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.Medium,
                )
            }
            Text(
                text = "⟳",
                color = Color.White,
                fontSize = 22.sp,
                modifier = Modifier
                    // Disabled mid-take. Rebinding the camera during a recording
                    // ends it, and a seller who taps this halfway round the car
                    // would lose the half they had.
                    .clickable(enabled = recording == null) { facingBack = !facingBack }
                    .padding(8.dp),
            )
        }

        Column(
            Modifier
                .align(Alignment.BottomCenter)
                .fillMaxWidth()
                .padding(bottom = 40.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            if (tip.isNotEmpty() && recording == null) {
                // The vertical's own advice, from the server. There is no
                // when(vertical) here — a tip for property or for a restaurant
                // offer arrives the same way and this code does not change.
                Text(
                    text = tip,
                    color = Color.White.copy(alpha = 0.85f),
                    fontSize = 13.sp,
                    modifier = Modifier
                        .padding(horizontal = 32.dp, vertical = 16.dp)
                        .background(Color.Black.copy(alpha = 0.35f), RoundedCornerShape(10.dp))
                        .padding(horizontal = 14.dp, vertical = 9.dp),
                )
            }

            ShutterButton(
                recording = recording != null,
                enabled = hasCamera && capture != null,
                onClick = {
                    val active = recording
                    if (active != null) {
                        active.stop()
                        return@ShutterButton
                    }
                    val videoCapture = capture ?: return@ShutterButton
                    recording = startRecording(
                        context = context,
                        capture = videoCapture,
                        withAudio = hasAudio,
                        executor = executor,
                        onProgress = { elapsedMs = it },
                        onFinished = { file, durationMs ->
                            recording = null
                            elapsedMs = 0
                            if (file != null) {
                                // Read once, then deleted. The bytes go to the
                                // view model, which holds them until the draft
                                // exists to upload them to; leaving the file
                                // behind would accumulate a clip per abandoned
                                // attempt in the app's cache.
                                val bytes = runCatching { file.readBytes() }.getOrNull()
                                file.delete()
                                if (bytes != null) onRecorded(bytes, durationMs)
                            }
                        },
                    )
                },
            )
        }
    }

    DisposableEffect(Unit) {
        onDispose {
            // Leaving the tab mid-take must not leave a recorder running with the
            // microphone open behind another screen.
            recording?.stop()
        }
    }
}

@Composable
private fun ShutterButton(recording: Boolean, enabled: Boolean, onClick: () -> Unit) {
    Box(
        Modifier
            .size(76.dp)
            .border(3.dp, if (enabled) Color.White else Color.White.copy(alpha = 0.3f), RoundedCornerShape(50))
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        Box(
            Modifier
                .size(if (recording) 30.dp else 60.dp)
                .background(
                    if (enabled) Accent else Accent.copy(alpha = 0.3f),
                    // Square while recording, round when idle. The shape *is* the
                    // stop affordance; a second icon over a live viewfinder is
                    // one more thing between the seller and the car.
                    RoundedCornerShape(if (recording) 6.dp else 50.dp),
                ),
        )
    }
}

@Composable
private fun CameraDenied(onAsk: () -> Unit, modifier: Modifier = Modifier) {
    Column(
        modifier.padding(horizontal = 40.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = stringResource(R.string.sell_camera_needed),
            color = Color.White,
            fontSize = 15.sp,
            fontWeight = FontWeight.Medium,
        )
        Text(
            text = stringResource(R.string.sell_camera_why),
            color = Color.White.copy(alpha = 0.6f),
            fontSize = 13.sp,
            modifier = Modifier.padding(top = 8.dp),
        )
        Box(
            Modifier
                .padding(top = 20.dp)
                .background(Accent, RoundedCornerShape(10.dp))
                .clickable(onClick = onAsk)
                .padding(horizontal = 22.dp, vertical = 11.dp)
        ) {
            Text(stringResource(R.string.sell_camera_allow), color = Color.White, fontSize = 14.sp)
        }
    }
}

// ── CameraX plumbing ─────────────────────────────────────────────────────────

private fun bindCamera(
    view: PreviewView,
    lifecycleOwner: androidx.lifecycle.LifecycleOwner,
    facingBack: Boolean,
    executor: Executor,
    onBound: (VideoCapture<Recorder>) -> Unit,
) {
    val future = ProcessCameraProvider.getInstance(view.context)
    future.addListener({
        val provider = runCatching { future.get() }.getOrNull() ?: return@addListener

        val preview = Preview.Builder().build().also { it.surfaceProvider = view.surfaceProvider }
        val recorder = Recorder.Builder()
            // HD, not the highest the sensor offers. The pipeline caps playback
            // at 720p (PlaybackPolicy), so anything above it is bytes the seller
            // uploads on a cell connection and nobody ever sees. Fallback rules
            // handle a device that cannot do HD.
            .setQualitySelector(
                QualitySelector.from(Quality.HD, FallbackStrategy)
            )
            .build()
        val capture = VideoCapture.withOutput(recorder)

        val selector = if (facingBack) CameraSelector.DEFAULT_BACK_CAMERA
        else CameraSelector.DEFAULT_FRONT_CAMERA

        runCatching {
            provider.unbindAll()
            provider.bindToLifecycle(lifecycleOwner, selector, preview, capture)
            onBound(capture)
        }
    }, executor)
}

private val FallbackStrategy =
    androidx.camera.video.FallbackStrategy.lowerQualityOrHigherThan(Quality.SD)

@SuppressLint("MissingPermission")
private fun startRecording(
    context: Context,
    capture: VideoCapture<Recorder>,
    withAudio: Boolean,
    executor: Executor,
    onProgress: (Long) -> Unit,
    onFinished: (File?, Long) -> Unit,
): Recording? {
    val file = File(context.cacheDir, "sell-clip-${System.currentTimeMillis()}.mp4")
    // The cap is enforced by the recorder rather than by the status callback.
    // A check in the callback stops at whenever the next status event happens to
    // fire — a few hundred milliseconds late, and later still under load — and
    // the recorder finalises a clean file at the limit instead.
    val options = FileOutputOptions.Builder(file)
        .setDurationLimitMillis(MaxClipMs)
        .build()

    return runCatching {
        capture.output
            .prepareRecording(context, options)
            .apply { if (withAudio) withAudioEnabled() }
            .start(executor) { event ->
                when (event) {
                    is VideoRecordEvent.Status ->
                        onProgress(event.recordingStats.recordedDurationNanos / 1_000_000)

                    is VideoRecordEvent.Finalize -> {
                        val ms = event.recordingStats.recordedDurationNanos / 1_000_000
                        // A duration-limit stop reports an error code and still
                        // produces a complete file. Treating every non-zero code
                        // as a failure would throw away exactly the clips that
                        // ran the full sixty seconds.
                        val usable = !event.hasError() ||
                            event.error == VideoRecordEvent.Finalize.ERROR_DURATION_LIMIT_REACHED
                        if (usable && file.length() > 0) {
                            onFinished(file, ms)
                        } else {
                            file.delete()
                            onFinished(null, 0)
                        }
                    }
                }
            }
    }.getOrNull()
}

private fun Context.hasPermission(permission: String): Boolean =
    ContextCompat.checkSelfPermission(this, permission) == PackageManager.PERMISSION_GRANTED

private fun clock(ms: Long): String {
    val total = ms / 1000
    return "${total / 60}:${(total % 60).toString().padStart(2, '0')}"
}
