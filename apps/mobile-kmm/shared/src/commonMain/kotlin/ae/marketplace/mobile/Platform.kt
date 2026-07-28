package ae.marketplace.mobile

/**
 * The platform handle the shared module needs to build anything stateful.
 *
 * A wrapper class rather than `actual typealias PlatformContext = Context`,
 * which is the pattern most tutorials show and which does not compile: `Context`
 * is abstract and an `expect class` is final, so the modalities disagree.
 * Widening the expect to `abstract` would fix Android and leave iOS with a class
 * nobody can instantiate.
 *
 * The wrapper costs one constructor call at app start and keeps `commonMain`
 * free of `android.*`, which is the whole point — every other platform
 * dependency in this module is reached through it.
 */
expect class PlatformContext

/** Whether the device is on a metered connection right now. */
expect class NetworkStatus {
    fun isMetered(): Boolean
    fun isConnected(): Boolean
}

expect fun networkStatus(context: PlatformContext): NetworkStatus

/** For the User-Agent and for crash grouping. Not used for behaviour. */
expect val platformName: String
