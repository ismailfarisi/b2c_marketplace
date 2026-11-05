package ae.marketplace.mobile.android.support

import ae.marketplace.mobile.android.R
import ae.marketplace.mobile.android.meals.money
import ae.marketplace.mobile.android.ui.EmptyBlock
import ae.marketplace.mobile.android.ui.FailureBlock
import ae.marketplace.mobile.android.ui.LoadingBlock
import ae.marketplace.mobile.android.ui.Palette
import ae.marketplace.mobile.android.ui.Space
import ae.marketplace.mobile.android.ui.Type
import ae.marketplace.mobile.android.ui.words
import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.support.InvoiceDocument
import ae.marketplace.mobile.support.SupportViewModel
import android.content.Intent
import androidx.activity.compose.BackHandler
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
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.statusBarsPadding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle
import java.util.Locale

private val Dubai: ZoneId = ZoneId.of("Asia/Dubai")

@Composable
private fun arabic(): Boolean =
    LocalContext.current.resources.configuration.locales[0].language == "ar"

/** Activity time in Dubai, so a 01:00 reply isn't dated the day before. */
private fun whenText(iso: String, arabic: Boolean, withTime: Boolean): String = runCatching {
    val locale = if (arabic) Locale("ar") else Locale.ENGLISH
    val f = if (withTime) DateTimeFormatter.ofLocalizedDateTime(FormatStyle.MEDIUM, FormatStyle.SHORT)
    else DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
    Instant.parse(iso).atZone(Dubai).format(f.withLocale(locale))
}.getOrDefault(iso.take(10))

@Composable
private fun statusLabel(status: String): String = when (status) {
    "open" -> stringResource(R.string.help_status_open)
    "answered" -> stringResource(R.string.help_status_answered)
    "closed" -> stringResource(R.string.help_status_closed)
    else -> status
}

@Composable
private fun Header(title: String, onBack: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = 14.dp, vertical = Space.Medium),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            // Points back in the reading direction.
            text = if (arabic()) "›" else "‹",
            color = Palette.Text,
            fontSize = 26.sp,
            modifier = Modifier
                .clickable(onClick = onBack)
                .defaultMinSize(minWidth = Space.Touch, minHeight = Space.Touch)
                .semantics { contentDescription = "Back" }
                .padding(end = Space.Medium),
        )
        Text(title, color = Palette.Text, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold)
    }
}

@Composable
private fun Field(value: String, hint: String, onChange: (String) -> Unit, lines: Int, error: String?) {
    Column(Modifier.padding(horizontal = Space.Edge, vertical = Space.Tight)) {
        Box(
            Modifier
                .fillMaxWidth()
                .border(1.dp, if (error != null) Palette.Bad else Palette.Line, RoundedCornerShape(12.dp))
                .padding(horizontal = 14.dp, vertical = 11.dp),
        ) {
            if (value.isEmpty()) Text(hint, color = Palette.TextGhost, fontSize = 14.sp)
            BasicTextField(
                value = value,
                onValueChange = onChange,
                textStyle = TextStyle(color = Palette.Text, fontSize = 14.sp),
                cursorBrush = SolidColor(Palette.Accent),
                minLines = lines,
                maxLines = lines + 3,
                modifier = Modifier.fillMaxWidth(),
            )
        }
        error?.let { Text(it, color = Palette.Bad, fontSize = Type.Secondary) }
    }
}

/** A send failure in words, plus that the text is still there. */
@Composable
private fun SendFailure(f: Failure) {
    val words = (f as? Failure.Rejected)?.detail?.takeIf { !arabic() } ?: f.words()
    val kept = if (f is Failure.Rejected) "" else " " + stringResource(R.string.help_kept)
    Text(
        words + kept,
        color = Palette.Bad,
        fontSize = Type.Secondary,
        modifier = Modifier.padding(horizontal = Space.Edge, vertical = Space.Tight),
    )
}

@Composable
private fun SendButton(label: String, enabled: Boolean, onClick: () -> Unit) {
    Box(
        Modifier
            .padding(horizontal = Space.Edge, vertical = Space.Small)
            .background(if (enabled) Palette.Accent else Palette.Accent.copy(alpha = 0.3f), RoundedCornerShape(20.dp))
            .clickable(enabled = enabled, onClick = onClick)
            .defaultMinSize(minWidth = Space.Touch, minHeight = Space.Touch)
            .padding(horizontal = Space.Large, vertical = 11.dp),
        contentAlignment = Alignment.Center,
    ) {
        Text(label, color = Color.White, fontSize = 14.sp, fontWeight = FontWeight.Medium)
    }
}

