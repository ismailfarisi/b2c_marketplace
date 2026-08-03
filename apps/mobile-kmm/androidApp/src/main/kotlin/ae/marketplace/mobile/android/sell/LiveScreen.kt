package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.words
import ae.marketplace.mobile.model.TrustUpload
import ae.marketplace.mobile.sell.DocumentPhase
import ae.marketplace.mobile.sell.SellUiState
import ae.marketplace.mobile.sell.TrustState
import ae.marketplace.mobile.sell.UploadPhase
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.annotation.StringRes
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
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The last screen: what actually happened.
 *
 * The mock calls this "You're live in Al Quoz". It is not always true, and the
 * screen says which. A reel in `review_pending` is with a human; telling the
 * seller it is live produces the worst possible sequence — they share the link,
 * their friend sees nothing, and they conclude the platform is broken.
 *
 * Every author on this path is new in author_trust, so *all* of it goes to a
 * person before publish. That means the honest headline here is the review one
 * far more often than the live one, and the counters start and stay at zero
 * until a moderator gets to it. Showing them anyway is deliberate: it is the
 * shape of the screen the seller will come back to.
 */
@Composable
fun LiveStep(
    onPickDocument: (String) -> Unit,
    state: SellUiState,
    onRetry: () -> Unit,
    onRetryPublish: () -> Unit,
    onSeeFeed: () -> Unit,
    onDone: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(modifier.fillMaxSize().background(Ink)) {
        Column(
            Modifier
                .weight(1f)
                .fillMaxWidth()
                .statusBarsPadding()
                .padding(horizontal = 18.dp, vertical = 16.dp),
        ) {
            val phase = phaseOf(state)

            Row(verticalAlignment = Alignment.Top) {
                Text(
                    text = phase.glyph,
                    fontSize = 20.sp,
                    color = if (phase.bad) Accent else Palette.Good,
                    modifier = Modifier.padding(end = 12.dp, top = 2.dp),
                )
                Column {
                    Text(
                        text = phase.headline,
                        color = Color.White,
                        fontSize = 19.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                    Text(
                        text = phase.detail,
                        color = Color.White.copy(alpha = 0.55f),
                        fontSize = 13.sp,
                        modifier = Modifier.padding(top = 4.dp),
                    )
                }
            }

            Column(Modifier.padding(top = 28.dp)) {
                val counters = state.status?.counters
                StatRow(stringResource(R.string.sell_stat_views), counters?.views)
                StatRow(stringResource(R.string.sell_stat_saves), counters?.saves)
                StatRow(stringResource(R.string.sell_stat_comments), counters?.comments)
            }

            // The button repairs whichever half actually broke. Retrying the
            // upload after a refused publish would send the seller to fix the
            // one part that worked.
            val repair: Pair<Int, () -> Unit>? = when (state.upload) {
                UploadPhase.Failed -> R.string.sell_retry_upload to onRetry
                UploadPhase.Refused -> R.string.sell_retry_publish to onRetryPublish
                else -> null
            }
            repair?.let { (label, action) ->
                Box(
                    Modifier
                        .padding(top = 24.dp)
                        .fillMaxWidth()
                        .border(1.dp, Accent, RoundedCornerShape(10.dp))
                        .clickable(onClick = action)
                        .padding(vertical = 13.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        stringResource(label),
                        color = Accent, fontSize = 14.sp, fontWeight = FontWeight.Medium,
                    )
                }
            }

            state.error?.let {
                Text(
                    text = it.words(),
                    color = Accent.copy(alpha = 0.9f),
                    fontSize = 12.sp,
                    modifier = Modifier.padding(top = 14.dp),
                )
            }

            // Dropped once the reel is actually in the feed. Leaving "reviewed
            // by a person before they appear" under "You're live" contradicts
            // the headline, and the seller has to decide which half to believe.
            if (state.reviewNote.isNotEmpty() && state.status?.state != "published") {
                Text(
                    // Verbatim from the server. It is the sentence that stops a
                    // seller expecting their video in ninety seconds and getting
                    // it in two hours from writing a one-star review about a
                    // working system.
                    text = state.reviewNote,
                    color = Color.White.copy(alpha = 0.35f),
                    fontSize = 11.sp,
                    modifier = Modifier.padding(top = 24.dp),
                )
            }

            // The trust document, offered here rather than on the form.
            //
            // It could not be anywhere else: a document attaches to a subject,
            // and the subject does not exist until the draft is created — which
            // happens when the seller taps Publish. Asking earlier would mean
            // holding the bytes in memory through three screens to attach them
            // to a row that might never exist.
            //
            // It also happens to be the right moment. The seller has just
            // finished and is looking at a screen that says their listing is
            // with a reviewer; "add your mulkiya and earn Verified" is a useful
            // thing to do with that minute, and it is the only minute they will
            // ever be this engaged.
            state.form?.trust?.let { trust ->
                TrustDocumentRow(
                    trust = trust,
                    state = state.trust,
                    onPick = onPickDocument,
                )
            }
        }

        Column(
            Modifier.navigationBarsPadding().padding(horizontal = 18.dp, vertical = 12.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            // Only offered once the reel is actually in the feed. A "See it in
            // the feed" button on a reel awaiting review sends the seller to
            // look for something that is not there.
            if (state.status?.state == "published") {
                PrimaryButton(
                    label = stringResource(R.string.sell_see_in_feed),
                    enabled = true,
                    onClick = onSeeFeed,
                )
            }
            Box(
                Modifier
                    .fillMaxWidth()
                    .border(1.dp, Line, RoundedCornerShape(10.dp))
                    .clickable(onClick = onDone)
                    .padding(vertical = 14.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(stringResource(R.string.sell_done), color = Color.White, fontSize = 15.sp)
            }
        }
    }
}

@Composable
private fun StatRow(label: String, value: Long?) {
    Row(
        Modifier.fillMaxWidth().padding(vertical = 7.dp),
        horizontalArrangement = Arrangement.SpaceBetween,
    ) {
        Text(label, color = Color.White.copy(alpha = 0.6f), fontSize = 14.sp)
        Text(
            // A dash, not a zero, before the first status lands. Zero is a
            // measurement; a dash is "not measured yet", and a seller who reads
            // an unfetched zero as a real one concludes their reel died.
            text = value?.toString() ?: "—",
            color = Color.White,
            fontSize = 14.sp,
            fontWeight = FontWeight.Medium,
        )
    }
}

private data class Phase(
    val glyph: String,
    val headline: String,
    val detail: String,
    val bad: Boolean = false,
)

/**
 * The one place a pipeline state becomes a sentence.
 *
 * The server sends the state raw and this maps it, which is the right split:
 * the states are a fact about the pipeline, and their wording is copy that
 * differs per locale. An unrecognised state falls through to the processing
 * message rather than to an error — a newer server with a new intermediate
 * state should read as "still working", not as "broken".
 */
@Composable
private fun phaseOf(state: SellUiState): Phase {
    if (state.upload == UploadPhase.Uploading) {
        return Phase("↑", stringResource(R.string.sell_uploading), stringResource(R.string.sell_uploading_detail))
    }
    if (state.upload == UploadPhase.Failed) {
        return Phase("!", stringResource(R.string.sell_upload_failed), stringResource(R.string.sell_upload_failed_detail), bad = true)
    }
    if (state.upload == UploadPhase.Refused) {
        // The video is up. What was refused is the listing, and saying so is
        // the difference between a seller retrying the right thing and giving
        // up on a video they have already successfully sent.
        return Phase("!", stringResource(R.string.sell_publish_refused), stringResource(R.string.sell_publish_refused_detail), bad = true)
    }

    return when (state.status?.state) {
        "published" -> Phase(
            "✓",
            stringResource(R.string.sell_live_in, state.place.ifEmpty { "—" }),
            stringResource(R.string.sell_live_detail),
        )
        "review_pending" -> Phase(
            "◷",
            stringResource(R.string.sell_in_review),
            stringResource(R.string.sell_in_review_detail),
        )
        "rejected" -> Phase(
            "✕",
            stringResource(R.string.sell_rejected),
            state.status?.failure?.detail?.takeIf { it.isNotEmpty() }
                ?: stringResource(R.string.sell_rejected_detail),
            bad = true,
        )
        "failed" -> Phase(
            "!",
            stringResource(R.string.sell_processing_failed),
            state.status?.failure?.detail?.takeIf { it.isNotEmpty() }
                ?: stringResource(R.string.sell_processing_failed_detail),
            bad = true,
        )
        else -> Phase(
            "◌",
            stringResource(R.string.sell_processing),
            stringResource(R.string.sell_processing_detail),
        )
    }
}

/**
 * "Add mulkiya to earn Verified", and a way to actually do it.
 *
 * This row existed from the first build of the sell flow and did nothing. It
 * said so — "Document upload is not built yet" — which is better than a control
 * that silently fails, and is still the product asking for something it cannot
 * receive.
 *
 * # Why the states are what they are
 *
 * Five, and none of them says "Verified". The badge is the reviewer's to grant;
 * this screen reports what has happened to the document, which is a different
 * fact. Telling a seller they are Verified the moment bytes land is the same
 * mistake as calling a reel live while it is with a moderator — and this one
 * would be worse, because the badge is the thing a buyer trusts.
 */
@Composable
private fun TrustDocumentRow(
    trust: TrustUpload,
    state: TrustState,
    onPick: (String) -> Unit,
) {
    // The picker constrains itself to what the vertical accepts, which the
    // server declared. No `image/*` wildcard: the reviewer's tooling cannot
    // open a HEIC, and letting one through produces a document nobody can look
    // at and a seller who is told, days later, to send it again.
    val mimes = trust.accept.ifEmpty { listOf("image/jpeg", "image/png", "application/pdf") }
    val picker = rememberLauncherForActivityResult(
        ActivityResultContracts.OpenDocument()
    ) { uri -> uri?.let { onPick(it.toString()) } }

    Column(
        Modifier
            .padding(top = 24.dp)
            .fillMaxWidth()
            .border(1.dp, Line, RoundedCornerShape(10.dp))
            .padding(horizontal = 14.dp, vertical = 14.dp),
    ) {
        Text(trust.label, color = Color.White.copy(alpha = 0.75f), fontSize = 13.sp)

        when (state.phase) {
            DocumentPhase.Uploading -> Text(
                text = stringResource(R.string.trust_uploading),
                color = Color.White.copy(alpha = 0.45f),
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 8.dp),
            )

            DocumentPhase.Submitted -> Text(
                text = stringResource(R.string.trust_submitted),
                color = Color.White.copy(alpha = 0.45f),
                fontSize = 11.sp,
                modifier = Modifier.padding(top = 8.dp),
            )

            DocumentPhase.Approved -> Text(
                // The only place the promised badge is named, and only once it
                // is real.
                text = stringResource(R.string.trust_approved, trust.earns),
                color = Palette.Good,
                fontSize = 11.sp,
                fontWeight = FontWeight.SemiBold,
                modifier = Modifier.padding(top = 8.dp),
            )

            DocumentPhase.Rejected -> {
                Text(
                    // The reviewer's own words. A document refused with no
                    // explanation is a seller who sends the same photograph
                    // again, and then a third time.
                    text = state.reason ?: stringResource(R.string.trust_rejected),
                    color = Palette.Bad,
                    fontSize = 11.sp,
                    modifier = Modifier.padding(top = 8.dp),
                )
                TrustButton(R.string.trust_try_again) { picker.launch(mimes.toTypedArray()) }
            }

            DocumentPhase.Failed -> {
                Text(
                    text = state.error?.words() ?: stringResource(R.string.err_unknown),
                    color = Palette.Bad,
                    fontSize = 11.sp,
                    modifier = Modifier.padding(top = 8.dp),
                )
                TrustButton(R.string.trust_try_again) { picker.launch(mimes.toTypedArray()) }
            }

            DocumentPhase.Idle ->
                TrustButton(R.string.trust_choose) { picker.launch(mimes.toTypedArray()) }
        }
    }
}

@Composable
private fun TrustButton(@StringRes label: Int, onClick: () -> Unit) {
    Box(
        Modifier
            .padding(top = 10.dp)
            .border(1.dp, Accent.copy(alpha = 0.6f), RoundedCornerShape(8.dp))
            .clickable(onClick = onClick)
            .defaultMinSize(minHeight = Space.Touch)
            .padding(horizontal = 16.dp, vertical = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(stringResource(label), color = Accent, fontSize = 13.sp)
    }
}
