# Termwright for Android (preview)

A small companion app: your Termwright sessions on the phone, and **Wake-on-LAN with one tap** -
from the phone on the same Wi-Fi, or through a jump host (a NAS, a server) over SSH when you are away,
including the sleep-proxy ports that wake a Mac with Apple silicon.

- **Import**: on the computer, *File → Export Sessions → Zip archive*; send the zip to the phone and open it
  with Termwright (or *Import* in the app). Passwords are not in exports: the app asks for the jump host's
  login once and keeps it encrypted with a key in the Android Keystore.
- **Wake**: the *Wake* button on every session with a MAC address (several are fine, e.g. Wi-Fi and dock).
  The phone sends the packet itself when it is on the device's network, otherwise the jump host sends it.
  The first connection to a jump host shows its key to accept; a changed key is never accepted silently.
- **Status dots**: green when the device answers on a port (22, 445, 3389, 5900, 80 or its sleep-proxy
  ports), every minute while the app is open. Apps cannot ping without root.
- **Quick settings tile**: *Use for the Wake tile* on a session, then add the *Wake* tile to quick settings.

## How it is built

| Part | Where | What |
|---|---|---|
| Go core | `mobile/core` (package `twcore`) | Session import (the desktop's importers), wake packet, jump host commands, SSH - the desktop code |
| Android app | `mobile/android` | Kotlin, Jetpack Compose; calls `twcore` through `twcore.aar` |

The GitHub workflow (`build.yml`, job `android`) builds `twcore.aar` with gomobile and the APK with Gradle,
and attaches `Termwright-<version>-android.apk` to every release and nightly.

To build it yourself (Go 1.24, JDK 17, Android SDK with an NDK):

```
go install golang.org/x/mobile/cmd/gomobile@latest && go install golang.org/x/mobile/cmd/gobind@latest
go get golang.org/x/mobile/bind && gomobile init
gomobile bind -target=android -androidapi 26 -o mobile/android/app/libs/twcore.aar ./mobile/core
cd mobile/android && gradle assembleRelease
```

## Signing

An Android app must keep the same key so that a new version installs over the old one. Create the key once
and store it as GitHub secrets (Settings → Secrets and variables → Actions):

```
keytool -genkeypair -v -keystore termwright-release.jks -alias termwright -keyalg RSA -keysize 4096 -validity 10000
base64 -i termwright-release.jks | pbcopy
```

| Secret | Value |
|---|---|
| `ANDROID_KEYSTORE_B64` | the base64 text (copied above) |
| `ANDROID_KEYSTORE_PASSWORD` | the keystore password |
| `ANDROID_KEY_ALIAS` | `termwright` |
| `ANDROID_KEY_PASSWORD` | the key password (the same, unless you chose another) |

Keep `termwright-release.jks` and its password safe and out of the repository: a lost key means users
have to uninstall the app before installing a new version. Without the secrets the APK is signed with a
temporary key - fine for trying it out.
