package ae.marketplace.mobile.network

import ae.marketplace.mobile.PlatformContext
import kotlinx.cinterop.ExperimentalForeignApi
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
@OptIn(ExperimentalForeignApi::class)
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
        if (status != noErr) return null
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
private fun Map<Any?, Any?>.toCFDictionary(): CFDictionaryRef? =
    platform.Foundation.NSDictionary.dictionaryWithDictionary(this) as CFDictionaryRef?

actual fun tokenStore(context: PlatformContext): TokenStore = KeychainTokenStore()
