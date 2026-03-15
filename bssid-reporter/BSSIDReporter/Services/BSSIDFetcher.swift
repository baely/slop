import Foundation
import NetworkExtension

struct BSSIDFetcher {
    static func fetchCurrent() async -> String? {
        await withCheckedContinuation { continuation in
            NEHotspotNetwork.fetchCurrent { network in
                continuation.resume(returning: network?.bssid)
            }
        }
    }
}
