package ae.marketplace.mobile.sell

import ae.marketplace.mobile.model.LookupOptions
import ae.marketplace.mobile.model.SellDraft
import ae.marketplace.mobile.model.SellForm
import ae.marketplace.mobile.model.SellStatus
import ae.marketplace.mobile.model.SellVerticals
import io.ktor.client.HttpClient
import io.ktor.client.call.body
import io.ktor.client.request.get
import io.ktor.client.request.header
import io.ktor.client.request.parameter
import io.ktor.client.request.post
import io.ktor.client.request.put
import io.ktor.client.request.setBody
import io.ktor.client.statement.HttpResponse
import io.ktor.http.ContentType
import io.ktor.http.contentType
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * The sell endpoints.
 *
 * Two clients, not one, and the second one is the interesting half — see
 * [uploadVideo].
 */
class SellApi(
    private val client: HttpClient,
    /**
     * A bare client for the object store. See [uploadVideo]; a null disables
     * uploading rather than sending the video through the API client, because
     * the failure that produces is silent and confusing.
     */
    private val uploader: HttpClient? = null,
) {

    suspend fun verticals(): SellVerticals = client.get("sell/verticals").body()

    suspend fun form(vertical: String): SellForm =
        client.get("sell/verticals/$vertical/form").body()

    /**
     * The lookup field's typeahead.
     *
     * [source] arrives from the server as a path on the field. It is passed
     * through rather than matched against anything the client knows: the moment
     * this function contains "if the source is the car one", adding a vertical
     * whose lookup is neighbourhoods means an app release.
     */
    suspend fun lookup(source: String, query: String): LookupOptions =
        client.get(source.removePrefix("/v1/")) { parameter("q", query) }.body()

    /**
     * Creates the subject, the reel draft and the CTA, and returns a presigned
     * PUT.
     *
     * One call because the server composes it. The client could make the five
     * underlying calls itself and would lose a race it cannot see — a CTA has to
     * be attached before moderation begins, and moderation begins when the
     * object store's event lands.
     */
    suspend fun createDraft(body: SellDraftRequest): SellDraft =
        client.post("sell/drafts") {
            contentType(ContentType.Application.Json)
            setBody(body)
        }.body()

    /**
     * Sends the video to the object store.
     *
     * On a **separate client with no auth plugin**, and that is not tidiness.
     * The presigned URL carries its own signature in the query string; the API
     * client would attach an `Authorization: Bearer` header to it, and S3 reads
     * that header as an attempt at SigV4 header authentication and rejects the
     * request. The symptom is a 403 on upload with a perfectly valid token,
     * which is a genuinely hard afternoon.
     *
     * The API client is also wrong for a second reason: its request timeout is
     * ten seconds, chosen for a feed page. A twenty-megabyte video on a
     * congested cell will not finish in ten seconds, and the client would abort
     * it repeatedly with no error a seller could act on.
     *
     * [bytes] is the whole file in memory. Acceptable for a clip capped at sixty
     * seconds and a phone; it is the thing to change first when longer video
     * arrives, and the change is local to this function and its caller.
     */
    suspend fun uploadVideo(url: String, headers: Map<String, String>, bytes: ByteArray) {
        val http = uploader ?: error("no uploader configured")
        val response: HttpResponse = http.put(url) {
            headers.forEach { (k, v) -> header(k, v) }
            setBody(bytes)
        }
        // The presigned PUT is outside the API's problem+json contract, so the
        // status is checked here rather than by a response validator. A silent
        // failure would leave the reel waiting forever for bytes that are not
        // coming, and the seller watching a progress bar that already finished.
        if (response.status.value !in 200..299) {
            error("upload failed with ${response.status.value}")
        }
    }

    suspend fun publish(reelId: String): SellStatus =
        client.post("sell/drafts/$reelId/publish").body()

    suspend fun status(reelId: String): SellStatus =
        client.get("sell/reels/$reelId").body()

    // ── Trust documents ──────────────────────────────────────────────────────

    suspend fun requestDocumentUpload(
        subjectType: String,
        subjectId: String,
        kind: String,
        contentType: String,
    ): DocumentUpload = client.post("documents/uploads") {
        contentType(ContentType.Application.Json)
        setBody(DocumentUploadRequest(subjectType, subjectId, kind, contentType))
    }.body()

    // Shares uploadVideo's client and its reasoning about presigned PUTs; only
    // the name differs, and it differs because a caller reading `uploadVideo`
    // for a mulkiya would reasonably wonder what it was doing.
    suspend fun uploadDocument(url: String, headers: Map<String, String>, bytes: ByteArray) =
        uploadVideo(url, headers, bytes)

    suspend fun confirmDocument(documentId: String): TrustDocument =
        client.post("documents/$documentId/confirm").body()

    suspend fun documentsFor(subjectType: String, subjectId: String): TrustDocuments =
        client.get("subjects/$subjectType/$subjectId/documents").body()
}

@Serializable
data class SellDraftRequest(
    @SerialName("subject_type") val subjectType: String,
    /**
     * Field key → the seller's answer, as a string.
     *
     * Untyped on purpose, mirroring the server. A typed body would have to
     * enumerate every kind this seam might grow, in a file that must never learn
     * what a car is.
     */
    val values: Map<String, String>,
    val clock: String,
    @SerialName("window_start") val windowStart: String? = null,
    @SerialName("window_end") val windowEnd: String? = null,
    val lat: Double,
    val lng: Double,
    val region: String = "",
    val place: String = "",
    @SerialName("content_type") val contentType: String = "video/mp4",
)

/**
 * A presigned PUT for a trust document, and the row it belongs to.
 *
 * Deliberately the same shape as [SellDraft]'s upload fields. A document and a
 * reel are both "here is a URL, put bytes at it, then tell us" — and the
 * headers matter for the same reason in both: a presigned URL signs the headers
 * it was created with, so omitting Content-Type produces a signature mismatch
 * that reads like an auth failure and is not one.
 */
@Serializable
data class DocumentUpload(
    @SerialName("document_id") val documentId: String,
    @SerialName("upload_url") val uploadUrl: String,
    @SerialName("upload_headers") val uploadHeaders: Map<String, String> = emptyMap(),
)

@Serializable
data class TrustDocument(
    val id: String,
    val kind: String = "",
    /** pending | uploaded | approved | rejected */
    val status: String = "",
    /** Why it was refused. Shown to the uploader, who otherwise re-sends the same photograph. */
    val reason: String? = null,
)

@Serializable
data class TrustDocuments(val items: List<TrustDocument> = emptyList())

@Serializable
private data class DocumentUploadRequest(
    @SerialName("subject_type") val subjectType: String,
    @SerialName("subject_id") val subjectId: String,
    val kind: String,
    @SerialName("content_type") val contentType: String,
)
