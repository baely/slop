import SwiftUI

@main
struct BSSIDReporterApp: App {
    @StateObject private var settings = AppSettings()
    @StateObject private var locationManager = LocationManager()

    var body: some Scene {
        WindowGroup {
            ContentView(settings: settings, locationManager: locationManager)
                .onAppear {
                    locationManager.configure(settings: settings)
                    if settings.isEnabled {
                        locationManager.requestAuthorization()
                        locationManager.start()
                    }
                }
        }
    }
}
