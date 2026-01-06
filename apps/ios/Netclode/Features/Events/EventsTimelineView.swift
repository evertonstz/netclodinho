import SwiftUI

struct EventsTimelineView: View {
    let sessionId: String

    @Environment(EventStore.self) private var eventStore

    var events: [AgentEvent] {
        eventStore.events(for: sessionId)
    }

    var body: some View {
        Group {
            if events.isEmpty {
                EmptyEventsView()
            } else {
                eventsList
            }
        }
    }

    private var eventsList: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 0) {
                    ForEach(Array(events.enumerated()), id: \.element.id) { index, event in
                        EventRow(event: event, isLast: index == events.count - 1)
                            .id(event.id)
                    }
                }
                .padding()
            }
            .onChange(of: events.count) {
                if let lastEvent = events.last {
                    withAnimation(.glassSpring) {
                        proxy.scrollTo(lastEvent.id, anchor: .bottom)
                    }
                }
            }
        }
    }
}

// MARK: - Event Row

struct EventRow: View {
    let event: AgentEvent
    let isLast: Bool

    @State private var isExpanded = false

    var body: some View {
        HStack(alignment: .top, spacing: Theme.Spacing.md) {
            // Timeline indicator
            VStack(spacing: 0) {
                Circle()
                    .fill(eventColor)
                    .frame(width: 12, height: 12)

                if !isLast {
                    Rectangle()
                        .fill(Theme.Colors.gentleGray.opacity(0.3))
                        .frame(width: 2)
                        .frame(maxHeight: .infinity)
                }
            }

            // Event content
            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                eventCard
                    .onTapGesture {
                        withAnimation(.glassSpring) {
                            isExpanded.toggle()
                        }
                        HapticFeedback.selection()
                    }

                // Timestamp
                Text(event.timestamp.formatted(.dateTime.hour().minute().second()))
                    .font(.netclodeCaption)
                    .foregroundStyle(.tertiary)
            }
            .padding(.bottom, Theme.Spacing.md)
        }
    }

    @ViewBuilder
    private var eventCard: some View {
        switch event {
        case .toolStart(let e):
            ToolEventCard(tool: e.tool, status: .started, input: e.input, isExpanded: isExpanded)

        case .toolEnd(let e):
            ToolEventCard(tool: e.tool, status: e.isSuccess ? .success : .failed, result: e.result, error: e.error, isExpanded: isExpanded)

        case .fileChange(let e):
            FileChangeCard(event: e)

        case .commandStart(let e):
            CommandCard(command: e.command, status: .started, cwd: e.cwd, isExpanded: isExpanded)

        case .commandEnd(let e):
            CommandCard(command: e.command, status: e.isSuccess ? .success : .failed, exitCode: e.exitCode, output: e.output, isExpanded: isExpanded)

        case .thinking(let e):
            ThinkingCard(content: e.content, isExpanded: isExpanded)

        case .portDetected(let e):
            PortDetectedCard(event: e)
        }
    }

    private var eventColor: Color {
        switch event {
        case .toolStart, .commandStart:
            Theme.Colors.gentleBlue
        case .toolEnd(let e):
            e.isSuccess ? Theme.Colors.cozySage : Theme.Colors.warmCoral
        case .commandEnd(let e):
            e.isSuccess ? Theme.Colors.cozySage : Theme.Colors.warmCoral
        case .fileChange:
            Theme.Colors.cozyPurple
        case .thinking:
            Theme.Colors.cozyLavender
        case .portDetected:
            Theme.Colors.cozyTeal
        }
    }
}

// MARK: - Tool Event Card

struct ToolEventCard: View {
    let tool: String
    let status: EventStatus
    var input: [String: AnyCodableValue]? = nil
    var result: String? = nil
    var error: String? = nil
    let isExpanded: Bool

    enum EventStatus {
        case started, success, failed

        var icon: String {
            switch self {
            case .started: "play.circle.fill"
            case .success: "checkmark.circle.fill"
            case .failed: "xmark.circle.fill"
            }
        }

        var color: Color {
            switch self {
            case .started: Theme.Colors.gentleBlue
            case .success: Theme.Colors.cozySage
            case .failed: Theme.Colors.warmCoral
            }
        }
    }

    var body: some View {
        GlassCard(tint: status.color.opacity(0.15), padding: Theme.Spacing.sm) {
            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                HStack {
                    Image(systemName: "wrench.and.screwdriver.fill")
                        .foregroundStyle(status.color)

                    Text(tool)
                        .font(.netclodeSubheadline)

                    Spacer()

                    Image(systemName: status.icon)
                        .foregroundStyle(status.color)
                }

                if isExpanded {
                    if let input, !input.isEmpty {
                        Text("Input: \(input.map { "\($0.key): \($0.value)" }.joined(separator: ", "))")
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                            .lineLimit(3)
                    }

                    if let result {
                        Text(result)
                            .font(.netclodeMonospacedSmall)
                            .foregroundStyle(.secondary)
                            .lineLimit(5)
                    }

                    if let error {
                        Text("Error: \(error)")
                            .font(.netclodeCaption)
                            .foregroundStyle(Theme.Colors.warmCoral)
                    }
                }
            }
        }
    }
}

