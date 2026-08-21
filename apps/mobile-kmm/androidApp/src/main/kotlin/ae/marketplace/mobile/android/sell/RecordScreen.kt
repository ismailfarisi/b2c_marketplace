package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.android.R
import android.Manifest
import android.app.Activity
import android.content.ContextWrapper
import android.content.Intent
import android.net.Uri
import android.provider.Settings
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
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Cameraswitch
import androidx.compose.material.icons.filled.Close
import androidx.compose.material.icons.filled.FlashOff
import androidx.compose.material.icons.filled.FlashOn
import androidx.compose.material.icons.outlined.PhotoLibrary
import androidx.compose.material.icons.outlined.Timer
import androidx.compose.material3.Icon
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
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalLifecycleOwner
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import androidx.core.app.ActivityCompat
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.core.content.ContextCompat
import android.media.MediaMetadataRetriever
import androidx.activity.result.PickVisualMediaRequest
import androidx.camera.core.Camera
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.width
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import ae.marketplace.mobile.android.ui.Palette
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
 * There is a gallery import, video-only, and the restriction is the point:
 * the whole wedge of this product is that its supply is filmed rather than
 * photographed, and a picker that accepted images would fill the feed with
 * the same stills every classifieds site already has.
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
    val closeLabel = stringResource(R.string.action_close)

    var hasCamera by remember { mutableStateOf(context.hasPermission(Manifest.permission.CAMERA)) }
    // Tracked separately and never gating the recorder. A seller who refuses the
    // microphone gets a silent clip, which is a worse listing and still a
    // listing — blocking them would trade the supply for the audio.
    var hasAudio by remember { mutableStateOf(context.hasPermission(Manifest.permission.RECORD_AUDIO)) }

    // Whether the system will still show a dialog if we ask again.
    //
    // It will not, once the seller has refused twice or picked "Don't allow" —
    // and `launch` then returns denied without drawing anything. So "Allow
    // camera" was a button that did nothing, forever, on the tab this product
    // gets its supply from. The only way back is Settings, which the screen
    // never mentioned.
    //
    // `shouldShowRequestPermissionRationale` cannot distinguish "never asked"
    // from "permanently denied" on its own — both are false — so the ask is
    // recorded and the two are read together.
    var asked by rememberSaveable { mutableStateOf(false) }
    var blocked by remember { mutableStateOf(false) }

    val permissions = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestMultiplePermissions()
    ) { granted ->
        hasCamera = granted[Manifest.permission.CAMERA] ?: hasCamera
        hasAudio = granted[Manifest.permission.RECORD_AUDIO] ?: hasAudio
        blocked = !hasCamera && !context.canAskAgain(Manifest.permission.CAMERA)
    }

    val ask = {
        asked = true
        permissions.launch(arrayOf(Manifest.permission.CAMERA, Manifest.permission.RECORD_AUDIO))
    }

    // Re-read on resume: the seller may have granted it in Settings and come
    // back, and without this they return to the same refusal screen and
    // conclude the setting did not take.
    val lifecycle = LocalLifecycleOwner.current.lifecycle
    DisposableEffect(lifecycle) {
        val observer = LifecycleEventObserver { _, event ->
            if (event == Lifecycle.Event.ON_RESUME) {
                hasCamera = context.hasPermission(Manifest.permission.CAMERA)
                hasAudio = context.hasPermission(Manifest.permission.RECORD_AUDIO)
                if (hasCamera) blocked = false
                else if (asked) blocked = !context.canAskAgain(Manifest.permission.CAMERA)
            }
        }
        lifecycle.addObserver(observer)
        onDispose { lifecycle.removeObserver(observer) }
    }

    LaunchedEffect(Unit) {
        if (!hasCamera && !asked) ask()
    }

    var recording by remember { mutableStateOf<Recording?>(null) }
    var elapsedMs by remember { mutableStateOf(0L) }
    var capture by remember { mutableStateOf<VideoCapture<Recorder>?>(null) }
    var camera by remember { mutableStateOf<Camera?>(null) }
    var facingBack by remember { mutableStateOf(true) }

    // The controls. Saveable, because a rotation mid-setup that reset the
    // seller's chosen length back to 60s would be its own small insult.
    var maxMs by rememberSaveable { mutableStateOf(MaxClipMs) }
    var timerSec by rememberSaveable { mutableStateOf(0) }
    var torchOn by rememberSaveable { mutableStateOf(false) }
    var zoom by rememberSaveable { mutableStateOf(1f) }
    // Ticking down before a take. Null when not counting.
    var countdown by remember { mutableStateOf<Int?>(null) }
    var notice by remember { mutableStateOf<String?>(null) }

    // Torch and zoom are applied to the bound camera, and re-applied whenever
    // it is rebound — flipping to the front camera creates a new one, and the
    // old torch state would otherwise be silently lost.
    LaunchedEffect(camera, torchOn, zoom) {
        val cam = camera ?: return@LaunchedEffect
        runCatching { cam.cameraControl.enableTorch(torchOn) }
        val max = cam.cameraInfo.zoomState.value?.maxZoomRatio ?: 1f
        runCatching { cam.cameraControl.setZoomRatio(zoom.coerceIn(1f, max)) }
    }

    // The front camera has no torch on most devices. Leaving the control lit
    // while it does nothing is worse than removing it.
    val hasTorch = camera?.cameraInfo?.hasFlashUnit() == true

    val startTake: () -> Unit = start@{
        val videoCapture = capture ?: return@start
        recording = startRecording(
            context = context,
            capture = videoCapture,
            withAudio = hasAudio,
            maxMs = maxMs,
            executor = executor,
            onProgress = { elapsedMs = it },
            onFinished = { file, durationMs ->
                recording = null
                elapsedMs = 0
                if (file != null) {
                    // Read once, then deleted. The bytes go to the view model,
                    // which holds them until the draft exists to upload them
                    // to; leaving the file behind would accumulate a clip per
                    // abandoned attempt in the app's cache.
                    val bytes = runCatching { file.readBytes() }.getOrNull()
                    file.delete()
                    if (bytes != null) onRecorded(bytes, durationMs)
                }
            },
        )
    }

    // The self-timer, so the phone can be propped on the wing mirror and the
    // seller can be in shot walking round the car — which is the whole reason
    // this exists rather than a nicety.
    LaunchedEffect(countdown) {
        val n = countdown ?: return@LaunchedEffect
        if (n <= 0) {
            countdown = null
            startTake()
            return@LaunchedEffect
        }
        kotlinx.coroutines.delay(1000)
        countdown = n - 1
    }

    val picker = rememberLauncherForActivityResult(
        // Video only, and not by oversight. The wedge of this product is that
        // its supply is filmed; a picker that accepted images would fill the
        // feed with the same stills every classifieds site already has, which
        // is the argument that kept a gallery button out of this screen for as
        // long as there wasn't one.
        ActivityResultContracts.PickVisualMedia()
    ) { uri ->
        if (uri == null) return@rememberLauncherForActivityResult
        when (val picked = context.readClip(uri, maxMs)) {
            is Picked.Ok -> onRecorded(picked.bytes, picked.durationMs)
            is Picked.TooLong -> notice = context.getString(
                R.string.sell_clip_too_long, (maxMs / 1000).toInt()
            )
            is Picked.TooLarge -> notice = context.getString(R.string.sell_clip_too_large)
            is Picked.Unreadable -> notice = context.getString(R.string.sell_clip_unreadable)
        }
    }

    Box(modifier.fillMaxSize().background(Color.Black)) {

        if (!hasCamera) {
            CameraDenied(
                blocked = blocked,
                onAsk = ask,
                onSettings = { context.openAppSettings() },
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
                    bindCamera(view, lifecycleOwner, facingBack, executor) { c, cam ->
                        capture = c
                        camera = cam
                    }
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
            Box(
                contentAlignment = Alignment.Center,
                modifier = Modifier
                    .size(36.dp)
                    .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(50))
                    .clickable(onClick = onClose)
                    .semantics { contentDescription = closeLabel },
            ) {
                Icon(
                    imageVector = Icons.Filled.Close,
                    contentDescription = null,
                    tint = Color.White,
                    modifier = Modifier.size(20.dp),
                )
            }
            Box(
                Modifier
                    .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(50))
                    .padding(horizontal = 12.dp, vertical = 5.dp)
            ) {
                Text(
                    // The chosen length, not the constant. The pill reading 1:00 while
                    // the rail said 15s was the two halves of the same fact
                    // disagreeing on screen.
                    text = "${clock(elapsedMs)} / ${clock(maxMs)}",
                    color = if (recording != null) Accent else Color.White,
                    fontSize = 12.sp,
                    fontWeight = FontWeight.Medium,
                )
            }
            // Flip moved down beside the shutter, where the reference puts it
            // and where a thumb already is. This holds its width so the clock
            // stays centred rather than drifting right.
            Spacer(Modifier.width(38.dp))
        }

        // The control rail, down the left edge like the reference.
        //
        // Four controls, and every one of them does something. The reference
        // also carries Audio, Effects, Green screen and Touch up; those belong
        // to a video product, not to a listing tool, and adding them as dead
        // buttons would be worse than not having them.
        if (hasCamera && recording == null && countdown == null) {
            Column(
                Modifier
                    .align(Alignment.CenterStart)
                    .padding(start = 10.dp),
                verticalArrangement = Arrangement.spacedBy(18.dp),
            ) {
                RailControl(
                    label = stringResource(R.string.sell_length),
                    value = "${maxMs / 1000}s",
                    onClick = {
                        maxMs = when (maxMs) {
                            15_000L -> 30_000L
                            30_000L -> 60_000L
                            else -> 15_000L
                        }
                    },
                )
                RailControl(
                    icon = Icons.Outlined.Timer,
                    label = stringResource(R.string.sell_timer),
                    value = if (timerSec == 0) stringResource(R.string.sell_off)
                    else stringResource(R.string.sell_seconds_short, timerSec),
                    active = timerSec > 0,
                    onClick = { timerSec = when (timerSec) { 0 -> 3; 3 -> 10; else -> 0 } },
                )
                if (hasTorch) {
                    RailControl(
                        icon = if (torchOn) Icons.Filled.FlashOn else Icons.Filled.FlashOff,
                        label = stringResource(R.string.sell_flash),
                        value = "",
                        active = torchOn,
                        onClick = { torchOn = !torchOn },
                    )
                }
                RailControl(
                    label = "",
                    value = if (zoom > 1.5f) "2×" else "1×",
                    active = zoom > 1.5f,
                    onClick = { zoom = if (zoom > 1.5f) 1f else 2f },
                )
            }
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

            notice?.let {
                Text(
                    text = it,
                    color = Color.White,
                    fontSize = 13.sp,
                    modifier = Modifier
                        .padding(horizontal = 32.dp)
                        .background(Palette.Bad.copy(alpha = 0.85f), RoundedCornerShape(10.dp))
                        .padding(horizontal = 14.dp, vertical = 9.dp)
                        .clickable { notice = null },
                )
                Spacer(Modifier.height(12.dp))
            }

            // Gallery, shutter, flip — the reference layout, and the order a
            // right-handed thumb reaches them in.
            Row(
                Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.Center,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                SideButton(
                    icon = Icons.Outlined.PhotoLibrary,
                    label = stringResource(R.string.sell_gallery),
                    // Not offered mid-take. Launching a picker over a running
                    // recorder leaves the microphone open behind another app's
                    // UI.
                    enabled = recording == null && countdown == null,
                    onClick = {
                        notice = null
                        picker.launch(
                            PickVisualMediaRequest(ActivityResultContracts.PickVisualMedia.VideoOnly)
                        )
                    },
                )

                Spacer(Modifier.width(28.dp))

                ShutterButton(
                    recording = recording != null,
                    enabled = hasCamera && capture != null && countdown == null,
                    countdown = countdown,
                    onClick = {
                        val active = recording
                        when {
                            active != null -> active.stop()
                            timerSec > 0 -> countdown = timerSec
                            else -> startTake()
                        }
                    },
                )

                Spacer(Modifier.width(28.dp))

                SideButton(
                    icon = Icons.Filled.Cameraswitch,
                    label = stringResource(R.string.sell_flip_camera),
                    // Disabled mid-take. Rebinding the camera during a
                    // recording ends it, and a seller who taps this halfway
                    // round the car would lose the half they had.
                    enabled = recording == null && countdown == null,
                    onClick = { facingBack = !facingBack },
                )
            }
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
private fun ShutterButton(
    recording: Boolean,
    enabled: Boolean,
    countdown: Int? = null,
    onClick: () -> Unit,
) {
    Box(
        Modifier
            .size(76.dp)
            .border(3.dp, if (enabled) Color.White else Color.White.copy(alpha = 0.3f), RoundedCornerShape(50))
            .clickable(enabled = enabled, onClick = onClick),
        contentAlignment = Alignment.Center,
    ) {
        if (countdown != null) {
            // The number replaces the fill rather than sitting over it. A
            // seller who has propped the phone up is reading this from two
            // metres away.
            Text(
                text = "$countdown",
                color = Color.White,
                fontSize = 34.sp,
                fontWeight = FontWeight.Bold,
            )
        } else {
            Box(
                Modifier
                    .size(if (recording) 30.dp else 60.dp)
                    .background(
                        // Red, not the brand orange. Every camera the seller
                        // has ever used records on red, and the shutter is not
                        // the place to teach them a new colour.
                        if (enabled) Palette.Bad else Palette.Bad.copy(alpha = 0.3f),
                        // Square while recording, round when idle. The shape *is*
                        // the stop affordance; a second icon over a live
                        // viewfinder is one more thing between the seller and the
                        // car.
                        RoundedCornerShape(if (recording) 6.dp else 50.dp),
                    ),
            )
        }
    }
}

/**
 * A control on the left rail: an icon, or the control's own value on the
 * face when the value is the point, and a name under it.
 *
 * The *value* is on the face rather than only the name, because these cycle on
 * tap. A "Length" button that does not say 30s is a button the seller has to
 * press to discover the state of.
 */
@Composable
private fun RailControl(
    label: String,
    value: String,
    icon: ImageVector? = null,
    active: Boolean = false,
    onClick: () -> Unit,
) {
    Column(
        horizontalAlignment = Alignment.CenterHorizontally,
        modifier = Modifier
            .clickable(onClick = onClick)
            .padding(6.dp),
    ) {
        Box(
            Modifier
                .size(40.dp)
                .background(
                    if (active) Accent.copy(alpha = 0.85f) else Color.Black.copy(alpha = 0.45f),
                    RoundedCornerShape(50),
                ),
            contentAlignment = Alignment.Center,
        ) {
            if (icon != null) {
                Icon(
                    imageVector = icon,
                    contentDescription = null,
                    tint = Color.White,
                    modifier = Modifier.size(20.dp),
                )
            } else {
                // Length and zoom wear their value instead of an icon. "30s"
                // and "2x" say in two characters what a stopwatch or a
                // magnifier can only hint at, and these are the two controls
                // whose current setting the seller has to read before filming.
                Text(
                    text = value,
                    color = Color.White,
                    fontSize = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                )
            }
        }
        if (icon != null && value.isNotEmpty()) {
            Text(
                text = value,
                color = Color.White,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
        if (label.isNotEmpty()) {
            Text(
                text = label,
                color = Color.White.copy(alpha = 0.7f),
                fontSize = 10.sp,
                modifier = Modifier.padding(top = 2.dp),
            )
        }
    }
}

/** Gallery and flip, either side of the shutter. */
@Composable
private fun SideButton(
    icon: ImageVector,
    label: String,
    enabled: Boolean,
    onClick: () -> Unit,
) {
    Box(
        Modifier
            .size(52.dp)
            .background(Color.Black.copy(alpha = 0.45f), RoundedCornerShape(50))
            .clickable(enabled = enabled, onClick = onClick)
            .semantics { contentDescription = label },
        contentAlignment = Alignment.Center,
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            tint = if (enabled) Color.White else Color.White.copy(alpha = 0.35f),
            modifier = Modifier.size(24.dp),
        )
    }
}

// ── gallery import ───────────────────────────────────────────────────────────

/**
 * The outcome of reading a picked video, so the caller can say which thing was
 * wrong rather than "that did not work".
 */
private sealed interface Picked {
    data class Ok(val bytes: ByteArray, val durationMs: Long) : Picked
    data object TooLong : Picked
    data object TooLarge : Picked
    data object Unreadable : Picked
}

/**
 * Bytes hold the whole clip in memory until the draft exists to upload it to,
 * which is defensible for a sixty-second recording this app made and is not
 * defensible for an arbitrary file off the device. A 4K clip from a modern
 * phone is comfortably several hundred megabytes and would take the process
 * down before it ever reached the network.
 */
private const val MaxClipBytes = 220L * 1024 * 1024

private fun Context.readClip(uri: Uri, maxMs: Long): Picked {
    val retriever = MediaMetadataRetriever()
    return try {
        retriever.setDataSource(this, uri)
        val durationMs = retriever
            .extractMetadata(MediaMetadataRetriever.METADATA_KEY_DURATION)
            ?.toLongOrNull() ?: return Picked.Unreadable

        // Checked before the bytes are read, not after. Reading first would
        // mean allocating the file to discover it is too long to use.
        //
        // One second of slack: a clip trimmed to exactly thirty seconds often
        // reports 30_012ms, and refusing it would be indistinguishable from a
        // bug to the person who just trimmed it.
        if (durationMs > maxMs + 1_000) return Picked.TooLong

        val size = contentResolver.openAssetFileDescriptor(uri, "r")?.use { it.length } ?: -1L
        if (size > MaxClipBytes) return Picked.TooLarge

        val bytes = contentResolver.openInputStream(uri)?.use { it.readBytes() }
            ?: return Picked.Unreadable
        if (bytes.isEmpty()) return Picked.Unreadable
        // Re-checked against the bytes actually read: the descriptor reports
        // UNKNOWN_LENGTH (-1) for plenty of providers, so the cheap check above
        // silently passes for exactly the files it was meant to catch.
        if (bytes.size > MaxClipBytes) return Picked.TooLarge

        Picked.Ok(bytes, durationMs)
    } catch (_: Exception) {
        // A picked file can be anything: a stream the provider revoked, a
        // codec this device cannot parse, a URI that has already expired.
        Picked.Unreadable
    } finally {
        runCatching { retriever.release() }
    }
}

@Composable
private fun CameraDenied(
    blocked: Boolean,
    onAsk: () -> Unit,
    onSettings: () -> Unit,
    modifier: Modifier = Modifier,
) {
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
            text = stringResource(
                if (blocked) R.string.sell_camera_blocked else R.string.sell_camera_why
            ),
            color = Color.White.copy(alpha = 0.6f),
            fontSize = 13.sp,
            modifier = Modifier.padding(top = 8.dp),
        )
        Box(
            Modifier
                .padding(top = 20.dp)
                .background(Accent, RoundedCornerShape(10.dp))
                .clickable(onClick = if (blocked) onSettings else onAsk)
                .padding(horizontal = 22.dp, vertical = 11.dp)
        ) {
            Text(
                stringResource(
                    if (blocked) R.string.sell_camera_settings else R.string.sell_camera_allow
                ),
                color = Color.White,
                fontSize = 14.sp,
            )
        }
    }
}

// ── CameraX plumbing ─────────────────────────────────────────────────────────

private fun bindCamera(
    view: PreviewView,
    lifecycleOwner: androidx.lifecycle.LifecycleOwner,
    facingBack: Boolean,
    executor: Executor,
    onBound: (VideoCapture<Recorder>, Camera?) -> Unit,
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
            // The bound Camera is kept, not discarded: torch and zoom are
            // properties of the *bound* camera, not of the capture use case,
            // and there is no other handle to them.
            val camera = provider.bindToLifecycle(lifecycleOwner, selector, preview, capture)
            onBound(capture, camera)
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
    maxMs: Long,
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
        .setDurationLimitMillis(maxMs)
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

/// Whether asking again would still show the system dialog.
///
/// False both before the first ask and after a permanent refusal, which is why
/// the caller pairs it with a record of having asked.
private fun Context.canAskAgain(permission: String): Boolean {
    val activity = findActivity() ?: return false
    return ActivityCompat.shouldShowRequestPermissionRationale(activity, permission)
}

private fun Context.openAppSettings() {
    startActivity(
        Intent(
            Settings.ACTION_APPLICATION_DETAILS_SETTINGS,
            Uri.fromParts("package", packageName, null),
        ).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
    )
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
