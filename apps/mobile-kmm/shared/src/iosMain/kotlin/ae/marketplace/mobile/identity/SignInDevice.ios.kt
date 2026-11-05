package ae.marketplace.mobile.identity

import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.alloc
import kotlinx.cinterop.memScoped
import kotlinx.cinterop.ptr
import kotlinx.cinterop.toKString
import platform.UIKit.UIDevice
import platform.posix.uname
import platform.posix.utsname

/** The hardware identifier (iPhone15,2) where available, else UIKit's model ("iPhone"). */
@OptIn(ExperimentalForeignApi::class)
actual fun signInDevice(): SignInDevice {
    val machine = memScoped {
        val info = alloc<utsname>()
        if (uname(info.ptr) == 0) info.machine.toKString() else ""
    }
    // The simulator reports the Mac's architecture, which names nothing.
    val name = machine.takeIf { it.startsWith("iP") } ?: UIDevice.currentDevice.model
    return SignInDevice(platform = "ios", name = name)
}
