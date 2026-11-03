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

## Alpha 10: channel alerts and message deep links

- Settings → **Звуки и каналы**: edit the sound, vibration, and importance for each channel on each connected server. Sound choices: system default, original ChimePort chime, triple signal, and silence. A preview button plays each sound.
- Android notification groups separate server accounts. Each channel is subdivided into urgent/high, normal, and low priority lanes. Per-channel importance can instead force high importance or quiet delivery. OS Do Not Disturb is never bypassed.
- Android NotificationChannel settings are immutable once created. Each explicit profile change creates a new versioned channel ID; old records may remain in Android settings for notifications already posted.
- Several active messages from the same ChimePort server receive a silent group summary. Tapping an individual notification opens its message detail, even when the app was already open.
- This release remains a best-effort SSE delivery application. Optional foreground service still presents a persistent OS notification and may be interrupted by battery restrictions or force-stop.
- No server-side changes for alpha 10. The APK signing fingerprint is verified before publishing.

The two original ChimePort notification tones were synthesized for the application, with no external music dependency.

## Alpha 11 — Android system ringtone picker

The per-channel sound settings now include **Из звуков телефона**, launching Android's native notification ringtone picker (`RingtoneManager.ACTION_RINGTONE_PICKER`, `TYPE_NOTIFICATION`). Choosing a system or installed notification tone stores its URI and display title per ChimePort channel, without requesting broad access to the phone's music library. The chosen URI becomes the sound for the next revision of that channel's Android `NotificationChannel`, because Android does not allow changing an existing channel's sound. The system can still override per-channel sound and Do Not Disturb applies. The original ChimePort sounds remain available. This does not yet provide a separate file-browser picker for arbitrary music files.

## Alpha 12: daily quiet hours (Android-only)

Settings → Тихие часы defines a device-local daily window with start and end time
in 24-hour format. Configuration is hierarchical: global → per-server → per-channel.
Servers and channels default to INHERIT; they can explicitly turn the schedule OFF
even when their parent has one, or set an independent daily schedule. The default
global setting is OFF. Overnight spans such as 22:00–07:00 work across midnight; equal
start and end means an all-day quiet window. Times follow the phone's current local
time zone; no permanent background timer or extra alarm permission is used.

During quiet hours NEW notifications still appear in Android's shade and
ChimePort's inbox, but they use an existing dedicated silent notification lane
without sound or vibration. At the end of a quiet window, subsequent notifications
use their normal priority/sound/vibration profile. No queued alerts are replayed
with sound. Optionally allow urgent priority to use its normal configured
sound and vibration during a quiet interval; this does not override Android's
Do Not Disturb or an explicitly SILENT/QUIET channel profile. High and all
other priorities are quiet during the window.

The persistent foreground-service connection indicator remains visible and silent.
In-app schedules and user-selected sounds cannot bypass Android notification
permissions or Do Not Disturb. These preferences are local to this phone (not
synchronized to the ChimePort server) and are erased for an account if the account
is removed. No server update is necessary. The APK's signing certificate remains pinned
and checked before release.

## Android signing: migrate pinned alpha key to GitHub Secrets

All APKs from alpha 8 onward use the same pinned certificate. Rotating the
private signing key would force an uninstall and wipe Android app-local data.
The private key currently lives in an expiring Actions cache.

A one-time *manual* GitHub Actions workflow safely migrates the **same**
keystore to an encrypted repository secret. The owner must explicitly authorize
this operation:

1. In GitHub create a **short-lived fine-grained PAT**, restricted to
   **mirivlad/ChimePort**, with **Secrets: Read and Write** permission.
   Do NOT paste it into chats or code.
2. In repository Settings → Secrets and variables → Actions, add
   **CHIMEPORT_SIGNING_MIGRATION_PAT** with that temporary PAT.
3. From Actions manually run **Migrate existing ChimePort alpha signing key to
   GitHub Secrets** on the main branch.
4. The workflow restores the old cache and checks its SHA-256 fingerprint
   against the public pinned fingerprint before sending an encrypted
   copy into **CHIMEPORT_ALPHA_KEYSTORE_B64** using GitHub CLI.
   It never prints or uploads an artifact containing the private key.
5. Run a normal Android build and ensure the output APK signature check is
   successful, and that the signing source is the protected secret.
6. Delete CHIMEPORT_SIGNING_MIGRATION_PAT and revoke that short-lived PAT.

Until then release CI prefers the protected secret when present, falling
back to the SAME original cache; if neither exists the build fails rather
than silently rotating certificates. Never commit private key material.