// MARK: - File Change Card

struct FileChangeCard: View {
    let event: FileChangeEvent

    var body: some View {
        GlassCard(tint: Theme.Colors.cozyPurple.opacity(0.15), padding: Theme.Spacing.sm) {
            HStack {
                Image(systemName: event.action.systemImage)
                    .foregroundStyle(actionColor)

                VStack(alignment: .leading, spacing: 2) {
                    Text(event.fileName)
                        .font(.netclodeSubheadline)
                        .lineLimit(1)

                    Text(event.path)
                        .font(.netclodeCaption)
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                }

                Spacer()

                if event.linesAdded != nil || event.linesRemoved != nil {
                    Text(event.changeDescription)
                        .font(.netclodeMonospacedSmall)
                        .foregroundStyle(.secondary)
                }
            }
        }
    }

    private var actionColor: Color {
        switch event.action {
        case .create: Theme.Colors.cozySage
        case .edit: Theme.Colors.cozyPurple
        case .delete: Theme.Colors.warmCoral
        }
    }
}

// MARK: - Command Card

struct CommandCard: View {
    let command: String
    let status: ToolEventCard.EventStatus
    var cwd: String? = nil
    var exitCode: Int? = nil
    var output: String? = nil
    let isExpanded: Bool

    var body: some View {
        GlassCard(tint: status.color.opacity(0.15), padding: Theme.Spacing.sm) {
            VStack(alignment: .leading, spacing: Theme.Spacing.xs) {
                HStack {
                    Image(systemName: "terminal.fill")
                        .foregroundStyle(status.color)

                    Text(command)
                        .font(.netclodeMonospacedSmall)
                        .lineLimit(isExpanded ? nil : 1)

                    Spacer()

                    if let exitCode {
                        Text("exit \(exitCode)")
                            .font(.netclodeCaption)
                            .foregroundStyle(exitCode == 0 ? Theme.Colors.cozySage : Theme.Colors.warmCoral)
                    } else {
                        Image(systemName: status.icon)
                            .foregroundStyle(status.color)
                    }
                }

                if isExpanded {
                    if let cwd {
                        Text("in \(cwd)")
                            .font(.netclodeCaption)
                            .foregroundStyle(.tertiary)
                    }

                    if let output, !output.isEmpty {
                        Text(output)
                            .font(.netclodeMonospacedSmall)
                            .foregroundStyle(.secondary)
                            .lineLimit(10)
                    }
                }
            }
        }
    }
}

// MARK: - Thinking Card

struct ThinkingCard: View {
    let content: String
    let isExpanded: Bool

    var body: some View {
        GlassCard(tint: Theme.Colors.cozyLavender.opacity(0.15), padding: Theme.Spacing.sm) {
            HStack(alignment: .top) {
                Image(systemName: "brain.head.profile")
                    .foregroundStyle(Theme.Colors.cozyLavender)

                Text(content)
                    .font(.netclodeCaption)
                    .foregroundStyle(.secondary)
                    .lineLimit(isExpanded ? nil : 2)
                    .italic()
            }
        }
    }
}

// MARK: - Port Detected Card

struct PortDetectedCard: View {
    let event: PortDetectedEvent

    var body: some View {
        GlassCard(tint: Theme.Colors.cozyTeal.opacity(0.15), padding: Theme.Spacing.sm) {
            HStack {
                Image(systemName: "network")
                    .foregroundStyle(Theme.Colors.cozyTeal)

                VStack(alignment: .leading, spacing: 2) {
                    Text("Port \(event.port)")
                        .font(.netclodeSubheadline)

                    if let process = event.process {
                        Text(process)
                            .font(.netclodeCaption)
                            .foregroundStyle(.secondary)
                    }
                }

                Spacer()

                if let url = event.previewUrl {
                    Link(destination: URL(string: url)!) {
                        Image(systemName: "arrow.up.right.square")
                            .foregroundStyle(Theme.Colors.cozyTeal)
                    }
                }
            }
        }
    }
}

// MARK: - Empty State

struct EmptyEventsView: View {
    var body: some View {
        VStack(spacing: Theme.Spacing.md) {
            Image(systemName: "list.bullet.rectangle")
                .font(.system(size: 48))
                .foregroundStyle(Theme.Colors.gentleGray.opacity(0.5))

            Text("No Events Yet")
                .font(.netclodeHeadline)

            Text("Agent activity will appear here as Claude works on your requests.")
                .font(.netclodeBody)
                .foregroundStyle(.secondary)
                .multilineTextAlignment(.center)
        }
        .padding()
    }
}

// MARK: - Preview

#Preview {
    let store = EventStore()
    store.appendEvent(sessionId: "test", event: .previewToolStart)
    store.appendEvent(sessionId: "test", event: .previewFileChange)
    store.appendEvent(sessionId: "test", event: .previewCommandStart)
    store.appendEvent(sessionId: "test", event: .previewCommandEnd)
    store.appendEvent(sessionId: "test", event: .previewToolEnd)

    return NavigationStack {
        EventsTimelineView(sessionId: "test")
    }
    .environment(store)
    .background(WarmGradientBackground())
}
