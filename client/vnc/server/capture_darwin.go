//go:build darwin && !ios

package server

import (
	"errors"
	"fmt"
	"hash/maphash"
	"image"
	"os"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	log "github.com/sirupsen/logrus"
)

var darwinCaptureOnce sync.Once

var (
	cgMainDisplayID              func() uint32
	cgDisplayPixelsWide          func(uint32) uintptr
	cgDisplayPixelsHigh          func(uint32) uintptr
	cgDisplayCreateImage         func(uint32) uintptr
	cgImageGetWidth              func(uintptr) uintptr
	cgImageGetHeight             func(uintptr) uintptr
	cgImageGetBytesPerRow        func(uintptr) uintptr
	cgImageGetBitsPerPixel       func(uintptr) uintptr
	cgImageGetDataProvider       func(uintptr) uintptr
	cgDataProviderCopyData       func(uintptr) uintptr
	cgImageRelease               func(uintptr)
	cfDataGetLength              func(uintptr) int64
	cfDataGetBytePtr             func(uintptr) uintptr
	cfRelease                    func(uintptr)
	cgRequestScreenCaptureAccess func() bool
	cgEventCreate                func(uintptr) uintptr
	cgEventGetLocation           func(uintptr) cgPoint
	darwinCaptureReady           bool
)

// cgPoint mirrors CoreGraphics CGPoint: two doubles, 16 bytes, returned
// in registers on Darwin amd64/arm64. Used to receive cursor coordinates
// from CGEventGetLocation via purego.
type cgPoint struct {
	X, Y float64
}

func initDarwinCapture() {
	darwinCaptureOnce.Do(func() {
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			log.Debugf("load CoreGraphics: %v", err)
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			log.Debugf("load CoreFoundation: %v", err)
			return
		}

		purego.RegisterLibFunc(&cgMainDisplayID, cg, "CGMainDisplayID")
		purego.RegisterLibFunc(&cgDisplayPixelsWide, cg, "CGDisplayPixelsWide")
		purego.RegisterLibFunc(&cgDisplayPixelsHigh, cg, "CGDisplayPixelsHigh")
		purego.RegisterLibFunc(&cgDisplayCreateImage, cg, "CGDisplayCreateImage")
		purego.RegisterLibFunc(&cgImageGetWidth, cg, "CGImageGetWidth")
		purego.RegisterLibFunc(&cgImageGetHeight, cg, "CGImageGetHeight")
		purego.RegisterLibFunc(&cgImageGetBytesPerRow, cg, "CGImageGetBytesPerRow")
		purego.RegisterLibFunc(&cgImageGetBitsPerPixel, cg, "CGImageGetBitsPerPixel")
		purego.RegisterLibFunc(&cgImageGetDataProvider, cg, "CGImageGetDataProvider")
		purego.RegisterLibFunc(&cgDataProviderCopyData, cg, "CGDataProviderCopyData")
		purego.RegisterLibFunc(&cgImageRelease, cg, "CGImageRelease")
		purego.RegisterLibFunc(&cfDataGetLength, cf, "CFDataGetLength")
		purego.RegisterLibFunc(&cfDataGetBytePtr, cf, "CFDataGetBytePtr")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")

		// CGRequestScreenCaptureAccess (macOS 11+) prompts on first call and
		// is a cheap no-op once granted. The Preflight companion is unreliable
		// on Sequoia (returns false even when access is granted), so we drive
		// the permission flow from actual capture failures instead.
		if sym, err := purego.Dlsym(cg, "CGRequestScreenCaptureAccess"); err == nil {
			purego.RegisterFunc(&cgRequestScreenCaptureAccess, sym)
		}
		// CGEventCreate / CGEventGetLocation feed the cursor position used
		// by remote-cursor compositing. Optional; absence reports as a
		// position-source error and disables that feature on this host.
		if sym, err := purego.Dlsym(cg, "CGEventCreate"); err == nil {
			purego.RegisterFunc(&cgEventCreate, sym)
		}
		if sym, err := purego.Dlsym(cg, "CGEventGetLocation"); err == nil {
			purego.RegisterFunc(&cgEventGetLocation, sym)
		}

		darwinCaptureReady = true
	})
}

