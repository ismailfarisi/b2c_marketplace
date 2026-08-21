package ae.marketplace.mobile.android.me

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.ui.EmptyBlock
import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.FailureStrip
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Shape
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.me.MeViewModel
import ae.marketplace.mobile.model.Conversation
import ae.marketplace.mobile.model.Message
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.defaultMinSize
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Send
import androidx.compose.material3.Icon
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp

/**
 * The inbox.
 *
 * This is the other half of the CTA. "Message seller" has been on every reel in
 * the feed since Phase 3 and until recently it recorded a click and went
 * nowhere — so the conversion the whole video bet is measured on had no
 * destination in the app at all.
 */
@Composable
fun InboxScreen(
    viewModel: MeViewModel,
    onOpen: (Conversation) -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val state by viewModel.inbox.collectAsState()
    LaunchedEffect(Unit) { viewModel.loadInbox() }

    // The system back gesture, which did nothing here. Messaging is drawn over
    // the tabs rather than being one, so Android's back had no idea it was open
    // and swiping took the viewer out of the app from what looks like a
    // second-level screen.
    BackHandler(onBack = onBack)

    Column(modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding()) {
        Header(stringResource(R.string.me_messages), onBack)
        when {
            state.loading && state.items.isEmpty() -> LoadingBlock()

            state.error != null && state.items.isEmpty() ->
                FailureBlock(state.error!!, onRetry = viewModel::loadInbox)

            state.items.isEmpty() ->
                EmptyBlock(
                    message = stringResource(R.string.inbox_empty),
                    hint = stringResource(R.string.inbox_empty_hint),
                )

            else -> LazyColumn {
                state.error?.let { item { FailureStrip(it, onRetry = viewModel::loadInbox) } }
                items(state.items, key = { it.id }) { c -> InboxRow(c) { onOpen(c) } }
            }
        }
    }
}

@Composable
private fun InboxRow(c: Conversation, onClick: () -> Unit) {
    val title = c.listingTitle.ifEmpty { stringResource(R.string.inbox_untitled) }
    Row(
        Modifier
            .fillMaxWidth()
            .clickable(onClick = onClick)
            .defaultMinSize(minHeight = Space.Touch)
            .padding(horizontal = Space.Edge, vertical = 14.dp)
            .semantics(mergeDescendants = true) { contentDescription = title },
        verticalAlignment = Alignment.CenterVertically,
    ) {
        // The avatar is the conversation's initial rather than a photo: there
        // is no seller-picture endpoint, and a grey silhouette repeated down
        // the list would be decoration pretending to be information.
        Box(
            contentAlignment = Alignment.Center,
            modifier = Modifier
                .size(44.dp)
                .background(Palette.Surface, RoundedCornerShape(50)),
        ) {
            Text(
                text = title.first().uppercase(),
                color = Palette.TextMuted,
                fontSize = Type.Heading,
                fontWeight = FontWeight.SemiBold,
            )
        }
        Column(Modifier.weight(1f).padding(start = Space.Medium)) {
            Text(
                text = title,
                color = Palette.Text,
                fontSize = Type.Body,
                fontWeight = if (c.unread > 0) FontWeight.SemiBold else FontWeight.Normal,
            )
            if (c.state != "open" && c.state.isNotEmpty()) {
                // A closed or blocked thread is stated. A buyer typing into a
                // thread the seller closed and getting silence is worse than
                // being told.
                Text(
                    text = c.state,
                    color = Palette.TextFaint,
                    fontSize = Type.Caption,
                    modifier = Modifier.padding(top = Space.Hair),
                )
            }
        }
        if (c.unread > 0) {
            Box(
                Modifier.size(20.dp).background(Palette.Accent, RoundedCornerShape(50)),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = if (c.unread > 9) "9+" else "${c.unread}",
                    color = Color.White,
                    fontSize = 10.sp,
                    fontWeight = FontWeight.Bold,
                )
            }
        }
    }
}

/**
 * One conversation.
 *
 * Three things here are not cosmetic.
 *
 * **Bubbles have sides.** `mine` is computed by the server, because the client
 * has no idea which uuid is its own. Without it every bubble rendered
 * identically down the left, which is a transcript with the speaker labels torn
 * off — unreadable the moment a thread is longer than two messages.
 *
 * **There is no optimistic bubble.** The message that appears is the one the
 * server returned. A message can come back `held`, meaning a moderator has it
 * and it has *not* been delivered, or `redacted`, meaning a phone number was
 * stripped out of it. Drawing what the sender typed and moving on would
 * misreport both, and a sender who thinks a held message arrived concludes they
 * are being ignored and goes looking for a way off the platform — which is
 * precisely what the redaction exists to prevent.
 *
 * **The draft survives a failure.** Losing what someone typed because the
 * network dropped is the most annoying failure a messaging screen has.
 */
