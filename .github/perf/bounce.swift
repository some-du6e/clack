// bounce.swift: the Dock's launch bounce, timed from bounce.sh's film.
//
// usage: swift bounce.swift <movie> <outdir>
//
// Every frame's bottom band (the Dock and the space above it) is compared
// with the first frame's, before the launch: the topmost row that changed
// is the bouncing icon's top. Its height above its place in the Dock (the
// lowest top once it is in), frame by frame, is one bounce after another:
// each bounce starts as the icon leaves its place, peaks, and lands. The
// bounce's length is from leaving to landing; the period is from one
// leaving to the next. bounce.csv has the series.
import AVFoundation
import CoreVideo
import Foundation

let args = CommandLine.arguments
guard args.count >= 3 else {
    print("usage: bounce.swift <movie> <outdir>")
    exit(2)
}
let asset = AVURLAsset(url: URL(fileURLWithPath: args[1]))
let outDir = URL(fileURLWithPath: args[2])
guard let track = asset.tracks(withMediaType: .video).first else {
    print("bounce: no video track")
    exit(1)
}
print("bounce: video \(Int(track.naturalSize.width))x\(Int(track.naturalSize.height)), nominal \(track.nominalFrameRate) fps, \(CMTimeGetSeconds(asset.duration)) s")
let reader: AVAssetReader
do {
    reader = try AVAssetReader(asset: asset)
} catch {
    print("bounce: \(error)")
    exit(1)
}
let output = AVAssetReaderTrackOutput(track: track, outputSettings: [kCVPixelBufferPixelFormatTypeKey as String: Int(kCVPixelFormatType_32BGRA)])
output.alwaysCopiesSampleData = false
reader.add(output)
guard reader.startReading() else {
    print("bounce: can't read the film: \(String(describing: reader.error))")
    exit(1)
}

var ref: [UInt8] = []
var width = 0, band = 0, y0 = 0
var series: [(t: Double, top: Int)] = []
while let sb = output.copyNextSampleBuffer() {
    guard let pb = CMSampleBufferGetImageBuffer(sb) else { continue }
    let t = CMTimeGetSeconds(CMSampleBufferGetPresentationTimeStamp(sb))
    CVPixelBufferLockBaseAddress(pb, .readOnly)
    let w = CVPixelBufferGetWidth(pb), h = CVPixelBufferGetHeight(pb), bpr = CVPixelBufferGetBytesPerRow(pb)
    guard let raw = CVPixelBufferGetBaseAddress(pb) else {
        CVPixelBufferUnlockBaseAddress(pb, .readOnly)
        continue
    }
    let base = raw.assumingMemoryBound(to: UInt8.self)
    if ref.isEmpty {
        width = w
        band = min(h / 3, h)
        y0 = h - band
    }
    var cur = [UInt8](repeating: 0, count: width * band * 4)
    cur.withUnsafeMutableBytes { dst in
        for y in 0..<band {
            memcpy(dst.baseAddress! + y * width * 4, base + (y0 + y) * bpr, width * 4)
        }
    }
    CVPixelBufferUnlockBaseAddress(pb, .readOnly)
    if ref.isEmpty {
        ref = cur
        series.append((t, -1))
        continue
    }
    var top = -1
    rows: for y in 0..<band {
        let o = y * width * 4
        var x = 0
        while x < width {
            let i = o + x * 4
            let d = abs(Int(cur[i]) - Int(ref[i])) + abs(Int(cur[i + 1]) - Int(ref[i + 1])) + abs(Int(cur[i + 2]) - Int(ref[i + 2]))
            if d > 90 {
                top = y
                break rows
            }
            x += 1
        }
    }
    series.append((t, top))
}
print("bounce: \(series.count) frames, band rows \(y0)-\(y0 + band)")
var csv = "t,top\n"
for s in series { csv += String(format: "%.4f,%d\n", s.t, s.top < 0 ? -1 : s.top + y0) }
try? csv.write(to: outDir.appendingPathComponent("bounce.csv"), atomically: true, encoding: .utf8)

let tops = series.filter { $0.top >= 0 }.map { $0.top }
guard !tops.isEmpty else {
    print("bounce: nothing changed in the Dock band (no Dock on screen, or no Screen Recording)")
    exit(1)
}
// the icon at rest: the lowest top seen in at least a few frames
var counts: [Int: Int] = [:]
for t in tops { counts[t, default: 0] += 1 }
let rest = counts.filter { $0.value >= 3 }.keys.max() ?? tops.max()!
let heights = series.map { (t: $0.t, h: $0.top < 0 ? 0 : max(0, rest - $0.top)) }
let peak = heights.map { $0.h }.max() ?? 0
print("bounce: rest top row \(rest + y0), highest \(peak) px above it")
guard peak >= 4 else {
    print("bounce: the icon never left its place")
    exit(1)
}
// bounces: runs of frames more than 2 px up (or 10% of the peak)
let up = max(2, peak / 10)
var bounces: [(start: Double, peakAt: Double, peakH: Int, land: Double)] = []
var i = 0
while i < heights.count {
    if heights[i].h > up {
        let s = i > 0 ? heights[i - 1].t : heights[i].t
        var j = i, pk = i
        while j < heights.count && heights[j].h > up {
            if heights[j].h > heights[pk].h { pk = j }
            j += 1
        }
        let land = j < heights.count ? heights[j].t : heights[j - 1].t
        bounces.append((s, heights[pk].t, heights[pk].h, land))
        i = j
    } else {
        i += 1
    }
}
for (n, b) in bounces.enumerated() {
    print(String(format: "bounce %d: leaves %.3f s, peaks %.3f s at %d px, lands %.3f s: %.0f ms up and down", n + 1, b.start, b.peakAt, b.peakH, b.land, (b.land - b.start) * 1000))
}
func median(_ v: [Double]) -> Double {
    let s = v.sorted()
    return s.isEmpty ? .nan : (s.count % 2 == 1 ? s[s.count / 2] : (s[s.count / 2 - 1] + s[s.count / 2]) / 2)
}
let full = bounces.count > 2 ? Array(bounces.dropLast()) : bounces // the last one may be cut by the launch ending
let lengths = full.map { ($0.land - $0.start) * 1000 }
var periods: [Double] = []
if full.count > 1 {
    for k in 1..<full.count { periods.append((full[k].start - full[k - 1].start) * 1000) }
}
print(String(format: "bounce: %d bounces; one lasts %.0f ms (median, leaving to landing), one starts every %.0f ms (median)", bounces.count, median(lengths), median(periods)))
