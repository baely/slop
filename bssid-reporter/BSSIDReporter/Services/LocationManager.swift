import Foundation
import CoreLocation
import Combine

final class LocationManager: NSObject, ObservableObject, CLLocationManagerDelegate {
    @Published var lastBSSID: String?
    @Published var lastReportTime: Date?
    @Published var authorizationStatus: CLAuthorizationStatus = .notDetermined
    @Published var logEntries: [LogEntry] = []

    struct LogEntry: Identifiable {
        let id = UUID()
        let date: Date
        let message: String
    }

    private let manager = CLLocationManager()
    private var settings: AppSettings?
    private var lastSendDate: Date?

    override init() {
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyThreeKilometers
        manager.allowsBackgroundLocationUpdates = true
        manager.pausesLocationUpdatesAutomatically = false
        manager.showsBackgroundLocationIndicator = false
        authorizationStatus = manager.authorizationStatus
    }

    func configure(settings: AppSettings) {
        self.settings = settings
    }

    func requestAuthorization() {
        manager.requestAlwaysAuthorization()
    }

    func start() {
        manager.startUpdatingLocation()
        manager.startMonitoringSignificantLocationChanges()
        log("Started location updates")
    }

    func stop() {
        manager.stopUpdatingLocation()
        manager.stopMonitoringSignificantLocationChanges()
        log("Stopped location updates")
    }

    // MARK: - CLLocationManagerDelegate

    func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        authorizationStatus = manager.authorizationStatus
    }

    func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        guard let settings = settings, settings.isEnabled else { return }

        let hour = Calendar.current.component(.hour, from: Date())
        let inWindow: Bool
        if settings.startHour <= settings.endHour {
            inWindow = hour >= settings.startHour && hour < settings.endHour
        } else {
            inWindow = hour >= settings.startHour || hour < settings.endHour
        }
        guard inWindow else { return }

        if let last = lastSendDate {
            let elapsed = Date().timeIntervalSince(last)
            guard elapsed >= Double(settings.frequencyMinutes) * 60 else { return }
        }

        Task {
            guard let bssid = await BSSIDFetcher.fetchCurrent() else {
                await MainActor.run { log("No BSSID available") }
                return
            }

            let success = await Reporter.send(bssid: bssid, settings: settings)

            await MainActor.run {
                lastBSSID = bssid
                if success {
                    lastReportTime = Date()
                    lastSendDate = Date()
                    log("Reported \(bssid)")
                } else {
                    log("Failed to report \(bssid)")
                }
            }
        }
    }

    private func log(_ message: String) {
        let entry = LogEntry(date: Date(), message: message)
        logEntries.insert(entry, at: 0)
        if logEntries.count > 50 {
            logEntries = Array(logEntries.prefix(50))
        }
    }
}
