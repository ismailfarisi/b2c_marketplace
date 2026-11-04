package ae.marketplace.mobile.android.push

import ae.marketplace.mobile.android.BuildConfig
import ae.marketplace.mobile.android.MainActivity
import ae.marketplace.mobile.android.MarketplaceApp
import ae.marketplace.mobile.android.R
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import com.google.firebase.FirebaseApp
import com.google.firebase.FirebaseOptions
import com.google.firebase.messaging.FirebaseMessaging
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch

object Push {
    const val CHANNEL = "meals"
    const val EXTRA_LINK = "push_link"

    /** The FCM data key, as the tray delivers it for a background push. */
    const val FCM_LINK = "link"
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    /** False when the build has no Firebase project, in which case push is off. */
    fun start(context: Context): Boolean {
        if (BuildConfig.FIREBASE_APP_ID.isEmpty()) return false
        if (FirebaseApp.getApps(context).isEmpty()) {
            FirebaseApp.initializeApp(
                context,
                FirebaseOptions.Builder()
                    .setApplicationId(BuildConfig.FIREBASE_APP_ID)
                    .setApiKey(BuildConfig.FIREBASE_API_KEY)
                    .setProjectId(BuildConfig.FIREBASE_PROJECT_ID)
                    .setGcmSenderId(BuildConfig.FIREBASE_SENDER_ID)
                    .build(),
            )
        }
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(
                CHANNEL,
                context.getString(R.string.push_channel_name),
                NotificationManager.IMPORTANCE_HIGH,
            ),
        )
        return true
    }

    fun token(onToken: suspend (String) -> Unit) {
        FirebaseMessaging.getInstance().token.addOnSuccessListener { token ->
            scope.launch { onToken(token) }
        }
    }

    internal fun register(context: Context, token: String) {
        val app = context.applicationContext as MarketplaceApp
        scope.launch { app.sdk.await().registerPushToken(token, "android") }
    }
}

/**
 * FCM shows notification messages itself while the app is in the background;
 * this draws them while it is open, and keeps the token current.
 */
class PushService : FirebaseMessagingService() {
    override fun onNewToken(token: String) = Push.register(this, token)

    override fun onMessageReceived(message: RemoteMessage) {
        val n = message.notification ?: return
        val open = Intent(this, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP
            message.data["link"]?.let { putExtra(Push.EXTRA_LINK, it) }
        }
        val tap = PendingIntent.getActivity(
            this, message.messageId.hashCode(), open,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        val notification = NotificationCompat.Builder(this, Push.CHANNEL)
            .setSmallIcon(android.R.drawable.stat_notify_chat)
            .setContentTitle(n.title)
            .setContentText(n.body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(n.body))
            .setAutoCancel(true)
            .setContentIntent(tap)
            .build()
        runCatching {
            NotificationManagerCompat.from(this).notify(message.messageId.hashCode(), notification)
        }
    }
}
