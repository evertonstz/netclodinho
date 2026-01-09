import Foundation
import SwiftUI

@MainActor
@Observable
final class SettingsStore {
    var serverURL: String {
        didSet {
            UserDefaults.standard.set(serverURL, forKey: "netclode_server_url")
        }
    }

    var preferredColorScheme: ColorScheme? {
        didSet {
            let value: String? = switch preferredColorScheme {
            case .light: "light"
            case .dark: "dark"
            case nil: nil
            @unknown default: nil
            }
            UserDefaults.standard.set(value, forKey: "netclode_color_scheme")
        }
    }

    var hapticFeedbackEnabled: Bool {
        didSet {
            UserDefaults.standard.set(hapticFeedbackEnabled, forKey: "netclode_haptic_feedback")
        }
    }

    init() {
        serverURL = UserDefaults.standard.string(forKey: "netclode_server_url") ?? ""

        if let scheme = UserDefaults.standard.string(forKey: "netclode_color_scheme") {
            preferredColorScheme = scheme == "light" ? .light : scheme == "dark" ? .dark : nil
        } else {
            preferredColorScheme = nil
        }

        hapticFeedbackEnabled = UserDefaults.standard.object(forKey: "netclode_haptic_feedback") as? Bool ?? true
    }
}