// CGCapturer captures the macOS main display using Core Graphics.
type CGCapturer struct {
	displayID uint32
	w, h      int
	// downscale is 1 for pixel-perfect, 2 for Retina 2:1 box-filter downscale.
	downscale int
	hashSeed  maphash.Seed
	lastHash  uint64
	hasHash   bool
	// cursor lazily binds the private CGSCreateCurrentCursorImage symbol
	// so we can emit the Cursor pseudo-encoding without a per-frame cost
	// on builds that never query it.
	cursorOnce sync.Once
	cursor     *cgCursor
}

// Screen Recording state for this process. TCC decisions are per process and a
// grant only reaches a process that started after it was made, so all of this is
// process-wide rather than per capturer.
var (
	// screenRecordingAsks counts how often the question has been put to the user
	// here. The native call only shows its dialog on the first ask.
	screenRecordingAsks atomic.Int32
	// screenRecordingGranted holds the last answer TCC gave. It can be true
	// while capture still fails, which is precisely the case a restart fixes.
	screenRecordingGranted atomic.Bool
	// screenRecordingResolved is set once the question no longer needs the
	// screen: a capture succeeded, or the user has been asked. The input side
	// waits for it before asking for Accessibility, since macOS shows one
	// permission pane at a time and losing the Screen Recording one is the worse
	// outcome: without it there is no picture.
	screenRecordingResolved atomic.Bool
	// screenRecordingPaneShown keeps the Settings escalation to once per process.
	screenRecordingPaneShown atomic.Bool
	// captureRestartPending is set once the process has been asked to exit, so
	// nothing else opens a dialog that the exit would tear down.
	captureRestartPending atomic.Bool
)

// PrimeScreenCapturePermission asks for Screen Recording without creating a full
// capturer, so the request comes from the agent's startup rather than from the
// middle of a session. The native call is a no-op once a decision exists, so
// calling it on every agent start is cheap and safe.
func PrimeScreenCapturePermission() {
	initDarwinCapture()
	if !darwinCaptureReady {
		return
	}
	requestScreenRecording()
}

// requestScreenRecording puts the Screen Recording question to the user and
// reports what TCC answered, plus whether this was the first ask in this
// process. The native call shows its dialog on the first ask only and returns
// the standing decision on every later one.
func requestScreenRecording() (granted, first bool) {
	if cgRequestScreenCaptureAccess == nil {
		return false, false
	}
	granted = cgRequestScreenCaptureAccess()
	first = screenRecordingAsks.Add(1) == 1
	screenRecordingGranted.Store(granted)
	screenRecordingResolved.Store(true)
	return granted, first
}

// notifyScreenRecordingMissing asks for Screen Recording after a capture failed.
//
// The native dialog carries its own "Open System Settings" button, so opening the
// pane alongside it would leave two things on screen for one decision. The pane
// is for the cases where asking cannot help: a host with no prompting symbol
// here, and an unanswered dialog in showScreenRecordingPane.
func notifyScreenRecordingMissing() {
	if cgRequestScreenCaptureAccess == nil {
		showScreenRecordingPane()
		screenRecordingResolved.Store(true)
		return
	}
	granted, first := requestScreenRecording()
	switch {
	case granted:
		// The grant exists but arrived after this process started, so it cannot
		// capture with it. Restarting is the only way out, see maybeGiveUpLocked.
		log.Info("Screen Recording is granted but not in effect for this process")
	case first:
		log.Warn("Screen Recording permission not granted; approve the prompt to share the screen")
	default:
		log.Debug("Screen Recording permission still not granted")
	}
}

// showScreenRecordingPane opens the Screen Recording pane of System Settings
// once per process. Used when the dialog cannot appear or has gone unanswered
// long enough that it is no longer competing for the user's attention.
func showScreenRecordingPane() {
	if !screenRecordingPaneShown.CompareAndSwap(false, true) {
		return
	}
	openPrivacyPane("Privacy_ScreenCapture")
	log.Warn("Screen Recording permission not granted. " +
		"Opened System Settings > Privacy & Security > Screen Recording; enable netbird there.")
}

