# ChimePort — Android alpha

Native client: Kotlin + Jetpack Compose / Material 3. Connects to any HTTPS ChimePort server, with multiple server accounts and an offline message cache. Not a WebView.

## Build and release

Build with Android SDK 35, JDK 17, Gradle 8.11.1 from the android directory:
`gradle :app:assembleDebug`

GitHub Actions workflow "Android APK and prerelease" runs on pull requests and pushes to main. On a successful main build it publishes **an automatic GitHub prerelease** with an installable APK and checksum.

The alpha APK uses Android **debug signing** generated on the runner, not a stable signing identity. A new build may have a different certificate, so Android may require uninstall of the previous build. Uninstall removes the on-device cache/session; server messages persist. Before stable release we must configure a durable signing keystore in GitHub Secrets.

## Pilot acceptance
1. Install the APK without Android Studio or Google Play.
2. Add https://chime.mirv.top, check its instance identity, login as a regular ChimePort user.
3. Send a message via existing curl/Home Assistant publisher token and refresh Android feed.
4. Check channel filtering, detail, read/unread and local cache while offline.
5. Add a second HTTPS ChimePort server; verify separate sessions and no mixed messages.
6. Use light and dark themes, large fonts, bad network/TLS conditions.

Foreground SSE listeners keep the feed up to date while the app is open. Newly received live events can also appear in the Android notification shade after POST_NOTIFICATIONS permission is granted. Historical catch-up never generates a flood of old system notifications. There is still no reliable delivery when the app is closed or suspended; that requires a separate background-delivery stage.

## Live event transport

The app opens an authenticated SSE connection per connected server while in the foreground. When the server sends `messages_available`, the client synchronizes history through the existing cursor-based REST API, not through the SSE payload. A reconnect triggers a silent catch-up. Connections close when the application is stopped; transient failures retry with an exponential backoff up to 30 seconds. Connection status is visible in the header and server cards. New *live* events can produce Android system notifications after notification permission is granted; initial history and reconnected backlog stay silent.

## Android background and alpha signature

The app offers opt-in background monitoring using a visible ongoing foreground-service notification (Android specialUse type). The service keeps authenticated SSE subscriptions and synchronizes to SQLite. Swipe-away may continue receiving notifications on some devices, but force-stop, battery-saving restrictions, connectivity and OS process management can interrupt it; this is not guaranteed urgent alert delivery.

For alpha builds the GitHub workflow now caches one debug signing identity, so future runs should install over each other **while GitHub's signing cache survives**. This first build necessarily uses a new identity versus alpha 4: one more uninstall may be needed. GitHub cache eviction can still invalidate the signature. Before shipping a stable release, store a dedicated private signing key in encrypted GitHub Secrets and verify certificate continuity; do not rely on the cache or publish a private keystore in the repository.
