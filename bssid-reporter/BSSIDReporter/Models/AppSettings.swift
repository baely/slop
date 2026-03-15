import Foundation
import Combine

final class AppSettings: ObservableObject {
    @Published var endpointURL: String {
        didSet { UserDefaults.standard.set(endpointURL, forKey: "endpointURL") }
    }
    @Published var payloadTemplate: String {
        didSet { UserDefaults.standard.set(payloadTemplate, forKey: "payloadTemplate") }
    }
    @Published var frequencyMinutes: Int {
        didSet { UserDefaults.standard.set(frequencyMinutes, forKey: "frequencyMinutes") }
    }
    @Published var startHour: Int {
        didSet { UserDefaults.standard.set(startHour, forKey: "startHour") }
    }
    @Published var endHour: Int {
        didSet { UserDefaults.standard.set(endHour, forKey: "endHour") }
    }
    @Published var isEnabled: Bool {
        didSet { UserDefaults.standard.set(isEnabled, forKey: "isEnabled") }
    }

    init() {
        let defaults = UserDefaults.standard

        if defaults.object(forKey: "endpointURL") == nil {
            defaults.set("https://events.baileys.dev/bssid", forKey: "endpointURL")
        }
        if defaults.object(forKey: "payloadTemplate") == nil {
            defaults.set("{\"bssid\":\"{{bssid}}\"}", forKey: "payloadTemplate")
        }
        if defaults.object(forKey: "frequencyMinutes") == nil {
            defaults.set(2, forKey: "frequencyMinutes")
        }
        if defaults.object(forKey: "startHour") == nil {
            defaults.set(8, forKey: "startHour")
        }
        if defaults.object(forKey: "endHour") == nil {
            defaults.set(23, forKey: "endHour")
        }

        self.endpointURL = defaults.string(forKey: "endpointURL") ?? "https://events.baileys.dev/bssid"
        self.payloadTemplate = defaults.string(forKey: "payloadTemplate") ?? "{\"bssid\":\"{{bssid}}\"}"
        self.frequencyMinutes = defaults.integer(forKey: "frequencyMinutes")
        self.startHour = defaults.integer(forKey: "startHour")
        self.endHour = defaults.integer(forKey: "endHour")
        self.isEnabled = defaults.bool(forKey: "isEnabled")
    }
}
