# BSSID Reporter

An iOS app that periodically reports the connected WiFi BSSID to a configurable HTTP endpoint. Designed to replace an iOS Shortcut with a more reliable native background solution.

## How It Works

The app uses background location updates (at low accuracy/power) to stay alive and periodically:

1. Checks if the current time is within the configured active hours
2. Checks if enough time has passed since the last report
3. Fetches the current WiFi BSSID via `NEHotspotNetwork`
4. POSTs the BSSID to the configured endpoint

## Settings

- **Endpoint URL** — where to POST (default: `https://events.baileys.dev/bssid`)
- **Payload Template** — JSON body with `{{bssid}}` placeholder
- **Frequency** — minimum minutes between reports (default: 2)
- **Active Hours** — time window for reporting (default: 8:00–23:00)
- **Enabled** — master on/off toggle

## Requirements

- iOS 16.0+
- Real device (BSSID not available in simulator)
- "Always" location permission
- WiFi Info entitlement (must be enabled in Apple Developer portal)

## Building

```bash
brew install xcodegen
cd bssid-reporter
xcodegen generate
open BSSIDReporter.xcodeproj
```

Build and run on a physical device from Xcode. You'll need to configure signing with your Apple Developer account and ensure the WiFi Info entitlement is enabled in your provisioning profile.

## Technical Notes

- Uses `kCLLocationAccuracyThreeKilometers` for minimal battery impact
- `startMonitoringSignificantLocationChanges()` allows iOS to relaunch the app if it's terminated
- Force-quitting the app from the app switcher will stop background updates
- The time window check supports wrapping past midnight (e.g., 22:00–6:00)
