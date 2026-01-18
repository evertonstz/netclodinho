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

    /// Append partial thinking content or create a new thinking event
    func appendThinkingPartial(sessionId: String, thinkingId: String, content: String, timestamp: Date) {
        var events = eventsBySession[sessionId] ?? []

        // Find existing thinking event with this thinkingId
        if let existingIndex = events.lastIndex(where: { event in
            if case .thinking(let e) = event, e.thinkingId == thinkingId {
                return true
            }
            return false
        }) {
            // Update existing thinking event by appending content
            if case .thinking(var thinkingEvent) = events[existingIndex] {
                thinkingEvent.content += content
                events[existingIndex] = .thinking(thinkingEvent)
            }
        } else {
            // Create new thinking event
            let newEvent = ThinkingEvent(
                id: UUID(),
                timestamp: timestamp,
                thinkingId: thinkingId,
                content: content,
                partial: true
            )
            events.append(.thinking(newEvent))
        }

        eventsBySession[sessionId] = events
    }

    /// Finalize a thinking event (mark as complete)
    func finalizeThinking(sessionId: String, thinkingId: String) {
        guard var events = eventsBySession[sessionId] else { return }

        if let index = events.lastIndex(where: { event in
            if case .thinking(let e) = event, e.thinkingId == thinkingId {
                return true
            }
            return false
        }) {
            if case .thinking(var thinkingEvent) = events[index] {
                // Create a new event with partial = false
                let finalizedEvent = ThinkingEvent(
                    id: thinkingEvent.id,
                    timestamp: thinkingEvent.timestamp,
                    thinkingId: thinkingEvent.thinkingId,
                    content: thinkingEvent.content,
                    partial: false
                )
                events[index] = .thinking(finalizedEvent)
                eventsBySession[sessionId] = events
            }
        }
    }

    func clearEvents(for sessionId: String) {
        eventsBySession.removeValue(forKey: sessionId)
    }

    /// Update a tool_start event with complete input (received after streaming started)
    func updateToolInput(sessionId: String, toolUseId: String, input: [String: AnyCodableValue]) {
        guard var events = eventsBySession[sessionId] else { return }

        if let index = events.lastIndex(where: { event in
            if case .toolStart(let e) = event, e.toolUseId == toolUseId {
                return true
            }
            return false
        }) {
            if case .toolStart(let existing) = events[index] {
                // Create updated event with new input
                let updated = ToolStartEvent(
                    id: existing.id,
                    timestamp: existing.timestamp,
                    tool: existing.tool,
                    toolUseId: existing.toolUseId,
                    input: input
                )
                events[index] = .toolStart(updated)
                eventsBySession[sessionId] = events
            }
        }
    }

    /// Load events from server sync response
    func loadEvents(sessionId: String, events: [PersistedEvent]) {
        // Aggregate events:
        // 1. Thinking events by thinkingId to avoid fragmented display
        // 2. tool_input_complete input merged into tool_start events
        var aggregatedEvents: [AgentEvent] = []
        var thinkingIndex: [String: Int] = [:] // thinkingId -> index in aggregatedEvents
        var toolStartIndex: [String: Int] = [:] // toolUseId -> index in aggregatedEvents

        for persistedEvent in events {
            let event = persistedEvent.event.toAgentEvent()

            switch event {
            case .thinking(let thinkingEvent):
                if let existingIndex = thinkingIndex[thinkingEvent.thinkingId] {
                    // Append content to existing thinking event
                    if case .thinking(let existing) = aggregatedEvents[existingIndex] {
                        let updated = ThinkingEvent(
                            id: existing.id,
                            timestamp: existing.timestamp,
                            thinkingId: existing.thinkingId,
                            content: existing.content + thinkingEvent.content,
                            // Mark as not partial if we receive a final event
                            partial: thinkingEvent.partial && existing.partial
                        )
                        aggregatedEvents[existingIndex] = .thinking(updated)
                    }
                } else {
                    // New thinking event
                    thinkingIndex[thinkingEvent.thinkingId] = aggregatedEvents.count
                    aggregatedEvents.append(event)
                }

            case .toolStart(let toolStartEvent):
                // Track tool_start events for later input merging
                toolStartIndex[toolStartEvent.toolUseId] = aggregatedEvents.count
                aggregatedEvents.append(event)

            case .toolInputComplete(let inputCompleteEvent):
                // Merge input into corresponding tool_start event
                if let existingIndex = toolStartIndex[inputCompleteEvent.toolUseId] {
                    if case .toolStart(let existing) = aggregatedEvents[existingIndex] {
                        let updated = ToolStartEvent(
                            id: existing.id,
                            timestamp: existing.timestamp,
                            tool: existing.tool,
                            toolUseId: existing.toolUseId,
                            input: inputCompleteEvent.input
                        )
                        aggregatedEvents[existingIndex] = .toolStart(updated)
                    }
                }
                // Don't add tool_input_complete to aggregatedEvents (it's merged)

            case .toolInput:
                // Skip tool_input events (streaming deltas, not needed in history)
                break

            default:
                aggregatedEvents.append(event)
            }
        }

        eventsBySession[sessionId] = aggregatedEvents
    }
}