// NewCGCapturer creates a screen capturer for the main display.
func NewCGCapturer() (*CGCapturer, error) {
	initDarwinCapture()
	if !darwinCaptureReady {
		return nil, fmt.Errorf("CoreGraphics not available")
	}

	displayID := cgMainDisplayID()
	c := &CGCapturer{displayID: displayID, downscale: 1, hashSeed: maphash.MakeSeed()}

	img, err := c.Capture()
	if err != nil {
		notifyScreenRecordingMissing()
		return nil, fmt.Errorf("probe capture: %w", err)
	}
	// A frame is the only trustworthy grant signal: CGPreflight lies on Sequoia.
	screenRecordingGranted.Store(true)
	screenRecordingResolved.Store(true)
	nativeW := img.Rect.Dx()
	nativeH := img.Rect.Dy()
	c.hasHash = false
	if nativeW == 0 || nativeH == 0 {
		return nil, errors.New("display dimensions are zero")
	}

	logicalW := int(cgDisplayPixelsWide(displayID))
	logicalH := int(cgDisplayPixelsHigh(displayID))

	// Enable 2:1 downscale on Retina unless explicitly disabled. Cuts pixel
	// count 4x, shrinking convert, diff, and wire data proportionally.
	if !retinaDownscaleDisabled() && nativeW >= 2*logicalW && nativeH >= 2*logicalH && nativeW%2 == 0 && nativeH%2 == 0 {
		c.downscale = 2
	}
	c.w = nativeW / c.downscale
	c.h = nativeH / c.downscale

	log.Infof("macOS capturer ready: %dx%d (native %dx%d, logical %dx%d, downscale=%d, display=%d)",
		c.w, c.h, nativeW, nativeH, logicalW, logicalH, c.downscale, displayID)
	return c, nil
}

func retinaDownscaleDisabled() bool {
	v := os.Getenv(EnvVNCDisableDownscale)
	if v == "" {
		return false
	}
	disabled, err := strconv.ParseBool(v)
	if err != nil {
		log.Warnf("parse %s: %v", EnvVNCDisableDownscale, err)
		return false
	}
	return disabled
}

// Width returns the screen width.
func (c *CGCapturer) Width() int { return c.w }

// Height returns the screen height.
func (c *CGCapturer) Height() int { return c.h }

// CaptureInto writes a fresh frame directly into dst, skipping the
// per-frame image.RGBA allocation that Capture() does. It always fills
// dst: the capturer is shared across all sessions, so dedup here would
// starve every consumer but the first one to poll after a change.
// Per-session prevFrame diffing in the session layer handles no-op frames.
func (c *CGCapturer) CaptureInto(dst *image.RGBA) error {
	cgImage := cgDisplayCreateImage(c.displayID)
	if cgImage == 0 {
		return fmt.Errorf("CGDisplayCreateImage returned nil (screen recording permission?)")
	}
	defer cgImageRelease(cgImage)
	w := int(cgImageGetWidth(cgImage))
	h := int(cgImageGetHeight(cgImage))
	bytesPerRow := int(cgImageGetBytesPerRow(cgImage))
	bpp := int(cgImageGetBitsPerPixel(cgImage))
	provider := cgImageGetDataProvider(cgImage)
	if provider == 0 {
		return fmt.Errorf("CGImageGetDataProvider returned nil")
	}
	cfData := cgDataProviderCopyData(provider)
	if cfData == 0 {
		return fmt.Errorf("CGDataProviderCopyData returned nil")
	}
	defer cfRelease(cfData)
	dataLen := int(cfDataGetLength(cfData))
	dataPtr := cfDataGetBytePtr(cfData)
	if dataPtr == 0 || dataLen == 0 {
		return fmt.Errorf("empty image data")
	}
	src := unsafe.Slice((*byte)(unsafe.Pointer(dataPtr)), dataLen)

	ds := c.downscale
	if ds < 1 {
		ds = 1
	}
	outW := w / ds
	outH := h / ds
	if dst.Rect.Dx() != outW || dst.Rect.Dy() != outH {
		return fmt.Errorf("dst size mismatch: dst=%dx%d capturer=%dx%d",
			dst.Rect.Dx(), dst.Rect.Dy(), outW, outH)
	}
	bytesPerPixel := bpp / 8
	if bytesPerPixel == 4 && ds == 1 {
		convertBGRAToRGBA(dst.Pix, dst.Stride, src, bytesPerRow, w, h)
		return nil
	}
	if bytesPerPixel == 4 && ds == 2 {
		convertBGRAToRGBADownscale2(dst.Pix, dst.Stride, src, bytesPerRow, outW, outH)
		return nil
	}
	for row := 0; row < outH; row++ {
		srcOff := row * ds * bytesPerRow
		dstOff := row * dst.Stride
		for col := 0; col < outW; col++ {
			si := srcOff + col*ds*bytesPerPixel
			di := dstOff + col*4
			dst.Pix[di+0] = src[si+2]
			dst.Pix[di+1] = src[si+1]
			dst.Pix[di+2] = src[si+0]
			dst.Pix[di+3] = 0xff
		}
	}
	return nil
}

