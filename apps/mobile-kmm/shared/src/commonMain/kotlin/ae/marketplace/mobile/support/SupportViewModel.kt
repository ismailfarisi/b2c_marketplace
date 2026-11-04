package ae.marketplace.mobile.support

import ae.marketplace.mobile.network.Failure
import ae.marketplace.mobile.network.toFailure
import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlin.uuid.ExperimentalUuidApi
import kotlin.uuid.Uuid

data class HelpUiState(
    val tickets: List<SupportTicket> = emptyList(),
    val nextCursor: String? = null,
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    val loaded: Boolean = false,
    val error: Failure? = null,
    val subject: String = "",
    val body: String = "",
    /** Set when Send was pressed with a blank field. */
    val subjectMissing: Boolean = false,
    val bodyMissing: Boolean = false,
    val sending: Boolean = false,
    val sendError: Failure? = null,
)

data class HelpTicketUiState(
    val ticketId: String = "",
    val thread: SupportThread? = null,
    val loading: Boolean = false,
    val error: Failure? = null,
    val draft: String = "",
    val draftMissing: Boolean = false,
    val sending: Boolean = false,
    val sendError: Failure? = null,
)

data class InvoicesUiState(
    val items: List<InvoiceSummary> = emptyList(),
    val nextCursor: String? = null,
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    val loaded: Boolean = false,
    val error: Failure? = null,
)

/**
 * Native Help (list, new request, thread) and the invoice list.
 *
 * Drafts survive a failed send and keep their idempotency key until the text
 * changes or the send succeeds: a retry is the same request to the server, so
 * a tap after a timeout can't open the ticket twice.
 */
@OptIn(ExperimentalUuidApi::class)
class SupportViewModel(private val api: SupportApi) : ViewModel() {

    private val _help = MutableStateFlow(HelpUiState())
    val help: StateFlow<HelpUiState> = _help.asStateFlow()

    private val _ticket = MutableStateFlow(HelpTicketUiState())
    val ticket: StateFlow<HelpTicketUiState> = _ticket.asStateFlow()

    private val _invoices = MutableStateFlow(InvoicesUiState())
    val invoices: StateFlow<InvoicesUiState> = _invoices.asStateFlow()

    private var openKey: String? = null
    private var replyKey: String? = null

    private fun newKey() = Uuid.random().toString()

    /** Drops the account that just signed out. */
    fun forget() {
        _help.value = HelpUiState()
        _ticket.value = HelpTicketUiState()
        _invoices.value = InvoicesUiState()
        openKey = null
        replyKey = null
    }

    // ── Help list + new request ──────────────────────────────────────────────

