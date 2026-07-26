import SwiftUI
import Speech
import AVFoundation

// Press-and-hold to dictate. On-device recognition; the final transcript is
// handed back via onTranscript for the caller to place in the text field.
struct TalkButton: View {
    // When set, renders as a full-width "Hold to talk" bar (Controller mode,
    // which has no text field). When nil, a compact mic that sits in the text bar.
    var label: String? = nil
    let onTranscript: (String) -> Void
    @State private var recording = false
    @StateObject private var rec = SpeechRecorder()

    private var hot: Color { Color(hex: 0xff5f6b) }

    var body: some View {
        content
            .contentShape(Rectangle())
            .gesture(
                DragGesture(minimumDistance: 0)
                    .onChanged { _ in
                        if !recording { recording = true; rec.start() }
                    }
                    .onEnded { _ in
                        recording = false
                        rec.stop { text in if !text.isEmpty { onTranscript(text) } }
                    }
            )
    }

    @ViewBuilder private var content: some View {
        if let label {
            HStack(spacing: 9) {
                Image(systemName: recording ? "mic.fill" : "mic").font(.system(size: 19))
                Text(recording ? "Listening…" : label).font(.system(size: 16, weight: .medium))
            }
            .frame(maxWidth: .infinity, minHeight: 52)
            .foregroundStyle(recording ? hot : Theme.fg)
            .background(recording ? hot.opacity(0.12) : Theme.panel, in: RoundedRectangle(cornerRadius: 12))
            .overlay(RoundedRectangle(cornerRadius: 12)
                .strokeBorder(recording ? hot.opacity(0.6) : Theme.rule))
        } else {
            // Compact mic for the Terminal-mode text bar.
            Image(systemName: recording ? "mic.fill" : "mic")
                .font(.system(size: 22))
                .frame(width: 44, height: 44)
                .foregroundStyle(recording ? hot : Theme.dim)
        }
    }
}

final class SpeechRecorder: ObservableObject {
    private let engine = AVAudioEngine()
    private let recognizer = SFSpeechRecognizer()
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    private var latest = ""
    // Monotonic session token: guards `latest` against a previous session's
    // callback firing after a rapid release-then-re-press starts a new one.
    private var session = 0

    func start() {
        session += 1
        let token = session
        SFSpeechRecognizer.requestAuthorization { _ in }
        AVAudioApplication.requestRecordPermission { _ in }
        guard let recognizer, recognizer.isAvailable else { return }
        let req = SFSpeechAudioBufferRecognitionRequest()
        req.requiresOnDeviceRecognition = true
        req.shouldReportPartialResults = true
        request = req
        let audioSession = AVAudioSession.sharedInstance()
        try? audioSession.setCategory(.record, mode: .measurement, options: .duckOthers)
        try? audioSession.setActive(true, options: .notifyOthersOnDeactivation)
        let input = engine.inputNode
        input.installTap(onBus: 0, bufferSize: 1024, format: input.outputFormat(forBus: 0)) { buf, _ in
            req.append(buf)
        }
        engine.prepare()
        try? engine.start()
        task = recognizer.recognitionTask(with: req) { [weak self] result, _ in
            guard let r = result else { return }
            let text = r.bestTranscription.formattedString
            DispatchQueue.main.async {
                guard let self, token == self.session else { return }
                self.latest = text
            }
        }
    }

    func stop(_ done: @escaping (String) -> Void) {
        let token = session
        engine.inputNode.removeTap(onBus: 0)
        engine.stop()
        request?.endAudio()
        task?.finish()
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        // Give the recognizer a beat to emit the final result, then report.
        DispatchQueue.main.asyncAfter(deadline: .now() + 0.3) { [weak self] in
            guard let self, token == self.session else { done(""); return }
            done(self.latest)
            self.latest = ""
        }
    }
}
