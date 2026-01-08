import Foundation

enum ClientMessage: Encodable, Sendable {
    case sessionCreate(name: String?, repo: String?)
    case sessionList
    case sessionResume(id: String)
    case sessionPause(id: String)
    case sessionDelete(id: String)
    case prompt(sessionId: String, text: String)
    case promptInterrupt(sessionId: String)
    case terminalInput(sessionId: String, data: String)
    case terminalResize(sessionId: String, cols: Int, rows: Int)
    case portExpose(sessionId: String, port: Int)
    // Sync messages
    case sync
    case sessionOpen(id: String, lastMessageId: String?, lastNotificationId: String?)

    private enum CodingKeys: String, CodingKey {
        case type
        case name, repo, id, sessionId, text, data, cols, rows, port, lastMessageId, lastNotificationId
    }

    func encode(to encoder: Encoder) throws {
        var container = encoder.container(keyedBy: CodingKeys.self)

        switch self {
        case .sessionCreate(let name, let repo):
            try container.encode("session.create", forKey: .type)
            try container.encodeIfPresent(name, forKey: .name)
            try container.encodeIfPresent(repo, forKey: .repo)

        case .sessionList:
            try container.encode("session.list", forKey: .type)

        case .sessionResume(let id):
            try container.encode("session.resume", forKey: .type)
            try container.encode(id, forKey: .id)

        case .sessionPause(let id):
            try container.encode("session.pause", forKey: .type)
            try container.encode(id, forKey: .id)

        case .sessionDelete(let id):
            try container.encode("session.delete", forKey: .type)
            try container.encode(id, forKey: .id)

        case .prompt(let sessionId, let text):
            try container.encode("prompt", forKey: .type)
            try container.encode(sessionId, forKey: .sessionId)
            try container.encode(text, forKey: .text)

        case .promptInterrupt(let sessionId):
            try container.encode("prompt.interrupt", forKey: .type)
            try container.encode(sessionId, forKey: .sessionId)

        case .terminalInput(let sessionId, let data):
            try container.encode("terminal.input", forKey: .type)
            try container.encode(sessionId, forKey: .sessionId)
            try container.encode(data, forKey: .data)

        case .terminalResize(let sessionId, let cols, let rows):
            try container.encode("terminal.resize", forKey: .type)
            try container.encode(sessionId, forKey: .sessionId)
            try container.encode(cols, forKey: .cols)
            try container.encode(rows, forKey: .rows)

        case .portExpose(let sessionId, let port):
            try container.encode("port.expose", forKey: .type)
            try container.encode(sessionId, forKey: .sessionId)
            try container.encode(port, forKey: .port)

        case .sync:
            try container.encode("sync", forKey: .type)

        case .sessionOpen(let id, let lastMessageId, let lastNotificationId):
            try container.encode("session.open", forKey: .type)
            try container.encode(id, forKey: .id)
            try container.encodeIfPresent(lastMessageId, forKey: .lastMessageId)
            try container.encodeIfPresent(lastNotificationId, forKey: .lastNotificationId)
        }
    }
}