func (c *CGCapturer) Capture() (*image.RGBA, error) {
	cgImage := cgDisplayCreateImage(c.displayID)
	if cgImage == 0 {
		return nil, fmt.Errorf("CGDisplayCreateImage returned nil (screen recording permission?)")
	}
	defer cgImageRelease(cgImage)

	w := int(cgImageGetWidth(cgImage))
	h := int(cgImageGetHeight(cgImage))
	bytesPerRow := int(cgImageGetBytesPerRow(cgImage))
	bpp := int(cgImageGetBitsPerPixel(cgImage))

	provider := cgImageGetDataProvider(cgImage)
	if provider == 0 {
		return nil, fmt.Errorf("CGImageGetDataProvider returned nil")
	}

	cfData := cgDataProviderCopyData(provider)
	if cfData == 0 {
		return nil, fmt.Errorf("CGDataProviderCopyData returned nil")
	}
	defer cfRelease(cfData)

	dataLen := int(cfDataGetLength(cfData))
	dataPtr := cfDataGetBytePtr(cfData)
	if dataPtr == 0 || dataLen == 0 {
		return nil, fmt.Errorf("empty image data")
	}

	src := unsafe.Slice((*byte)(unsafe.Pointer(dataPtr)), dataLen)

	hash := maphash.Bytes(c.hashSeed, src)
	if c.hasHash && hash == c.lastHash {
		return nil, errFrameUnchanged
	}
	c.lastHash = hash
	c.hasHash = true

	ds := c.downscale
	if ds < 1 {
		ds = 1
	}
	outW := w / ds
	outH := h / ds
	img := image.NewRGBA(image.Rect(0, 0, outW, outH))

	bytesPerPixel := bpp / 8
	switch {
	case bytesPerPixel == 4 && ds == 1:
		convertBGRAToRGBA(img.Pix, img.Stride, src, bytesPerRow, w, h)
	case bytesPerPixel == 4 && ds == 2:
		convertBGRAToRGBADownscale2(img.Pix, img.Stride, src, bytesPerRow, outW, outH)
	default:
		convertBGRAToRGBAGeneric(img.Pix, img.Stride, src, bytesPerRow, bgraDownscaleParams{outW: outW, outH: outH, bytesPerPixel: bytesPerPixel, ds: ds})
	}

	return img, nil
}

type bgraDownscaleParams struct {
	outW, outH, bytesPerPixel, ds int
}

