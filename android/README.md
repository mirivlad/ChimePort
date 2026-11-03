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

## Alpha 6: inbox, search and reversible read state

Inbox starts in **Unread**, followed by **All** and a horizontally scrollable tab for each channel. Bottom navigation shows **Search** instead of a separate Channels screen. Search covers downloaded local history and filters by server, channel, read state and priority.

Swipe an unread notification left to mark read. Swipe a read notification right to restore it to Unread. No transient undo snackbar is shown. Both actions persist to server and device SQLite; errors leave the previous state.

**Server update required:** Reverse state uses DELETE /api/v1/messages/{id}/read, which earlier server versions do not support. Update the ChimePort Docker image (back up the data volume first); source tokens and history remain intact.

## Signing incident (2026-11-03)

The actual published alpha-5 and alpha-6 APK signatures are **different**, although the Actions workflow restored a keystore with an identical certificate fingerprint. Gradle was not explicitly bound to that keystore. Android correctly blocked in-place updates; the private signing identities of the already-distributed alpha-5/alpha-6 APKs are not available for reuse.

From the next alpha onward, CI sets an explicit CHIMEPORT_SIGNING_KEYSTORE for Gradle, verifies the **actual output APK** with apksigner, and compares the certificate with a pinned SHA-256 fingerprint. A missing cache now **fails the release build** instead of silently generating a new key. PR validation APKs may have another debug certificate and must not be distributed as upgrades.

The signing key is still held in Actions cache. This is a **temporary alpha-only solution**, not a secure or durable release setup. Before stable releases, migrate a protected signing keystore to GitHub Actions Secrets, not into Git.

## Alpha 9: repeated swipe regression fix

Swipe gesture state is scoped to both message identity and current read flag.
The old Compose state retained a callback capturing stale read state after restoring
a read notification to Unread. The card moved but the next gesture did nothing.
Updated callback references and a gesture-state reset after every read-state change
prevent this; a JVM regression test covers multiple unread/read/unread cycles.
Android-only change; no server update necessary. CI checks the signed output APK.
