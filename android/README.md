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

No notifications while the app is fully closed yet; this is M2.1 only.