// convertBGRAToRGBAGeneric is the slow per-pixel fallback for non-4-bytes
// or non-1/2 downscale formats. Always available regardless of the source
// format quirks the fast paths optimize for.
func convertBGRAToRGBAGeneric(dst []byte, dstStride int, src []byte, srcStride int, p bgraDownscaleParams) {
	for row := 0; row < p.outH; row++ {
		srcOff := row * p.ds * srcStride
		dstOff := row * dstStride
		for col := 0; col < p.outW; col++ {
			si := srcOff + col*p.ds*p.bytesPerPixel
			di := dstOff + col*4
			dst[di+0] = src[si+2]
			dst[di+1] = src[si+1]
			dst[di+2] = src[si+0]
			dst[di+3] = 0xff
		}
	}
}

// convertBGRAToRGBADownscale2 averages every 2x2 BGRA block into one RGBA
// output pixel, parallelised across GOMAXPROCS cores. outW and outH are the
// destination dimensions (source is 2*outW by 2*outH).
func convertBGRAToRGBADownscale2(dst []byte, dstStride int, src []byte, srcStride, outW, outH int) {
	workers := runtime.GOMAXPROCS(0)
	if workers > outH {
		workers = outH
	}
	if workers < 1 || outH < 32 {
		workers = 1
	}

	convertRows := func(y0, y1 int) {
		for row := y0; row < y1; row++ {
			srcRow0 := 2 * row * srcStride
			srcRow1 := srcRow0 + srcStride
			dstOff := row * dstStride
			for col := 0; col < outW; col++ {
				s0 := srcRow0 + col*8
				s1 := srcRow1 + col*8
				b := (uint32(src[s0]) + uint32(src[s0+4]) + uint32(src[s1]) + uint32(src[s1+4])) >> 2
				g := (uint32(src[s0+1]) + uint32(src[s0+5]) + uint32(src[s1+1]) + uint32(src[s1+5])) >> 2
				r := (uint32(src[s0+2]) + uint32(src[s0+6]) + uint32(src[s1+2]) + uint32(src[s1+6])) >> 2
				di := dstOff + col*4
				dst[di+0] = byte(r)
				dst[di+1] = byte(g)
				dst[di+2] = byte(b)
				dst[di+3] = 0xff
			}
		}
	}

	if workers == 1 {
		convertRows(0, outH)
		return
	}

	var wg sync.WaitGroup
	chunk := (outH + workers - 1) / workers
	for i := 0; i < workers; i++ {
		y0 := i * chunk
		y1 := y0 + chunk
		if y1 > outH {
			y1 = outH
		}
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			convertRows(y0, y1)
		}(y0, y1)
	}
	wg.Wait()
}

// convertBGRAToRGBA swaps R/B channels using uint32 word operations, and
// parallelises across GOMAXPROCS cores for large images.
func convertBGRAToRGBA(dst []byte, dstStride int, src []byte, srcStride, w, h int) {
	workers := runtime.GOMAXPROCS(0)
	if workers > h {
		workers = h
	}
	if workers < 1 || h < 64 {
		workers = 1
	}

	convertRows := func(y0, y1 int) {
		rowBytes := w * 4
		for row := y0; row < y1; row++ {
			dstRow := dst[row*dstStride : row*dstStride+rowBytes]
			srcRow := src[row*srcStride : row*srcStride+rowBytes]
			dstU := unsafe.Slice((*uint32)(unsafe.Pointer(&dstRow[0])), w)
			srcU := unsafe.Slice((*uint32)(unsafe.Pointer(&srcRow[0])), w)
			for i, p := range srcU {
				dstU[i] = (p & 0xff00ff00) | ((p & 0x000000ff) << 16) | ((p & 0x00ff0000) >> 16) | 0xff000000
			}
		}
	}

	if workers == 1 {
		convertRows(0, h)
		return
	}

	var wg sync.WaitGroup
	chunk := (h + workers - 1) / workers
	for i := 0; i < workers; i++ {
		y0 := i * chunk
		y1 := y0 + chunk
		if y1 > h {
			y1 = h
		}
		if y0 >= y1 {
			break
		}
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			convertRows(y0, y1)
		}(y0, y1)
	}
	wg.Wait()
}

