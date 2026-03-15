import Foundation

struct Reporter {
    static func send(bssid: String, settings: AppSettings) async -> Bool {
        guard let url = URL(string: settings.endpointURL) else { return false }

        let body = settings.payloadTemplate.replacingOccurrences(of: "{{bssid}}", with: bssid)
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = body.data(using: .utf8)

        do {
            let (_, response) = try await URLSession.shared.data(for: request)
            let status = (response as? HTTPURLResponse)?.statusCode ?? 0
            return (200..<300).contains(status)
        } catch {
            return false
        }
    }
}