    fun loadTickets(force: Boolean = false) {
        val s = _help.value
        if (s.loading || (s.loaded && !force)) return
        _help.value = s.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { api.tickets() }
                .onSuccess { p ->
                    _help.value = _help.value.copy(
                        tickets = p.items, nextCursor = p.nextCursor,
                        loading = false, loaded = true,
                    )
                }
                .onFailure { e -> _help.value = _help.value.copy(loading = false, error = e.toFailure()) }
        }
    }

    fun loadOlderTickets() {
        val s = _help.value
        val cursor = s.nextCursor ?: return
        if (s.loadingMore) return
        _help.value = s.copy(loadingMore = true, error = null)
        viewModelScope.launch {
            runCatching { api.tickets(cursor) }
                .onSuccess { p ->
                    val seen = _help.value.tickets.map { it.id }.toSet()
                    _help.value = _help.value.copy(
                        tickets = _help.value.tickets + p.items.filter { it.id !in seen },
                        nextCursor = p.nextCursor, loadingMore = false,
                    )
                }
                .onFailure { e -> _help.value = _help.value.copy(loadingMore = false, error = e.toFailure()) }
        }
    }

    fun onSubjectChanged(text: String) {
        openKey = null
        _help.value = _help.value.copy(subject = text, subjectMissing = false, sendError = null)
    }

    fun onBodyChanged(text: String) {
        openKey = null
        _help.value = _help.value.copy(body = text, bodyMissing = false, sendError = null)
    }

    /** Opens the request; [onOpened] gets the new ticket's id. */
    fun submitTicket(onOpened: (String) -> Unit) {
        val s = _help.value
        if (s.sending) return
        val subject = s.subject.trim()
        val body = s.body.trim()
        if (subject.isEmpty() || body.isEmpty()) {
            _help.value = s.copy(subjectMissing = subject.isEmpty(), bodyMissing = body.isEmpty())
            return
        }
        val key = openKey ?: newKey().also { openKey = it }
        _help.value = s.copy(sending = true, sendError = null)
        viewModelScope.launch {
            runCatching { api.open(subject, body, key) }
                .onSuccess { thread ->
                    openKey = null
                    _help.value = _help.value.copy(
                        subject = "", body = "", sending = false, loaded = false,
                    )
                    _ticket.value = HelpTicketUiState(ticketId = thread.id, thread = thread)
                    loadTickets(force = true)
                    onOpened(thread.id)
                }
                .onFailure { e -> _help.value = _help.value.copy(sending = false, sendError = e.toFailure()) }
        }
    }

    // ── One ticket ───────────────────────────────────────────────────────────

    fun openTicket(id: String) {
        val keep = _ticket.value.takeIf { it.ticketId == id }
        if (keep == null) replyKey = null
        _ticket.value = (keep ?: HelpTicketUiState(ticketId = id)).copy(loading = keep?.thread == null, error = null)
        viewModelScope.launch {
            runCatching { api.thread(id) }
                .onSuccess { t -> if (_ticket.value.ticketId == id) _ticket.value = _ticket.value.copy(thread = t, loading = false) }
                .onFailure { e -> if (_ticket.value.ticketId == id) _ticket.value = _ticket.value.copy(loading = false, error = e.toFailure()) }
        }
    }

    fun retryTicket() {
        _ticket.value.ticketId.takeIf { it.isNotEmpty() }?.let(::openTicket)
    }

    fun onReplyChanged(text: String) {
        replyKey = null
        _ticket.value = _ticket.value.copy(draft = text, draftMissing = false, sendError = null)
    }

    fun sendReply() {
        val s = _ticket.value
        if (s.sending || s.ticketId.isEmpty()) return
        val body = s.draft.trim()
        if (body.isEmpty()) {
            _ticket.value = s.copy(draftMissing = true)
            return
        }
        val key = replyKey ?: newKey().also { replyKey = it }
        val id = s.ticketId
        _ticket.value = s.copy(sending = true, sendError = null)
        viewModelScope.launch {
            runCatching { api.reply(id, body, key) }
                .onSuccess { t ->
                    replyKey = null
                    _ticket.value = _ticket.value.copy(thread = t, draft = "", sending = false)
                    _help.value = _help.value.copy(loaded = false)
                }
                .onFailure { e -> _ticket.value = _ticket.value.copy(sending = false, sendError = e.toFailure()) }
        }
    }

    // ── Invoices ─────────────────────────────────────────────────────────────

    fun loadInvoices(force: Boolean = false) {
        val s = _invoices.value
        if (s.loading || (s.loaded && !force)) return
        _invoices.value = s.copy(loading = true, error = null)
        viewModelScope.launch {
            runCatching { api.invoices() }
                .onSuccess { p -> _invoices.value = InvoicesUiState(items = p.items, nextCursor = p.nextCursor, loaded = true) }
                .onFailure { e -> _invoices.value = _invoices.value.copy(loading = false, error = e.toFailure()) }
        }
    }

    fun loadOlderInvoices() {
        val s = _invoices.value
        val cursor = s.nextCursor ?: return
        if (s.loadingMore) return
        _invoices.value = s.copy(loadingMore = true, error = null)
        viewModelScope.launch {
            runCatching { api.invoices(cursor) }
                .onSuccess { p ->
                    val seen = _invoices.value.items.map { it.id }.toSet()
                    _invoices.value = _invoices.value.copy(
                        items = _invoices.value.items + p.items.filter { it.id !in seen },
                        nextCursor = p.nextCursor, loadingMore = false,
                    )
                }
                .onFailure { e -> _invoices.value = _invoices.value.copy(loadingMore = false, error = e.toFailure()) }
        }
    }
}