// MacPoller wraps CGCapturer with a staleness-cached on-demand Capture:
// sessions drive captures themselves from their encoder goroutine, so we
// don't need a background ticker. The last result is cached for a short
// window so concurrent sessions coalesce into one capture.
//
// The capturer is allocated lazily on first use and released when all
// clients disconnect. Init is retried with backoff because the user may
// grant Screen Recording permission while the server is already running.
type MacPoller struct {
	mu sync.Mutex

	capturer *CGCapturer
	w, h     int

	lastFrame *image.RGBA
	lastAt    time.Time

	clients          atomic.Int32
	initFails        int
	initBackoffUntil time.Time
	closed           bool

	// captured records that a frame was produced since the current clients
	// connected. Reset per connection rather than latched for the process
	// lifetime, so a permission revoked between sessions is noticed.
	captured bool
	// firstFailAt is when the current run of init failures started.
	firstFailAt time.Time
	// gaveUp keeps the restart request to one per process.
	gaveUp bool

	// giveUp is called when capture can no longer be expected to start in this
	// process. Only the per-user agent sets it, where exiting is cheap and the
	// service respawns on the next connection; the daemon leaves it nil.
	giveUp func()
}

const (
	// macCaptureGiveUpWindow is how long capture may keep failing to start before
	// this process counts as unable to capture at all. Long enough that a user
	// still deciding on the prompt the first failure raised is not cut off by the
	// restart.
	macCaptureGiveUpWindow = 30 * time.Second
	// macCaptureGrantedRetries is how many failures a process with the grant in
	// hand takes before restarting. Enough to tell a stale grant, which only a
	// restart fixes, from a display that momentarily had no frame to give.
	macCaptureGrantedRetries = 2
)

// OnCaptureUnavailable registers a callback for when capture cannot start in
// this process, so the caller can restart to pick up a permission change.
func (p *MacPoller) OnCaptureUnavailable(fn func()) {
	p.mu.Lock()
	p.giveUp = fn
	p.mu.Unlock()
}

// macInitRetryBackoffFor returns the delay we wait between init attempts
// after consecutive failures. Screen Recording permission is a one-shot
// user grant, so after several failures we back off aggressively.
func macInitRetryBackoffFor(fails int) time.Duration {
	switch {
	case fails > 15:
		return 30 * time.Second
	case fails > 5:
		return 10 * time.Second
	default:
		return 2 * time.Second
	}
}

// NewMacPoller creates a lazy on-demand capturer for the macOS display.
func NewMacPoller() *MacPoller {
	return &MacPoller{}
}

// Wake is a no-op retained for API compatibility. With on-demand capture
// there is no background retry loop to kick: init happens on the next
// Capture/ClientConnect call.
func (p *MacPoller) Wake() {
	// intentional no-op
}

// ClientConnect increments the active client count and eagerly initialises
// the capturer so the first FBUpdateRequest doesn't pay the init cost.
func (p *MacPoller) ClientConnect() {
	if p.clients.Add(1) == 1 {
		p.mu.Lock()
		// Start the permission bookkeeping over for this session: whether capture
		// works has to be judged against the grants as they are now, not against
		// a frame produced before the user changed them.
		p.captured = false
		p.initFails = 0
		p.firstFailAt = time.Time{}
		p.initBackoffUntil = time.Time{}
		_ = p.ensureCapturerLocked()
		p.mu.Unlock()
	}
}

// ClientDisconnect decrements the active client count. On the last
// disconnect the capturer is released.
func (p *MacPoller) ClientDisconnect() {
	if p.clients.Add(-1) == 0 {
		p.mu.Lock()
		p.capturer = nil
		p.lastFrame = nil
		p.mu.Unlock()
	}
}

// Close releases all resources.
func (p *MacPoller) Close() {
	p.mu.Lock()
	p.closed = true
	p.capturer = nil
	p.lastFrame = nil
	p.mu.Unlock()
}

// Width returns the screen width. Triggers lazy init if needed.
func (p *MacPoller) Width() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.ensureCapturerLocked()
	return p.w
}

// Height returns the screen height. Triggers lazy init if needed.
func (p *MacPoller) Height() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	_ = p.ensureCapturerLocked()
	return p.h
}

