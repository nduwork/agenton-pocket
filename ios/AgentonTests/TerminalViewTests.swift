import XCTest
import SwiftTerm
@testable import Agenton

@MainActor
final class TerminalViewTests: XCTestCase {
    func testMouseTrackingDoesNotInstallCompetingPanRecognizer() {
        let view = OutputTerminalView(frame: CGRect(x: 0, y: 0, width: 390, height: 300))
        view.allowMouseReporting = false

        // An agent can enable button-event mouse tracking. On a phone, that
        // must not install SwiftTerm's mouse-drag recognizer alongside
        // UIScrollView's native pan recognizer, because the two recognizers
        // compete for the same one-finger drag and break scrolling.
        view.feed(byteArray: ArraySlice(Array("\u{1b}[?1002h".utf8)))

        let competingPans = (view.gestureRecognizers ?? []).compactMap {
            $0 as? UIPanGestureRecognizer
        }.filter {
            $0 !== view.panGestureRecognizer && $0.isEnabled
        }

        XCTAssertTrue(
            competingPans.isEmpty,
            "terminal mouse mode installed a competing one-finger pan recognizer"
        )
    }

    // MARK: drag → wheel

    func testDragTicksEmitOneWheelEventPerCellOfTravel() {
        var acc: CGFloat = 0
        // Half a cell is not a tick yet, but the remainder carries: the next
        // half completes one.
        XCTAssertEqual(OutputTerminalView.dragTicks(deltaY: 5, cellHeight: 10, accumulated: &acc), [])
        XCTAssertEqual(OutputTerminalView.dragTicks(deltaY: 5, cellHeight: 10, accumulated: &acc),
                       [OutputTerminalView.wheelUp])
        XCTAssertEqual(acc, 0, "a whole tick must consume its travel")

        // 3.5 cells at once = 3 ticks, half a cell carried forward.
        XCTAssertEqual(OutputTerminalView.dragTicks(deltaY: 35, cellHeight: 10, accumulated: &acc).count, 3)
        XCTAssertEqual(acc, 5)
    }

    func testDragDirectionMatchesNativePanning() {
        var acc: CGFloat = 0
        // Finger down pulls older content into view — that is a wheel *up*.
        XCTAssertEqual(OutputTerminalView.dragTicks(deltaY: 20, cellHeight: 10, accumulated: &acc),
                       [OutputTerminalView.wheelUp, OutputTerminalView.wheelUp])
        acc = 0
        XCTAssertEqual(OutputTerminalView.dragTicks(deltaY: -20, cellHeight: 10, accumulated: &acc),
                       [OutputTerminalView.wheelDown, OutputTerminalView.wheelDown])
    }

    func testDragTicksSurviveExactConsumeToZero() {
        var acc: CGFloat = 0
        // Consuming to exactly zero must not flip the next drag's direction.
        _ = OutputTerminalView.dragTicks(deltaY: 10, cellHeight: 10, accumulated: &acc)
        XCTAssertEqual(acc, 0)
        XCTAssertEqual(OutputTerminalView.dragTicks(deltaY: -10, cellHeight: 10, accumulated: &acc),
                       [OutputTerminalView.wheelDown])
    }

    func testDragTicksIgnoreDegenerateCellHeight() {
        var acc: CGFloat = 0
        XCTAssertEqual(OutputTerminalView.dragTicks(deltaY: 100, cellHeight: 0, accumulated: &acc), [])
    }

    func testWheelGestureIsInstalledOnceAndRecognizesSimultaneously() {
        let view = OutputTerminalView(frame: CGRect(x: 0, y: 0, width: 390, height: 300))
        let before = (view.gestureRecognizers ?? []).count
        view.installWheelGesture()
        view.installWheelGesture() // remounts must not stack a second recognizer
        let added = (view.gestureRecognizers ?? []).count - before
        XCTAssertEqual(added, 1, "drag→wheel pan installed \(added) times")

        // It must share the touch with the scroll view's own pan, or a session
        // without mouse reporting would lose native scrollback panning.
        let wheelPan = (view.gestureRecognizers ?? []).compactMap { $0 as? UIPanGestureRecognizer }
            .first { $0 !== view.panGestureRecognizer && $0.delegate != nil }
        XCTAssertNotNil(wheelPan)
        XCTAssertTrue(
            wheelPan?.delegate?.gestureRecognizer?(wheelPan!, shouldRecognizeSimultaneouslyWith: view.panGestureRecognizer) ?? false,
            "drag→wheel pan blocks the native scroll pan"
        )
    }

