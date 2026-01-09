import Foundation

@MainActor
@Observable
final class EventStore {
    private(set) var eventsBySession: [String: [AgentEvent]] = [:]

    private let maxEventsPerSession = 100

    func events(for sessionId: String) -> [AgentEvent] {
        eventsBySession[sessionId] ?? []
    }

    func recentEvents(for sessionId: String, count: Int = 5) -> [AgentEvent] {
        Array((eventsBySession[sessionId] ?? []).suffix(count))
    }

    func appendEvent(sessionId: String, event: AgentEvent) {
        var events = eventsBySession[sessionId] ?? []
        events.append(event)

        // Keep only the most recent events
        if events.count > maxEventsPerSession {
            events = Array(events.suffix(maxEventsPerSession))
        }

        eventsBySession[sessionId] = events
    }

    func clearEvents(for sessionId: String) {
        eventsBySession.removeValue(forKey: sessionId)
    }

    /// Load events from server sync response
    func loadEvents(sessionId: String, events: [PersistedEvent]) {
        eventsBySession[sessionId] = events.map { $0.event.toAgentEvent() }
    }
}