// CaptureInto fills dst directly via the underlying capturer, bypassing
// the freshness cache.
func (p *MacPoller) CaptureInto(dst *image.RGBA) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.ensureCapturerLocked(); err != nil {
		return err
	}
	if err := p.capturer.CaptureInto(dst); err != nil {
		p.capturer = nil
		return fmt.Errorf("macos capture: %w", err)
	}
	return nil
}

// Capture returns a fresh frame, serving from the short-lived cache if a
// previous caller captured within freshWindow. Handles the
// errFrameUnchanged return from CGCapturer by reusing the cached frame.
func (p *MacPoller) Capture() (*image.RGBA, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.lastFrame != nil && time.Since(p.lastAt) < freshWindow {
		return p.lastFrame, nil
	}
	if err := p.ensureCapturerLocked(); err != nil {
		return nil, err
	}
	img, err := p.capturer.Capture()
	if errors.Is(err, errFrameUnchanged) {
		if p.lastFrame != nil {
			p.lastAt = time.Now()
			return p.lastFrame, nil
		}
		return nil, err
	}
	if err != nil {
		// Drop the capturer so the next call retries init; the display stream
		// can die if the session changes or permissions are revoked.
		p.capturer = nil
		return nil, fmt.Errorf("macos capture: %w", err)
	}
	p.lastFrame = img
	p.lastAt = time.Now()
	return img, nil
}

// ensureCapturerLocked initialises the underlying CGCapturer if needed.
// Caller must hold p.mu.
func (p *MacPoller) ensureCapturerLocked() error {
	if p.closed {
		return fmt.Errorf("poller closed")
	}
	if p.capturer != nil {
		return nil
	}
	if time.Now().Before(p.initBackoffUntil) {
		return fmt.Errorf("macOS capturer unavailable (retry scheduled)")
	}
	c, err := NewCGCapturer()
	if err != nil {
		p.initFails++
		p.initBackoffUntil = time.Now().Add(macInitRetryBackoffFor(p.initFails))
		if p.firstFailAt.IsZero() {
			p.firstFailAt = time.Now()
		}
		p.maybeGiveUpLocked()
		if p.initFails == 1 || p.initFails%10 == 0 {
			log.Warnf("macOS capturer: %v (attempt %d)", err, p.initFails)
		} else {
			log.Debugf("macOS capturer: %v (attempt %d)", err, p.initFails)
		}
		return err
	}
	p.initFails = 0
	p.firstFailAt = time.Time{}
	p.captured = true
	p.capturer = c
	p.w, p.h = c.Width(), c.Height()
	return nil
}

// maybeGiveUpLocked asks the process to restart when capture cannot be made to
// work in it. TCC prompts once per process and a Screen Recording grant only
// reaches a process that started after it, so retrying in place cannot recover
// either a grant made just now or one the user has taken away. Only the agent
// registers the hook; the daemon has nothing to gain from exiting.
// Caller must hold p.mu.
func (p *MacPoller) maybeGiveUpLocked() {
	if p.giveUp == nil || p.gaveUp || p.captured {
		return
	}
	if screenRecordingGranted.Load() && p.initFails >= macCaptureGrantedRetries {
		log.Info("Screen Recording is granted but capture does not work in this process; restarting to use it")
		p.restartLocked()
		return
	}
	if time.Since(p.firstFailAt) < macCaptureGiveUpWindow {
		return
	}
	// Nothing was granted and the dialog has been on screen long enough to count
	// as unanswered, so hand the user Settings before the process goes away.
	showScreenRecordingPane()
	log.Warnf("macOS capturer has not started for %s; restarting so the permission can be asked again",
		macCaptureGiveUpWindow)
	p.restartLocked()
}

// restartLocked publishes that the process is on its way out before triggering
// it, so nothing opens a permission dialog that the exit would tear down.
// Caller must hold p.mu.
func (p *MacPoller) restartLocked() {
	p.gaveUp = true
	captureRestartPending.Store(true)
	p.giveUp()
}

var _ ScreenCapturer = (*MacPoller)(nil)