    // The whole point of routing through Terminal.sendEvent instead of an
    // encoder of our own: the same claude build negotiates SGR on one machine
    // (?1006h) and plain X10 on another. Hardcoding SGR — as the first attempt
    // at this feature did — emits garbage into every session that never set it.
    func testWheelEventsUseTheSessionsNegotiatedMouseEncoding() {
        let view = OutputTerminalView(frame: CGRect(x: 0, y: 0, width: 390, height: 300))
        let sink = CapturingTerminalDelegate()
        view.terminalDelegate = sink

        // Mouse tracking on, no ?1006h: X10 — CSI M then three offset bytes.
        view.feed(byteArray: ArraySlice(Array("\u{1b}[?1002h".utf8)))
        view.getTerminal().sendEvent(buttonFlags: OutputTerminalView.wheelUp, x: 3, y: 5)
        XCTAssertEqual(sink.sent.last.map(Array.init),
                       Array("\u{1b}[M".utf8) + [UInt8(32 + 64), UInt8(32 + 3 + 1), UInt8(32 + 5 + 1)])

        // Now the agent asks for SGR.
        view.feed(byteArray: ArraySlice(Array("\u{1b}[?1006h".utf8)))
        view.getTerminal().sendEvent(buttonFlags: OutputTerminalView.wheelDown, x: 3, y: 5)
        XCTAssertEqual(sink.sent.last.map { String(decoding: $0, as: UTF8.self) }, "\u{1b}[<65;4;6M")
    }

    func testBodyScrollGestureOwnershipDisablesSelectionTriggers() {
        let view = OutputTerminalView(frame: CGRect(x: 0, y: 0, width: 390, height: 300))
        view.ownBodyScrollGesture()

        // The long-press and double/triple-tap recognizers arm SwiftTerm's
        // selection pan, which steals one-finger drags from the scroll pan on a
        // real device. After ownership they must be disabled so a body drag
        // always scrolls.
        let selectionTriggers = (view.gestureRecognizers ?? []).filter { g in
            if g is UILongPressGestureRecognizer { return true }
            if let tap = g as? UITapGestureRecognizer, tap.numberOfTapsRequired >= 2 { return true }
            return false
        }
        XCTAssertFalse(selectionTriggers.isEmpty, "expected SwiftTerm to install selection-trigger gestures")
        XCTAssertTrue(
            selectionTriggers.allSatisfy { !$0.isEnabled },
            "selection-trigger gestures still enabled — one-finger drags can be stolen from the scroll pan"
        )
    }
}

// Captures what the terminal would send upstream, in place of the real
// coordinator's daemon hop.
@MainActor
private final class CapturingTerminalDelegate: NSObject, TerminalViewDelegate {
    var sent: [ArraySlice<UInt8>] = []

    nonisolated func send(source: SwiftTerm.TerminalView, data: ArraySlice<UInt8>) {
        MainActor.assumeIsolated { sent.append(data) }
    }
    func sizeChanged(source: SwiftTerm.TerminalView, newCols: Int, newRows: Int) {}
    func setTerminalTitle(source: SwiftTerm.TerminalView, title: String) {}
    func hostCurrentDirectoryUpdate(source: SwiftTerm.TerminalView, directory: String?) {}
    func scrolled(source: SwiftTerm.TerminalView, position: Double) {}
    func requestOpenLink(source: SwiftTerm.TerminalView, link: String, params: [String: String]) {}
    func clipboardCopy(source: SwiftTerm.TerminalView, content: Data) {}
    func rangeChanged(source: SwiftTerm.TerminalView, startY: Int, endY: Int) {}
}
