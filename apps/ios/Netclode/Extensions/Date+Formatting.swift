import Foundation

extension Date {
    /// Format date for display in session list
    var sessionListFormat: String {
        let now = Date()
        let calendar = Calendar.current

        if calendar.isDateInToday(self) {
            return "Today at \(formatted(.dateTime.hour().minute()))"
        } else if calendar.isDateInYesterday(self) {
            return "Yesterday at \(formatted(.dateTime.hour().minute()))"
        } else if calendar.isDate(self, equalTo: now, toGranularity: .weekOfYear) {
            return formatted(.dateTime.weekday(.wide).hour().minute())
        } else {
            return formatted(.dateTime.month().day().hour().minute())
        }
    }

    /// Format date for event timeline
    var eventTimeFormat: String {
        formatted(.dateTime.hour().minute().second())
    }

    /// Relative time format (e.g., "5 minutes ago")
    var relativeFormat: String {
        formatted(.relative(presentation: .named))
    }
}

// MARK: - ISO 8601 Parsing

extension Date {
    /// Parse ISO 8601 date string
    static func fromISO8601(_ string: String) -> Date? {
        ISO8601DateFormatter().date(from: string)
    }

    /// Convert to ISO 8601 string
    var iso8601String: String {
        ISO8601DateFormatter().string(from: self)
    }
}

// MARK: - Duration Formatting

extension TimeInterval {
    /// Format duration as human-readable string
    var durationFormat: String {
        let formatter = DateComponentsFormatter()
        formatter.allowedUnits = [.hour, .minute, .second]
        formatter.unitsStyle = .abbreviated
        formatter.maximumUnitCount = 2
        return formatter.string(from: self) ?? "0s"
    }
}
