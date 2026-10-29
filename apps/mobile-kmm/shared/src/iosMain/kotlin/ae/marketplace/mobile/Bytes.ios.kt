package ae.marketplace.mobile

import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.addressOf
import kotlinx.cinterop.usePinned
import platform.Foundation.NSData
import platform.posix.memcpy

/**
 * Copies raw bytes from Swift into a Kotlin [ByteArray] in one operation.
 *
 * # Why this exists on the Kotlin side
 *
 * Swift can construct a `KotlinByteArray` and set elements on it, and the
 * obvious bridge does exactly that in a loop. For a sixty-second clip that is
 * twenty million calls across the Objective-C boundary, each a dynamic
 * dispatch — seconds of main-thread work on the tap that is supposed to feel
 * like finishing, and it gets worse with a longer clip.
 *
 * `usePinned` fixes the array in memory for the duration of the block, which is
 * what makes a single `memcpy` legal: without it the garbage collector may move
 * the backing store while the pointer is live. That is the whole reason this
 * cannot be written in Swift.
 *
 * The copy itself is unavoidable — the bytes start in a `Data` and have to end
 * in a Kotlin array — and is the reason `SellViewModel.onClipRecorded` takes
 * bytes rather than a file path: a path would be an `NSURL` on one platform and
 * a `Uri` on the other, and a shared abstraction over them would be a third
 * thing neither platform uses.
 */
@OptIn(ExperimentalForeignApi::class)
fun toByteArray(data: NSData): ByteArray {
    // Takes NSData rather than a raw pointer: `CPointer` parameters are not
    // exported to Objective-C, so Swift could not call a pointer version.
    val out = ByteArray(data.length.toInt())
    val source = data.bytes ?: return out
    if (out.isEmpty()) return out
    out.usePinned { pinned ->
        memcpy(pinned.addressOf(0), source, out.size.toULong())
    }
    return out
}
