import SwiftUI
import CoreLocation

struct ContentView: View {
    @ObservedObject var settings: AppSettings
    @ObservedObject var locationManager: LocationManager

    private var authStatus: String {
        switch locationManager.authorizationStatus {
        case .authorizedAlways: return "Always"
        case .authorizedWhenInUse: return "When In Use"
        case .denied: return "Denied"
        case .restricted: return "Restricted"
        case .notDetermined: return "Not Determined"
        @unknown default: return "Unknown"
        }
    }

    var body: some View {
        NavigationView {
            Form {
                Section("Status") {
                    Toggle("Enabled", isOn: $settings.isEnabled)
                        .onChange(of: settings.isEnabled) { enabled in
                            if enabled {
                                locationManager.requestAuthorization()
                                locationManager.start()
                            } else {
                                locationManager.stop()
                            }
                        }

                    HStack {
                        Text("Location Permission")
                        Spacer()
                        Text(authStatus)
                            .foregroundColor(.secondary)
                    }

                    if let bssid = locationManager.lastBSSID {
                        HStack {
                            Text("Last BSSID")
                            Spacer()
                            Text(bssid)
                                .foregroundColor(.secondary)
                                .font(.system(.body, design: .monospaced))
                        }
                    }

                    if let time = locationManager.lastReportTime {
                        HStack {
                            Text("Last Report")
                            Spacer()
                            Text(time, style: .relative)
                                .foregroundColor(.secondary)
                        }
                    }
                }

                Section("Endpoint") {
                    TextField("URL", text: $settings.endpointURL)
                        .keyboardType(.URL)
                        .autocapitalization(.none)
                        .disableAutocorrection(true)

                    VStack(alignment: .leading) {
                        Text("Payload Template")
                            .font(.caption)
                            .foregroundColor(.secondary)
                        TextField("JSON payload", text: $settings.payloadTemplate)
                            .font(.system(.body, design: .monospaced))
                            .autocapitalization(.none)
                            .disableAutocorrection(true)
                    }
                }

                Section("Schedule") {
                    Stepper("Every \(settings.frequencyMinutes) min", value: $settings.frequencyMinutes, in: 1...60)

                    HStack {
                        Text("Active hours")
                        Spacer()
                        Picker("Start", selection: $settings.startHour) {
                            ForEach(0..<24) { h in
                                Text("\(h):00").tag(h)
                            }
                        }
                        .labelsHidden()
                        Text("–")
                        Picker("End", selection: $settings.endHour) {
                            ForEach(0..<24) { h in
                                Text("\(h):00").tag(h)
                            }
                        }
                        .labelsHidden()
                    }
                }

                if !locationManager.logEntries.isEmpty {
                    Section("Recent Activity") {
                        ForEach(locationManager.logEntries) { entry in
                            VStack(alignment: .leading, spacing: 2) {
                                Text(entry.message)
                                    .font(.system(.caption, design: .monospaced))
                                Text(entry.date, style: .time)
                                    .font(.caption2)
                                    .foregroundColor(.secondary)
                            }
                        }
                    }
                }
            }
            .navigationTitle("BSSID Reporter")
        }
    }
}
