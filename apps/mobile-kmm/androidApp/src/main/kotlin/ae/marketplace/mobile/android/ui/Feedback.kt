package ae.marketplace.mobile.android.ui

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.isRetryable
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp

/**
 * The three things a list can be other than a list, drawn the same way
 * everywhere.
 *
 * Loading, empty and failed were hand-drawn per screen before this, and the
 * inconsistencies were not cosmetic. Saved showed a centred spinner, Me showed
 * one inset by 40dp, the inbox showed none at all. Two screens rendered an
 * error as body text in the accent colour with no way to retry; one rendered it
 * as a full-screen replacement, so a failed refresh wiped a list that was
 * perfectly good a second ago.
 */

/**
 * Words for a [Failure], chosen by what the viewer can do about it.
 *
 * The mapping is here and not in shared code deliberately: the classification
 * is platform-neutral and belongs in the view model, and the *words* are
 * Android resources so they can be translated. That split is what makes an
 * Arabic build possible without touching a view model.
 */
@Composable
fun Failure.words(): String = when (this) {
    Failure.Offline -> stringResource(R.string.err_offline)
    Failure.Timeout -> stringResource(R.string.err_timeout)
    Failure.Unauthorized -> stringResource(R.string.err_unauthorized)
    Failure.Forbidden -> stringResource(R.string.err_forbidden)
    Failure.NotFound -> stringResource(R.string.err_not_found)
    is Failure.RateLimited -> stringResource(R.string.err_rate_limited)
    // The server's sentence when it wrote one for a human, and the generic
    // otherwise. A 422 from this API usually carries a detail that is better
    // than anything a client could invent — "This offer has ended", "Outside
    // this kitchen's delivery area" — and throwing it away to show "Invalid
    // request" would be discarding the most useful string in the response.
    is Failure.Rejected -> detail ?: stringResource(R.string.err_rejected)
    is Failure.Server -> stringResource(R.string.err_server)
    Failure.Unknown -> stringResource(R.string.err_unknown)
}

/**
 * A failed load, with a way out.
 *
 * The retry button is conditional on [Failure.isRetryable], because a button
 * that cannot possibly work is worse than none: it teaches the viewer that
 * tapping things in this app does nothing, and they stop tapping the ones that
 * do.
 */
@Composable
fun FailureBlock(
    failure: Failure,
    onRetry: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxWidth()
            .padding(horizontal = Space.XXL, vertical = Space.XXL),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = failure.words(),
            color = Palette.TextMuted,
            fontSize = Type.Secondary,
            textAlign = TextAlign.Center,
        )
        if (onRetry != null && failure.isRetryable) {
            Box(Modifier.padding(top = Space.Large)) {
                SecondaryButton(stringResource(R.string.action_retry), onRetry)
            }
        }
        // The request id, quietly, for a 500 only.
        //
        // It is the one thing that makes a support conversation about a server
        // error tractable, and it costs a line of 11sp grey text. Showing it for
        // an offline error would be noise about a failure that never reached a
        // server to be given an id.
        (failure as? Failure.Server)?.requestId?.let {
            Text(
                text = it,
                color = Palette.TextFaint,
                fontSize = Type.Caption,
                modifier = Modifier.padding(top = Space.Medium),
            )
        }
    }
}

/**
 * A failure that must not take the screen with it.
 *
 * Drawn as a strip above content that is still on display. This is the right
 * shape when a *refresh* fails: the list the viewer is looking at is still the
 * best information available, and replacing it with an error message destroys
 * something useful to report something they can already see — that the new data
 * did not arrive.
 */
@Composable
fun FailureStrip(
    failure: Failure,
    onRetry: (() -> Unit)? = null,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier
            .fillMaxWidth()
            .background(Palette.Surface)
            .padding(horizontal = Space.Edge, vertical = Space.Medium),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(Space.Medium),
    ) {
        Text(
            text = failure.words(),
            color = Palette.TextMuted,
            fontSize = Type.Caption,
            modifier = Modifier.weight(1f),
        )
        if (onRetry != null && failure.isRetryable) {
            Text(
                text = stringResource(R.string.action_retry),
                color = Palette.Accent,
                fontSize = Type.Caption,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier
                    .clickable(onClick = onRetry)
                    .defaultMinSize(minHeight = Space.Touch)
                    .padding(vertical = Space.Medium),
            )
        }
    }
}

/**
 * Nothing here, and that is not an error.
 *
 * [hint] is a second line saying what would put something here. An empty state
 * that only says "Nothing saved" is a dead end; one that says how to save
 * something is the cheapest onboarding in the product.
 */
@Composable
fun EmptyBlock(
    message: String,
    hint: String? = null,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier
            .fillMaxWidth()
            .padding(horizontal = Space.XXL, vertical = Space.XXL),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Text(
            text = message,
            color = Palette.TextMuted,
            fontSize = Type.Secondary,
            textAlign = TextAlign.Center,
        )
        hint?.let {
            Text(
                text = it,
                color = Palette.TextFaint,
                fontSize = Type.Caption,
                textAlign = TextAlign.Center,
                modifier = Modifier.padding(top = Space.Small),
            )
        }
    }
}

@Composable
fun LoadingBlock(modifier: Modifier = Modifier, inline: Boolean = false) {
    Box(
        modifier
            .fillMaxWidth()
            .then(if (inline) Modifier.padding(vertical = Space.XXL) else Modifier.fillMaxSize()),
        contentAlignment = Alignment.Center,
    ) {
        CircularProgressIndicator(color = Palette.Accent, strokeWidth = 2.dp)
    }
}

@Composable
fun SecondaryButton(label: String, onClick: () -> Unit, modifier: Modifier = Modifier) {
    Box(
        modifier
            .border(1.dp, Palette.Line, RoundedCornerShape(Shape.Field))
            .clickable(onClick = onClick)
            .defaultMinSize(minHeight = Space.Touch)
            .padding(horizontal = Space.XL, vertical = Space.Medium),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, color = Palette.Text, fontSize = Type.Secondary)
    }
}