@Composable
private fun MoreButton(loading: Boolean, onClick: () -> Unit) {
    Text(
        stringResource(R.string.help_older),
        color = Palette.Accent,
        fontSize = Type.Secondary,
        modifier = Modifier
            .clickable(enabled = !loading, onClick = onClick)
            .defaultMinSize(minHeight = Space.Touch)
            .padding(horizontal = Space.Edge, vertical = 12.dp),
    )
}

/** Help: new request form and the customer's requests, natively, on the app's session. */
@Composable
fun HelpScreen(viewModel: SupportViewModel, onBack: () -> Unit, onOpenTicket: (String) -> Unit) {
    val s by viewModel.help.collectAsState()
    BackHandler(onBack = onBack)
    LaunchedEffect(Unit) { viewModel.loadTickets() }

    Column(Modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding().imePadding()) {
        Header(stringResource(R.string.me_help), onBack)
        LazyColumn(Modifier.weight(1f)) {
            item {
                Text(
                    stringResource(R.string.help_lede),
                    color = Palette.TextMuted,
                    fontSize = Type.Secondary,
                    modifier = Modifier.padding(horizontal = Space.Edge, vertical = Space.Small),
                )
            }
            if (s.loading && !s.loaded) {
                item { LoadingBlock(inline = true) }
            } else if (s.error is Failure.Unauthorized) {
                item { FailureBlock(s.error!!) }
            } else {
                item {
                    Text(
                        stringResource(R.string.help_new),
                        color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.padding(horizontal = Space.Edge, vertical = Space.Small),
                    )
                    val rejected = s.sendError as? Failure.Rejected
                    Field(
                        s.subject, stringResource(R.string.help_subject), viewModel::onSubjectChanged, 1,
                        if (s.subjectMissing) stringResource(R.string.help_need_subject) else rejected?.fields?.get("subject"),
                    )
                    Field(
                        s.body, stringResource(R.string.help_message), viewModel::onBodyChanged, 4,
                        if (s.bodyMissing) stringResource(R.string.help_need_message) else rejected?.fields?.get("body"),
                    )
                    s.sendError?.takeIf { rejected == null || rejected.fields.isEmpty() }?.let { SendFailure(it) }
                    SendButton(
                        stringResource(if (s.sending) R.string.help_sending else R.string.help_send),
                        enabled = !s.sending,
                    ) { viewModel.submitTicket(onOpenTicket) }
                    Text(
                        stringResource(R.string.help_mine),
                        color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.padding(start = Space.Edge, end = Space.Edge, top = Space.XL, bottom = Space.Tight),
                    )
                }
                if (s.error != null && s.tickets.isEmpty()) {
                    item { FailureBlock(s.error!!, onRetry = { viewModel.loadTickets(force = true) }) }
                } else if (s.loaded && s.tickets.isEmpty()) {
                    item { EmptyBlock(message = stringResource(R.string.help_empty)) }
                }
                items(s.tickets, key = { it.id }) { t ->
                    val ar = arabic()
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable { onOpenTicket(t.id) }
                            .defaultMinSize(minHeight = Space.Touch)
                            .padding(horizontal = Space.Edge, vertical = 12.dp),
                        horizontalArrangement = Arrangement.spacedBy(Space.Small),
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text(t.subject, color = Palette.Text, fontSize = Type.Body)
                            Text(whenText(t.updatedAt, ar, false), color = Palette.TextFaint, fontSize = Type.Caption)
                        }
                        Text(
                            statusLabel(t.status),
                            color = if (t.status == "answered") Palette.Accent else Palette.TextMuted,
                            fontSize = Type.Secondary,
                        )
                    }
                }
                if (s.nextCursor != null) item { MoreButton(s.loadingMore) { viewModel.loadOlderTickets() } }
                if (s.error != null && s.tickets.isNotEmpty()) item { SendFailure(s.error!!) }
            }
            item { Box(Modifier.navigationBarsPadding().padding(bottom = Space.XL)) }
        }
    }
}

