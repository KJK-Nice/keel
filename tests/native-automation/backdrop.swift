// A solid window placed just below a window of another process (argument 1: its pid), over the same rectangle, so that a
// screen capture of a translucent window shows this and not whatever else is on the desktop (another Keel window, a
// terminal). Built and run by ghost_test.go; it exits when stdin closes or after 120 s.
import Cocoa

let pid = Int32(CommandLine.arguments[1])!
let app = NSApplication.shared
app.setActivationPolicy(.accessory)

guard let list = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as? [[String: Any]] else { exit(1) }
var target: [String: Any]?
for w in list where (w[kCGWindowOwnerPID as String] as? Int32) == pid && (w[kCGWindowLayer as String] as? Int) == 0 {
    let b = w[kCGWindowBounds as String] as? [String: Double] ?? [:]
    if (b["Width"] ?? 0) > 400 { target = w; break }
}
guard let t = target, let b = t[kCGWindowBounds as String] as? [String: Double], let num = t[kCGWindowNumber as String] as? Int else { exit(2) }
let screenH = NSScreen.screens[0].frame.height
let rect = NSRect(x: b["X"]!, y: screenH - b["Y"]! - b["Height"]!, width: b["Width"]!, height: b["Height"]!)
let win = NSWindow(contentRect: rect, styleMask: .borderless, backing: .buffered, defer: false)
win.backgroundColor = NSColor(calibratedRed: 0.55, green: 0.6, blue: 0.5, alpha: 1)
win.isOpaque = true
win.hasShadow = false
win.ignoresMouseEvents = true
win.order(.below, relativeTo: num)
DispatchQueue.global().async {
    _ = FileHandle.standardInput.readDataToEndOfFile()
    exit(0)
}
DispatchQueue.main.asyncAfter(deadline: .now() + 120) { exit(0) }
app.run()
