import Foundation

// Wire frames, mirrored from internal/protocol/frame.go and static/frame.js:
//   [1 type][4 session_id BE][4 len BE][payload]
// One WebSocket binary message carries exactly one frame, so the app speaks the
// same protocol as the TUI and browser client.
enum FrameType: UInt8 {
    case control = 0x01 // JSON envelope; session_id rides inside the JSON, header id is 0
    case output = 0x02  // raw PTY bytes for the terminal emulator
}

struct Frame {
    let type: UInt8
    let sessionID: UInt32
    let payload: Data
}

enum Wire {
    static let headerLen = 9

    // encodeControl matches frame.js: a control frame always uses header
    // session_id 0 — the real id travels in the JSON envelope.
    static func encodeControl<T: Encodable>(_ msg: T) throws -> Data {
        let json = try JSONEncoder().encode(msg)
        return frame(type: .control, sessionID: 0, payload: json)
    }

    static func frame(type: FrameType, sessionID: UInt32, payload: Data) -> Data {
        var out = Data(capacity: headerLen + payload.count)
        out.append(type.rawValue)
        out.append(contentsOf: beBytes(sessionID))
        out.append(contentsOf: beBytes(UInt32(payload.count)))
        out.append(payload)
        return out
    }

    // decodeFrame returns nil on a short/truncated buffer rather than trapping —
    // a hostile or half-delivered message must not crash the client.
    static func decodeFrame(_ data: Data) -> Frame? {
        guard data.count >= headerLen else { return nil }
        // Data slices can carry a non-zero startIndex; index from base each time.
        let base = data.startIndex
        let type = data[base]
        let sessionID = beUInt32(data, at: base + 1)
        let n = Int(beUInt32(data, at: base + 5))
        let start = base + headerLen
        guard data.count - headerLen >= n else { return nil }
        return Frame(type: type, sessionID: sessionID, payload: data.subdata(in: start ..< start + n))
    }

    private static func beBytes(_ v: UInt32) -> [UInt8] {
        [UInt8(v >> 24 & 0xff), UInt8(v >> 16 & 0xff), UInt8(v >> 8 & 0xff), UInt8(v & 0xff)]
    }

    private static func beUInt32(_ d: Data, at i: Data.Index) -> UInt32 {
        UInt32(d[i]) << 24 | UInt32(d[i + 1]) << 16 | UInt32(d[i + 2]) << 8 | UInt32(d[i + 3])
    }
}