/** One request: the thread and a reply box; a reply reopens a closed ticket. */
@Composable
fun HelpTicketScreen(viewModel: SupportViewModel, onBack: () -> Unit) {
    val s by viewModel.ticket.collectAsState()
    val ar = arabic()
    BackHandler(onBack = onBack)

    Column(Modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding().imePadding()) {
        Header(s.thread?.subject ?: stringResource(R.string.me_help), onBack)
        Box(Modifier.weight(1f)) {
            val thread = s.thread
            when {
                s.loading && thread == null -> LoadingBlock()
                s.error is Failure.NotFound -> EmptyBlock(message = stringResource(R.string.help_not_found))
                s.error != null && thread == null -> FailureBlock(s.error!!, onRetry = viewModel::retryTicket)
                thread != null -> LazyColumn {
                    item {
                        Text(
                            statusLabel(thread.status),
                            color = Palette.TextMuted, fontSize = Type.Secondary,
                            modifier = Modifier.padding(horizontal = Space.Edge),
                        )
                    }
                    items(thread.messages, key = { it.id }) { m ->
                        Column(
                            Modifier
                                .fillMaxWidth()
                                .padding(horizontal = Space.Edge, vertical = Space.Tight)
                                .background(
                                    if (m.fromStaff) Palette.Surface else Palette.Accent.copy(alpha = 0.18f),
                                    RoundedCornerShape(14.dp),
                                )
                                .padding(horizontal = 13.dp, vertical = 9.dp),
                        ) {
                            Text(
                                stringResource(if (m.fromStaff) R.string.help_staff else R.string.help_you) +
                                    " · " + whenText(m.createdAt, ar, true),
                                color = Palette.TextFaint, fontSize = Type.Caption,
                            )
                            Text(m.body, color = Palette.Text, fontSize = 14.sp)
                        }
                    }
                }
            }
        }
        if (s.thread != null) {
            val rejected = s.sendError as? Failure.Rejected
            s.sendError?.takeIf { rejected == null || rejected.fields.isEmpty() }?.let { SendFailure(it) }
            Field(
                s.draft, stringResource(R.string.help_reply_hint), viewModel::onReplyChanged, 2,
                if (s.draftMissing) stringResource(R.string.help_need_message) else rejected?.fields?.get("body"),
            )
            Box(Modifier.navigationBarsPadding()) {
                SendButton(
                    stringResource(if (s.sending) R.string.help_sending else R.string.help_send),
                    enabled = !s.sending,
                    onClick = viewModel::sendReply,
                )
            }
        }
    }
}

private fun invoiceDay(iso: String, arabic: Boolean): String = runCatching {
    val f = DateTimeFormatter.ofLocalizedDate(FormatStyle.LONG).withLocale(if (arabic) Locale("ar") else Locale.ENGLISH)
    if (iso.length > 10) Instant.parse(iso).atZone(Dubai).format(f) else LocalDate.parse(iso).format(f)
}.getOrDefault(iso.take(10))

@Composable
private fun kindLabel(kind: String): String = when (kind) {
    "tax_invoice" -> stringResource(R.string.invoice_tax_invoice)
    "simplified" -> stringResource(R.string.invoice_simplified)
    "credit_note" -> stringResource(R.string.invoice_credit_note)
    else -> kind
}

