package ae.marketplace.mobile.support

import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.request.parameter
import io.ktor.client.request.post
import io.ktor.client.request.setBody
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class SupportTicket(
    val id: String,
    val subject: String,
    /** open | answered | closed */
    val status: String,
    @SerialName("created_at") val createdAt: String,
    @SerialName("updated_at") val updatedAt: String,
)

@Serializable
data class SupportMessage(
    val id: String,
    @SerialName("from_staff") val fromStaff: Boolean,
    val body: String,
    @SerialName("created_at") val createdAt: String,
)

@Serializable
data class SupportThread(
    val id: String,
    val subject: String,
    val status: String,
    @SerialName("updated_at") val updatedAt: String,
    val messages: List<SupportMessage> = emptyList(),
)

@Serializable
data class SupportTicketPage(
    val items: List<SupportTicket> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

@Serializable
data class InvoiceAmount(val amount: Long, val currency: String)

@Serializable
data class InvoiceSummary(
    val id: String,
    val number: String,
    /** tax_invoice | simplified | credit_note */
    val kind: String,
    val total: InvoiceAmount,
    @SerialName("supply_date") val supplyDate: String,
    @SerialName("issued_at") val issuedAt: String,
    val status: String,
)

@Serializable
data class InvoicePage(
    val items: List<InvoiceSummary> = emptyList(),
    @SerialName("next_cursor") val nextCursor: String? = null,
)

/**
 * Help tickets and tax invoices for the signed-in customer, the same
 * `/me/support/tickets` and `/me/invoices` the web uses.
 *
 * Sends take the caller's [idempotencyKey], held for the life of a draft, so a
 * second tap after a timeout replays the first request instead of opening a
 * second ticket.
 */
class SupportApi(private val client: HttpClient) {

    suspend fun tickets(cursor: String? = null): SupportTicketPage =
        client.get("me/support/tickets") { cursor?.let { parameter("cursor", it) } }.body()

    suspend fun open(subject: String, body: String, idempotencyKey: String): SupportThread =
        client.post("me/support/tickets") {
            header("Idempotency-Key", idempotencyKey)
            contentType(ContentType.Application.Json)
            setBody(OpenBody(subject, body))
        }.body()

    suspend fun thread(id: String): SupportThread = client.get("me/support/tickets/$id").body()

    suspend fun reply(id: String, body: String, idempotencyKey: String): SupportThread =
        client.post("me/support/tickets/$id/messages") {
            header("Idempotency-Key", idempotencyKey)
            contentType(ContentType.Application.Json)
            setBody(ReplyBody(body))
        }.body()

    suspend fun invoices(cursor: String? = null): InvoicePage =
        client.get("me/invoices") { cursor?.let { parameter("cursor", it) } }.body()
}

@Serializable private data class OpenBody(val subject: String, val body: String)
@Serializable private data class ReplyBody(val body: String)