@Composable
fun ThreadScreen(viewModel: MeViewModel, onBack: () -> Unit, modifier: Modifier = Modifier) {
    val state by viewModel.thread.collectAsState()
    val listState = rememberLazyListState()
    val sendLabel = stringResource(R.string.thread_send)

    BackHandler(onBack = onBack)

    LaunchedEffect(state.messages.size) {
        if (state.messages.isNotEmpty()) listState.animateScrollToItem(state.messages.lastIndex)
    }

    Column(
        modifier
            .fillMaxSize()
            .background(Palette.Ink)
            .statusBarsPadding()
            .imePadding(),
    ) {
        Header(state.title.ifEmpty { stringResource(R.string.me_messages) }, onBack)

        Box(Modifier.weight(1f)) {
            when {
                state.loading -> LoadingBlock()

                // A thread that failed to load is not an empty thread. Showing
                // "No messages yet" over a request that 500'd invites the viewer
                // to type their question a second time into a conversation that
                // already has it.
                state.error != null && state.messages.isEmpty() ->
                    FailureBlock(state.error!!)

                state.messages.isEmpty() ->
                    EmptyBlock(
                        message = stringResource(R.string.thread_empty),
                        hint = stringResource(R.string.thread_empty_hint),
                    )

                else -> LazyColumn(
                    state = listState,
                    contentPadding = PaddingValues(vertical = Space.Small),
                ) {
                    items(state.messages, key = { it.id }) { Bubble(it) }
                }
            }
        }

        // A send failure, above the box the text is still sitting in. Retry is
        // the send button itself, which is right there.
        state.error?.takeIf { state.messages.isNotEmpty() }?.let {
            FailureStrip(it, onRetry = viewModel::sendDraft)
        }

        Row(
            Modifier
                .navigationBarsPadding()
                .fillMaxWidth()
                .padding(horizontal = Space.Medium, vertical = 10.dp),
            verticalAlignment = Alignment.Bottom,
            horizontalArrangement = Arrangement.spacedBy(Space.Small),
        ) {
            Box(
                Modifier
                    .weight(1f)
                    .background(Palette.Surface, RoundedCornerShape(22.dp))
                    .padding(horizontal = 14.dp, vertical = 11.dp),
            ) {
                if (state.draft.isEmpty()) {
                    Text(
                        stringResource(R.string.thread_hint),
                        color = Palette.TextGhost,
                        fontSize = 14.sp,
                    )
                }
                BasicTextField(
                    value = state.draft,
                    onValueChange = viewModel::onDraftChanged,
                    textStyle = TextStyle(color = Palette.Text, fontSize = 14.sp),
                    cursorBrush = SolidColor(Palette.Accent),
                    // Deliberately multi-line, capped. Someone describing a car
                    // they want to see writes three sentences, and a single-line
                    // field turns that into a horizontally scrolling slot they
                    // cannot proof-read before sending.
                    maxLines = 5,
                    modifier = Modifier.fillMaxWidth(),
                )
            }
            val canSend = state.draft.isNotBlank() && !state.sending
            Box(
                Modifier
                    .size(44.dp)
                    .background(
                        if (canSend) Palette.Accent else Palette.Surface,
                        RoundedCornerShape(50),
                    )
                    .clickable(enabled = canSend) { viewModel.sendDraft() }
                    .semantics { contentDescription = sendLabel },
                contentAlignment = Alignment.Center,
            ) {
                Icon(
                    imageVector = Icons.AutoMirrored.Filled.Send,
                    contentDescription = null,
                    tint = if (canSend) Color.White else Palette.TextFaint,
                    modifier = Modifier.size(18.dp),
                )
            }
        }
    }
}

@Composable
private fun Bubble(m: Message) {
    Column(
        Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = Space.Tight),
        horizontalAlignment = if (m.mine) Alignment.End else Alignment.Start,
    ) {
        Box(
            Modifier
                .widthIn(max = 300.dp)
                .background(
                    // The viewer's own messages carry the accent, muted. A
                    // saturated fill on the sender's side is the convention, but
                    // this accent is also the price colour and the CTA colour,
                    // and at full strength a chat would be the loudest screen in
                    // an app whose subject is video.
                    if (m.mine) Palette.Accent.copy(alpha = 0.22f) else Palette.Surface,
                    RoundedCornerShape(
                        topStart = 20.dp,
                        topEnd = 20.dp,
                        // The corner nearest the speaker is squared off. It is
                        // the cheapest possible way to say which side a message
                        // is on, and it survives being read in bright sunlight
                        // where a subtle fill difference does not.
                        bottomStart = if (m.mine) 20.dp else Space.Tight,
                        bottomEnd = if (m.mine) Space.Tight else 20.dp,
                    ),
                )
                .padding(horizontal = 13.dp, vertical = 9.dp),
        ) {
            Text(
                text = m.body,
                color = Palette.Text.copy(alpha = if (m.state == "held") 0.5f else 0.92f),
                fontSize = 14.sp,
            )
        }
        // The state, in the sender's own words, under the bubble it belongs to.
        stateNote(m)?.let {
            Text(
                text = it,
                color = Palette.Accent.copy(alpha = 0.85f),
                fontSize = 10.sp,
                modifier = Modifier.padding(top = 3.dp),
            )
        }
    }
}

@Composable
private fun stateNote(m: Message): String? = when (m.state) {
    "held" -> stringResource(R.string.msg_held)
    "redacted" -> stringResource(R.string.msg_redacted)
    "blocked" -> stringResource(R.string.msg_blocked)
    else -> null
}

@Composable
private fun Header(title: String, onBack: () -> Unit) {
    val backLabel = stringResource(R.string.cd_back)
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 8.dp, vertical = Space.Medium),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Box(
            contentAlignment = Alignment.Center,
            modifier = Modifier
                .size(44.dp)
                .clickable(onClick = onBack)
                .semantics { contentDescription = backLabel },
        ) {
            Icon(
                imageVector = Icons.AutoMirrored.Filled.ArrowBack,
                contentDescription = null,
                tint = Palette.Text,
                modifier = Modifier.size(24.dp),
            )
        }
        Text(
            text = title,
            color = Palette.Text,
            fontSize = Type.Heading,
            fontWeight = FontWeight.SemiBold,
        )
    }
}