/** The customer's tax invoices; each opens natively on the app's own session. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun InvoicesScreen(viewModel: SupportViewModel, onBack: () -> Unit, onOpenInvoice: (String) -> Unit) {
    val s by viewModel.invoices.collectAsState()
    val ar = arabic()
    BackHandler(onBack = onBack)
    // Every visit, not once per process: an invoice issued while the app was
    // open must show up without a restart. Keeps the list on screen meanwhile.
    LaunchedEffect(Unit) { viewModel.loadInvoices(force = true) }

    Column(Modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding()) {
        Header(stringResource(R.string.me_invoices), onBack)
        PullToRefreshBox(
            isRefreshing = s.refreshing,
            onRefresh = { viewModel.loadInvoices(force = true) },
            modifier = Modifier.weight(1f),
        ) {
            LazyColumn(Modifier.fillMaxSize()) {
                when {
                    s.loading && !s.loaded -> item { LoadingBlock(inline = true) }
                    s.error != null && s.items.isEmpty() ->
                        item { FailureBlock(s.error!!, onRetry = { viewModel.loadInvoices(force = true) }) }
                    s.items.isEmpty() -> item { EmptyBlock(message = stringResource(R.string.invoices_empty)) }
                    else -> item {
                        Text(
                            stringResource(R.string.invoices_print_hint),
                            color = Palette.TextMuted, fontSize = Type.Secondary,
                            modifier = Modifier.padding(horizontal = Space.Edge, vertical = Space.Small),
                        )
                    }
                }
                items(s.items, key = { it.id }) { inv ->
                    Row(
                        Modifier
                            .fillMaxWidth()
                            .clickable { onOpenInvoice(inv.id) }
                            .defaultMinSize(minHeight = Space.Touch)
                            .padding(horizontal = Space.Edge, vertical = 12.dp),
                        horizontalArrangement = Arrangement.spacedBy(Space.Small),
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text(inv.number, color = Palette.Text, fontSize = Type.Body)
                            val date = runCatching {
                                LocalDate.parse(inv.supplyDate.take(10))
                                    .format(DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM).withLocale(if (ar) Locale("ar") else Locale.ENGLISH))
                            }.getOrDefault(inv.supplyDate)
                            Text(
                                date + if (inv.kind == "credit_note") " · " + stringResource(R.string.invoices_credit) else "",
                                color = Palette.TextFaint, fontSize = Type.Caption,
                            )
                        }
                        Text(money(inv.total.amount, inv.total.currency), color = Palette.Text, fontSize = Type.Body)
                    }
                }
                if (s.nextCursor != null) item { MoreButton(s.loadingMore) { viewModel.loadOlderInvoices() } }
                // A failed refresh or older page with the list still showing.
                if (s.error != null && s.items.isNotEmpty()) item { SendFailure(s.error!!) }
                item { Box(Modifier.navigationBarsPadding().padding(bottom = Space.XL)) }
            }
        }
    }
}

@Composable
private fun InvoiceRow(label: String, value: String, strong: Boolean = false) {
    Row(
        Modifier.fillMaxWidth().padding(horizontal = Space.Edge, vertical = 3.dp),
        horizontalArrangement = Arrangement.spacedBy(Space.Small),
    ) {
        Text(
            label,
            color = if (strong) Palette.Text else Palette.TextMuted,
            fontSize = if (strong) Type.Body else Type.Secondary,
            fontWeight = if (strong) FontWeight.SemiBold else FontWeight.Normal,
            modifier = Modifier.weight(1f),
        )
        Text(
            value,
            color = Palette.Text,
            fontSize = if (strong) Type.Body else Type.Secondary,
            fontWeight = if (strong) FontWeight.SemiBold else FontWeight.Normal,
        )
    }
}

@Composable
private fun SectionTitle(text: String) {
    Text(
        text,
        color = Palette.Text, fontSize = Type.Body, fontWeight = FontWeight.SemiBold,
        modifier = Modifier.padding(start = Space.Edge, end = Space.Edge, top = Space.Large, bottom = Space.Tight),
    )
}

/** The invoice as plain text for the share sheet (mail, WhatsApp, a notes app). */
private fun shareText(inv: InvoiceDocument, ar: Boolean, words: (Int) -> String): String = buildString {
    val m = { minor: Long -> money(minor, inv.currency) }
    val kind = when (inv.kind) {
        "tax_invoice" -> words(R.string.invoice_tax_invoice)
        "simplified" -> words(R.string.invoice_simplified)
        "credit_note" -> words(R.string.invoice_credit_note)
        else -> inv.kind
    }
    appendLine("$kind ${inv.number}")
    when (inv.status) {
        "cancelled" -> appendLine(words(R.string.invoice_cancelled))
        "superseded" -> appendLine(words(R.string.invoice_superseded))
    }
    appendLine("${words(R.string.invoice_issued)}: ${invoiceDay(inv.issuedAt, ar)}")
    appendLine("${words(R.string.invoice_supply)}: ${invoiceDay(inv.supplyDate, ar)}")
    inv.correctsNumber?.let { appendLine("${words(R.string.invoice_corrects)}: $it") }
    for ((label, p) in listOf(words(R.string.invoice_supplier) to inv.supplier, words(R.string.invoice_buyer) to inv.buyer)) {
        appendLine()
        appendLine("$label: ${p.name.ifBlank { "—" }}")
        if (p.address.isNotBlank()) appendLine(p.address)
        if (p.trn.isNotBlank()) appendLine("${words(R.string.invoice_trn)}: ${p.trn}")
    }
    appendLine()
    for (l in inv.lines) {
        appendLine("${l.label(ar)} — ${l.quantityText} × ${m(l.unitNetMinor)}")
        appendLine("  ${words(R.string.invoice_vat_total)} ${l.ratePercent}%: ${m(l.taxMinor)} · ${words(R.string.invoice_line_total)}: ${m(l.grossMinor)}")
    }
    appendLine()
    appendLine("${words(R.string.invoice_subtotal)}: ${m(inv.netMinor)}")
    appendLine("${words(R.string.invoice_vat_total)}: ${m(inv.taxMinor)}")
    append("${words(R.string.invoice_grand)}: ${m(inv.grossMinor)}")
}

