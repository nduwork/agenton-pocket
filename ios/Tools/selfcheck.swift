import Foundation

// Runnable wire-protocol self-check. Compile against the real client models:
//   swiftc ios/Agenton/Models/Frame.swift ios/Agenton/Models/Protocol.swift \
//          ios/Tools/selfcheck.swift -o /tmp/selfcheck && /tmp/selfcheck
// Verifies the byte framing and envelope JSON match the wire spec that the Go
// daemon and browser client already speak. No Xcode app required — just a Swift
// toolchain with a working Foundation.

func check(_ cond: Bool, _ label: String) {
    if !cond { FileHandle.standardError.write("FAIL: \(label)\n".data(using: .utf8)!); exit(1) }
    print("ok  - \(label)")
}

@main enum SelfCheck {
    static func main() throws {
        // 1. Header layout: known bytes decode to the expected fields.
        //    [0x02][00 00 00 07][00 00 00 03]['a','b','c']
        let raw = Data([0x02, 0, 0, 0, 7, 0, 0, 0, 3, 0x61, 0x62, 0x63])
        let f = Wire.decodeFrame(raw)!
        check(f.type == FrameType.output.rawValue, "decode: type=output")
        check(f.sessionID == 7, "decode: session_id big-endian == 7")
        check(f.payload == Data([0x61, 0x62, 0x63]), "decode: payload == abc")

        // 2. Round-trip through frame() for an output frame.
        let rt = Wire.frame(type: .output, sessionID: 0xDEADBEEF, payload: Data("hi".utf8))
        let back = Wire.decodeFrame(rt)!
        check(back.sessionID == 0xDEADBEEF, "roundtrip: 32-bit session_id survives")
        check(back.payload == Data("hi".utf8), "roundtrip: payload survives")

        // 3. Data-slice safety: a frame parsed out of a larger buffer (non-zero
        //    startIndex) must index from its own base, not from 0.
        var buf = Data([0xFF, 0xFF]) // two leading junk bytes
        buf.append(rt)
        let slice = buf[(buf.startIndex + 2)...] // Data slice preserves a non-zero startIndex
        check(slice.startIndex != 0, "slice: startIndex is non-zero (exercises the bug path)")
        let sliced = Wire.decodeFrame(slice)!
        check(sliced.sessionID == 0xDEADBEEF && sliced.payload == Data("hi".utf8), "slice: decodes correctly")

        // 4. Truncated buffers return nil, never trap.
        check(Wire.decodeFrame(Data([0x01, 0, 0])) == nil, "short header -> nil")
        check(Wire.decodeFrame(Data([0x02, 0, 0, 0, 0, 0, 0, 0, 9, 0x61])) == nil, "len exceeds payload -> nil")

        // 5. Control encode: session_id 0 in the header, real id inside the JSON.
        var att = Envelope(type: .attach); att.sessionID = 42
        let ctrl = try Wire.encodeControl(att)
        let cf = Wire.decodeFrame(ctrl)!
        check(cf.type == FrameType.control.rawValue, "control: header type=control")
        check(cf.sessionID == 0, "control: header session_id == 0 (id rides in JSON)")
        let obj = try JSONSerialization.jsonObject(with: cf.payload) as! [String: Any]
        check(obj["type"] as? String == "attach", "control JSON: type == attach")
        check(obj["session_id"] as? Int == 42, "control JSON: session_id == 42")
        check(obj["cols"] == nil && obj["text"] == nil, "control JSON: nil fields omitted (omitempty parity)")

        // 5b. set_active carries the active flag, and it round-trips through decode.
        var sa = Envelope(type: .setActive); sa.sessionID = 5; sa.active = true
        let saCtrl = try Wire.encodeControl(sa)
        let saObj = try JSONSerialization.jsonObject(with: Wire.decodeFrame(saCtrl)!.payload) as! [String: Any]
        check(saObj["active"] as? Bool == true, "set_active JSON: active == true")
        let saDecoded = try JSONDecoder().decode(Envelope.self, from: Wire.decodeFrame(saCtrl)!.payload)
        check(saDecoded.active == true, "set_active: decodes back to active == true")

        // 6. new_session_cmd carries command/args/cwd/agent_label with the Go json keys.
        var ns = Envelope(type: .newSessionCmd)
        ns.command = "claude"; ns.args = ["--foo"]; ns.cwd = "~/x"; ns.agentLabel = "claude"
        let nsObj = try JSONSerialization.jsonObject(with: Wire.decodeFrame(try Wire.encodeControl(ns))!.payload) as! [String: Any]
        check(nsObj["agent_label"] as? String == "claude", "new_session_cmd: agent_label key")
        check((nsObj["args"] as? [String]) == ["--foo"], "new_session_cmd: args array")

        // 7. Decode a session_list envelope, including a session missing the
        //    omitempty fields (repo, command_line) — must NOT drop the list.
        let listJSON = """
        {"type":"session_list","sessions":[
          {"id":3,"name":"","agent":"/usr/local/bin/claude","status":"running","cwd":"/a/b","last_activity":0,"repo":"agenton","command_line":"claude --x"},
          {"id":4,"name":"","agent":"bash","status":"running","cwd":"/a/b","last_activity":0}
        ]}
        """.data(using: .utf8)!
        let listEnv = try JSONDecoder().decode(Envelope.self, from: listJSON)
        check(listEnv.type == .sessionList, "session_list: type parsed")
        check(listEnv.sessions?.count == 2, "session_list: both sessions decoded (omitempty-safe)")
        let s = listEnv.sessions![0]
        check(s.id == 3 && s.isRunning, "session_list: id + running status")
        check(s.repoLabel == "agenton", "session_list: repoLabel from repo")
        check(s.terminalName == "Claude", "session_list: terminalName from agent path")
        check(s.commandLine == "claude --x", "session_list: command_line key")
        check(listEnv.sessions![1].repoLabel == "b", "session_list: missing repo falls back to cwd folder")

        print("\nALL WIRE CHECKS PASSED")
    }
}
