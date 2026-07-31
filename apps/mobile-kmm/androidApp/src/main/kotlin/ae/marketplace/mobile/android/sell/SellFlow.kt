package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.sell.SellStep
import ae.marketplace.mobile.sell.SellViewModel
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color

/**
 * The Sell tab, end to end.
 *
 * The step machine lives in `:shared` — this composable only chooses which
 * screen to draw for the step the view model is in. That split is the same one
 * the feed makes and it matters more here: the ordering constraints in this flow
 * (a subject before a reel, a CTA before any bytes, metadata before the subject
 * publishes) are exactly the kind of thing that would be got subtly differently
 * right on two platforms.
 */
@Composable
fun SellFlow(
    viewModel: SellViewModel,
    onLeave: () -> Unit,
    onSeeFeed: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.state.collectAsState()

    LaunchedEffect(Unit) { viewModel.start() }

    Box(modifier.fillMaxSize().background(Ink)) {
        when (state.step) {
            SellStep.Record -> RecordScreen(
                onRecorded = viewModel::onClipRecorded,
                onClose = onLeave,
                // The vertical's own advice, once one is chosen. Empty on the
                // first pass, because nothing has been picked yet and a generic
                // "record a video" helps nobody.
                tip = state.form?.tip.orEmpty(),
            )

            SellStep.Vertical -> VerticalStep(
                state = state,
                onPick = viewModel::onVerticalPicked,
                onBack = viewModel::onBack,
            )

            SellStep.Details -> DetailsStep(
                state = state,
                onValue = viewModel::onValueChanged,
                onLookupQuery = viewModel::onLookupQuery,
                onLookupPick = viewModel::onLookupPicked,
                onNext = viewModel::onDetailsSubmitted,
                onBack = viewModel::onBack,
            )

            SellStep.Clock -> ClockStep(
                state = state,
                onClock = viewModel::onClockPicked,
                onPlace = { viewModel.onPlacePicked("Dubai", it.label, it.lat, it.lng) },
                onPublish = viewModel::onPublish,
                onBack = viewModel::onBack,
            )

            SellStep.Live -> LiveStep(
                state = state,
                onRetry = viewModel::retryUpload,
                onRetryPublish = viewModel::retryPublish,
                onSeeFeed = {
                    // Reset before leaving, so the tab is a camera again next
                    // time. Coming back to a finished live screen would make the
                    // Sell tab look like a receipt rather than a recorder — and
                    // it would hold the clip's bytes for the life of the process.
                    viewModel.reset()
                    onSeeFeed()
                },
                onDone = {
                    viewModel.reset()
                    onLeave()
                },
            )
        }
    }
}
