package ae.marketplace.mobile

import kotlinx.cinterop.CPointer
import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.addressOf
import kotlinx.cinterop.usePinned
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
fun copyInto(target: ByteArray, source: CPointer<*>, count: Int) {
    if (count <= 0 || target.isEmpty()) return
    // Never copy more than the destination holds. The caller passes a count
    // taken from the source, and a mismatch here would be an out-of-bounds
    // write into the heap rather than an exception.
    val safe = minOf(count, target.size)
    target.usePinned { pinned ->
        memcpy(pinned.addressOf(0), source, safe.toULong())
    }
}
