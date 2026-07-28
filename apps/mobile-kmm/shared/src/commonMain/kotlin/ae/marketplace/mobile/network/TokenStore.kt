package ae.marketplace.mobile.network

import ae.marketplace.mobile.PlatformContext

data class Tokens(val access: String, val refresh: String)

/**
 * Where the refresh token lives.
 *
 * An interface with a platform-backed implementation rather than one shared
 * file, because the only correct answer differs per platform and neither
 * implementation should be reachable from the other's binary: Android uses a
 * hardware-backed key through EncryptedSharedPreferences, iOS uses the Keychain.
 *
 * The web client (`apps/web/src/lib/token-store.ts`) makes the same split for
 * the same reason and settles for sessionStorage, which is the best a browser
 * offers without a same-origin backend route. Mobile has no such excuse.
 */
interface TokenStore {
    suspend fun load(): Tokens?
    suspend fun save(tokens: Tokens)
    suspend fun clear()
}

expect fun tokenStore(context: PlatformContext): TokenStore

/**
 * An in-memory store, for tests and for a deliberately signed-out session.
 *
 * Also the honest default for the *anonymous* case: the feed is browsable
 * without an account (`GET /feed` has `security: []`), so a client with no
 * tokens is a supported state rather than an error one.
 */
class InMemoryTokenStore(private var tokens: Tokens? = null) : TokenStore {
    override suspend fun load(): Tokens? = tokens
    override suspend fun save(tokens: Tokens) {
        this.tokens = tokens
    }

    override suspend fun clear() {
        tokens = null
    }
}