/** One invoice in full, from the stored snapshot; shares as text. */
@Composable
fun InvoiceDetailScreen(viewModel: SupportViewModel, onBack: () -> Unit) {
    val s by viewModel.invoice.collectAsState()
    val ar = arabic()
    val context = LocalContext.current
    BackHandler(onBack = onBack)

    Column(Modifier.fillMaxSize().background(Palette.Ink).statusBarsPadding()) {
        Header(s.invoice?.number ?: stringResource(R.string.invoice_title), onBack)
        Box(Modifier.weight(1f)) {
            val inv = s.invoice
            when {
                s.loading && inv == null -> LoadingBlock()
                s.error is Failure.NotFound -> EmptyBlock(message = stringResource(R.string.invoice_not_found))
                s.error != null && inv == null -> FailureBlock(s.error!!, onRetry = viewModel::retryInvoice)
                inv != null -> LazyColumn(Modifier.fillMaxSize()) {
                    item {
                        val m = { minor: Long -> money(minor, inv.currency) }
                        Text(
                            kindLabel(inv.kind),
                            color = Palette.Text, fontSize = Type.Heading, fontWeight = FontWeight.SemiBold,
                            modifier = Modifier.padding(horizontal = Space.Edge, vertical = Space.Tight),
                        )
                        if (inv.status != "issued") {
                            Text(
                                stringResource(if (inv.status == "cancelled") R.string.invoice_cancelled else R.string.invoice_superseded),
                                color = Palette.Bad, fontSize = Type.Secondary, fontWeight = FontWeight.Medium,
                                modifier = Modifier.padding(horizontal = Space.Edge),
                            )
                        }
                        InvoiceRow(stringResource(R.string.invoice_number), inv.number)
                        InvoiceRow(stringResource(R.string.invoice_issued), invoiceDay(inv.issuedAt, ar))
                        InvoiceRow(stringResource(R.string.invoice_supply), invoiceDay(inv.supplyDate, ar))
                        inv.correctsNumber?.let { InvoiceRow(stringResource(R.string.invoice_corrects), it) }

                        for ((label, p) in listOf(
                            stringResource(R.string.invoice_supplier) to inv.supplier,
                            stringResource(R.string.invoice_buyer) to inv.buyer,
                        )) {
                            SectionTitle(label)
                            Text(p.name.ifBlank { "—" }, color = Palette.Text, fontSize = Type.Body,
                                modifier = Modifier.padding(horizontal = Space.Edge))
                            if (p.address.isNotBlank()) Text(p.address, color = Palette.TextMuted, fontSize = Type.Secondary,
                                modifier = Modifier.padding(horizontal = Space.Edge))
                            if (p.trn.isNotBlank()) Text("${stringResource(R.string.invoice_trn)}: ${p.trn}",
                                color = Palette.TextMuted, fontSize = Type.Secondary,
                                modifier = Modifier.padding(horizontal = Space.Edge))
                        }

                        SectionTitle(stringResource(R.string.invoice_title))
                        inv.lines.forEach { l ->
                            Column(
                                Modifier
                                    .fillMaxWidth()
                                    .padding(horizontal = Space.Edge, vertical = Space.Tight)
                                    .border(1.dp, Palette.Line, RoundedCornerShape(12.dp))
                                    .padding(vertical = Space.Small),
                            ) {
                                Text(l.label(ar), color = Palette.Text, fontSize = Type.Body,
                                    modifier = Modifier.padding(horizontal = Space.Edge))
                                InvoiceRow("${l.quantityText} × ${m(l.unitNetMinor)}", "")
                                InvoiceRow(stringResource(R.string.invoice_net), m(l.netMinor))
                                InvoiceRow("${stringResource(R.string.invoice_vat_total)} (${l.ratePercent}%)", m(l.taxMinor))
                                InvoiceRow(stringResource(R.string.invoice_line_total), m(l.grossMinor), strong = true)
                            }
                        }

                        Box(Modifier.padding(top = Space.Large))
                        InvoiceRow(stringResource(R.string.invoice_subtotal), m(inv.netMinor))
                        InvoiceRow(stringResource(R.string.invoice_vat_total), m(inv.taxMinor))
                        InvoiceRow(stringResource(R.string.invoice_grand), m(inv.grossMinor), strong = true)

                        val shareLabel = stringResource(R.string.invoice_share)
                        SendButton(shareLabel, enabled = true) {
                            val text = shareText(inv, ar) { context.getString(it) }
                            val send = Intent(Intent.ACTION_SEND).apply {
                                type = "text/plain"
                                putExtra(Intent.EXTRA_SUBJECT, inv.number)
                                putExtra(Intent.EXTRA_TEXT, text)
                            }
                            runCatching { context.startActivity(Intent.createChooser(send, shareLabel)) }
                        }
                        Box(Modifier.navigationBarsPadding().padding(bottom = Space.XL))
                    }
                }
            }
        }
    }
}
