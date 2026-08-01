package ae.marketplace.mobile.network

import ae.marketplace.mobile.PlatformContext
import kotlinx.cinterop.ExperimentalForeignApi
import kotlinx.cinterop.CPointer
import kotlinx.cinterop.ptr
import kotlinx.cinterop.reinterpret
import platform.CoreFoundation.CFDictionaryCreateMutable
import platform.CoreFoundation.CFDictionarySetValue
import platform.CoreFoundation.CFTypeRef
import platform.CoreFoundation.kCFAllocatorDefault
import platform.CoreFoundation.kCFTypeDictionaryKeyCallBacks
import platform.CoreFoundation.kCFTypeDictionaryValueCallBacks
import platform.Foundation.CFBridgingRetain
import platform.Foundation.NSNumber
import platform.Foundation.NSData
import platform.Foundation.NSString
import platform.Foundation.NSUTF8StringEncoding
import platform.Foundation.create
import platform.Foundation.dataUsingEncoding
import platform.Security.SecItemAdd
import platform.Security.SecItemCopyMatching
import platform.Security.SecItemDelete
import platform.Security.kSecAttrAccessible
import platform.Security.kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
import platform.Security.kSecAttrAccount
import platform.Security.kSecAttrService
import platform.Security.kSecClass
import platform.Security.kSecClassGenericPassword
import platform.Security.kSecReturnData
import platform.Security.kSecValueData
import kotlinx.cinterop.alloc
import kotlinx.cinterop.memScoped
import kotlinx.cinterop.ptr
import kotlinx.cinterop.value
import platform.CoreFoundation.CFDictionaryRef
import platform.CoreFoundation.CFTypeRefVar
import platform.darwin.noErr

/**
 * The Keychain, with `AfterFirstUnlockThisDeviceOnly`.
 *
 * Two parts of that accessibility class matter and both are deliberate.
 *
 * `AfterFirstUnlock` rather than `WhenUnlocked`, because a background refresh —
 * a push-triggered fetch, an app resumed from a locked screen — needs to read
 * the token, and `WhenUnlocked` makes that fail in a way that logs the user out
 * for reasons they will never be able to describe.
 *
 * `ThisDeviceOnly` because the alternative syncs the refresh token to iCloud
 * Keychain and therefore to every device on the account. A refresh token is a
 * bearer credential; replicating it is the opposite of what it is for.
 */
@OptIn(ExperimentalForeignApi::class, kotlinx.cinterop.BetaInteropApi::class)
internal class KeychainTokenStore : TokenStore {

    override suspend fun load(): Tokens? {
        val access = read(ACCESS) ?: return null
        val refresh = read(REFRESH) ?: return null
        return Tokens(access, refresh)
    }

    override suspend fun save(tokens: Tokens) {
        write(ACCESS, tokens.access)
        write(REFRESH, tokens.refresh)
    }

    override suspend fun clear() {
        delete(ACCESS)
        delete(REFRESH)
    }

    private fun query(account: String): Map<Any?, Any?> = mapOf(
        kSecClass to kSecClassGenericPassword,
        kSecAttrService to SERVICE,
        kSecAttrAccount to account,
    )

    private fun read(account: String): String? = memScoped {
        val result = alloc<CFTypeRefVar>()
        val q = (query(account) + mapOf(kSecReturnData to true)).toCFDictionary()
        val status = SecItemCopyMatching(q, result.ptr)
        // `noErr` is an Int32 in the Security bindings and SecItemCopyMatching
        // returns an OSStatus, which Kotlin/Native surfaces as UInt on this
        // platform. Comparing them directly does not compile; converting the
        // constant rather than the status keeps the negative error codes
        // intact, which matters because errSecItemNotFound is -25300 and
        // reinterpreting it as unsigned would make "no token" look like a
        // successful read.
        if (status.toLong() != noErr.toLong()) return null
        val data = result.value as? NSData ?: return null
        NSString.create(data = data, encoding = NSUTF8StringEncoding) as String?
    }

    private fun write(account: String, value: String) {
        delete(account)
        val data = (value as NSString).dataUsingEncoding(NSUTF8StringEncoding) ?: return
        val attributes = (
            query(account) + mapOf(
                kSecValueData to data,
                kSecAttrAccessible to kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly,
            )
            ).toCFDictionary()
        SecItemAdd(attributes, null)
    }

    private fun delete(account: String) {
        SecItemDelete(query(account).toCFDictionary())
    }

    private companion object {
        const val SERVICE = "ae.marketplace.auth"
        const val ACCESS = "access_token"
        const val REFRESH = "refresh_token"
    }
}

/**
 * Bridging a Kotlin map to a CFDictionary.
 *
 * Declared here rather than inlined at three call sites because the CoreFoundation
 * bridge is the one place in this file where a mistake is silent — a wrong type
 * produces a `null` query that matches nothing, and the symptom is a user who is
 * simply logged out with no error anywhere.
 */
@OptIn(ExperimentalForeignApi::class)
private fun Map<Any?, Any?>.toCFDictionary(): CFDictionaryRef? {
    // Built with CoreFoundation directly, not through NSDictionary.
    //
    // The original called `NSDictionary.dictionaryWithDictionary`, which is not
    // in the Kotlin/Native Foundation bindings — only the instance initialisers
    // are. It never compiled, and it survived because nothing on this machine
    // could build an Apple target until someone tried.
    //
    // The key and value callbacks are not optional. Passing null gives a
    // dictionary that neither retains nor compares its contents, so the kSec*
    // constants would be matched by pointer identity and the query would find
    // nothing — silently, which is exactly the failure this function's own
    // comment warns about.
    val dict = CFDictionaryCreateMutable(
        kCFAllocatorDefault,
        size.toLong(),
        kCFTypeDictionaryKeyCallBacks.ptr,
        kCFTypeDictionaryValueCallBacks.ptr,
    )
    for ((key, value) in this) {
        CFDictionarySetValue(dict, key.toCFRef(), value.toCFRef())
    }
    return dict
}

/**
 * Bridges one Kotlin value into a CoreFoundation reference.
 *
 * The kSec* constants arrive already as CFStringRef and pass straight through.
 * Everything else is a Kotlin String, Boolean or NSData that has to be bridged.
 *
 * `CFBridgingRetain` rather than a plain cast, because the dictionary holds the
 * reference and the bridged object has to outlive the Kotlin value it came
 * from — a query built from a local string that has since been collected is a
 * query against freed memory, and it would work in a test and fail under load.
 */
@OptIn(ExperimentalForeignApi::class)
private fun Any?.toCFRef(): CFTypeRef? = when (this) {
    null -> null
    is CPointer<*> -> reinterpret()
    is String -> CFBridgingRetain(this as NSString) as CFTypeRef?
    is Boolean -> CFBridgingRetain(NSNumber(bool = this)) as CFTypeRef?
    is NSData -> CFBridgingRetain(this) as CFTypeRef?
    else -> null
}

actual fun tokenStore(context: PlatformContext): TokenStore = KeychainTokenStore()
