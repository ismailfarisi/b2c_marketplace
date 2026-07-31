package ae.marketplace.mobile.android.sell

import ae.marketplace.mobile.sell.SellViewModel
import android.content.Context
import android.net.Uri

/**
 * Reads a picked document and hands it to the shared view model.
 *
 * An Android extension rather than a method, because a `Uri` and a
 * `ContentResolver` are platform types and `SellViewModel` is common code that
 * iOS compiles too. The same split the recorder already makes: the platform
 * produces bytes, the shared layer decides what to do with them.
 */
fun SellViewModel.attachPickedDocument(context: Context, uri: String) {
    val parsed = Uri.parse(uri)
    val resolver = context.contentResolver

    // The resolver's type, not the file extension. A document picked from
    // Drive or a scanner app frequently has no extension at all, and the
    // presign is signed against the content type — so guessing it wrong
    // produces a 403 on upload that reads like an auth failure.
    val contentType = resolver.getType(parsed) ?: "application/octet-stream"

    val bytes = runCatching {
        resolver.openInputStream(parsed)?.use { it.readBytes() }
    }.getOrNull()

    if (bytes == null || bytes.isEmpty()) {
        // A picker can hand back a URI whose provider has already revoked
        // access — most often after a rotation. Reported as a failure rather
        // than ignored, because a tap that does nothing is indistinguishable
        // from a broken button.
        onDocumentUnreadable()
        return
    }

    // The vertical's key for the slot: "mulkiya", "permit". Server-supplied, so
    // this file never learns what a mulkiya is.
    val kind = state.value.form?.trust?.key ?: return
    attachDocument(kind, contentType, bytes)
}
