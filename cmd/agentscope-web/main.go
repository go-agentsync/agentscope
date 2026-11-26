// Command agentscope-web is the browser half of agentscope: the wasm canvas
// driver. It forwards mouse, wheel and keyboard input to the ui.Scene, blits
// the scene onto a <canvas>, and feeds it the snapshots a collector streams
// over gRPC-on-WebSocket (github.com/grpc-transports/websocket). This is the
// only file with a build tag; the ViewModel and the View are in package ui and
// are covered natively.
//
//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"

	"github.com/go-agentsync/agentscope/model"
	"github.com/go-agentsync/agentscope/ui"
	"github.com/go-agentsync/agentscope/wire"
)

func main() {
	doc := js.Global().Get("document")
	canvas := doc.Call("getElementById", "screen")
	if canvas.IsUndefined() || canvas.IsNull() {
		println("agentscope-web: no #screen canvas in the host page")
		return
	}
	ctx2d := canvas.Call("getContext", "2d")
	// Go's wasm runtime has no time zone: show the browser's.
	_, off := time.Now().Zone()
	if o := -js.Global().Get("Date").New().Call("getTimezoneOffset").Int() * 60; o != off {
		time.Local = time.FixedZone("browser", o)
	}

	var (
		mu        sync.Mutex
		local     []byte
		imageData js.Value
		dst       js.Value
		scene     = ui.NewScene(ui.DefaultW, ui.DefaultH)
		feed      = ui.NewFeed(scene)
		lastDPR   float64
	)
	feed.SetHidden(doc.Get("hidden").Bool())
	// agentscopeStats lets a measurement read how many full redraws were made and
	// what the wasm heap holds (a Chrome DevTools Runtime.evaluate, or the console).
	js.Global().Set("agentscopeStats", js.FuncOf(func(js.Value, []js.Value) any {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return fmt.Sprintf(`{"renders":%d,"heapAlloc":%d,"heapSys":%d,"numGC":%d,"canvasW":%d,"canvasH":%d}`,
			renders.Load(), m.HeapAlloc, m.HeapSys, m.NumGC, canvas.Get("width").Int(), canvas.Get("height").Int())
	}))
	render := func() {
		renders.Add(1)
		scene.Draw(local)
		js.CopyBytesToJS(dst, local)
		ctx2d.Call("putImageData", imageData, 0, 0)
	}
	// fit makes the canvas the viewport, in device pixels, and lays the scene out
	// again. The caller holds the lock.
	fit := func() {
		root := doc.Get("documentElement")
		cssW, cssH := root.Get("clientWidth").Int(), root.Get("clientHeight").Int()
		// The backing store is capped (ui.MaxBackingPixels): a 5K display at 2x
		// would otherwise be 30 M pixels, and the compositor keeps copies.
		dpr := ui.CapDPR(cssW, cssH, js.Global().Get("devicePixelRatio").Float())
		w := int(float64(cssW)*dpr + 0.5)
		h := int(float64(cssH)*dpr + 0.5)
		if w < 1 || h < 1 {
			return
		}
		if cw, ch := canvas.Get("width").Int(), canvas.Get("height").Int(); cw == w && ch == h && dpr == lastDPR {
			return // a resize event that changed nothing: no new buffers, no redraw
		}
		lastDPR = dpr
		canvas.Set("width", w)
		canvas.Set("height", h)
		// One pixel buffer and one ImageData serve every frame; they are made
		// again only when the size changes, and the Go buffer is reused when it
		// is already large enough.
		if n := 4 * w * h; cap(local) >= n {
			local = local[:n]
		} else {
			local = make([]byte, n)
		}
		imageData = ctx2d.Call("createImageData", w, h)
		dst = imageData.Get("data")
		scene.Resize(w, h, dpr)
		if !feed.Hidden() {
			render()
		}
	}
	mu.Lock()
	fit()
	mu.Unlock()
	js.Global().Call("addEventListener", "resize", js.FuncOf(func(js.Value, []js.Value) any {
		mu.Lock()
		defer mu.Unlock()
		fit()
		return nil
	}))
	// do runs fn under the scene lock and repaints when it says so.
	do := func(fn func() bool) {
		mu.Lock()
		defer mu.Unlock()
		// A hidden tab draws nothing: its state still follows (the dirty flag stays
		// set), and the one redraw happens when it is visible again.
		if (fn() || scene.Dirty()) && !feed.Hidden() {
			render()
		}
	}
	// Visibility: hidden stops all drawing (snapshots are held, only the newest);
	// visible applies the held one and draws once.
	doc.Call("addEventListener", "visibilitychange", js.FuncOf(func(js.Value, []js.Value) any {
		hidden := doc.Get("hidden").Bool()
		do(func() bool { feed.SetHidden(hidden); return !hidden })
		return nil
	}))
	// A change of devicePixelRatio (a window moved to another screen, a zoom) has
	// no event of its own: a resolution media query does, and is armed again for
	// the new ratio each time.
	var watchDPR func()
	watchDPR = func() {
		q := js.Global().Call("matchMedia", fmt.Sprintf("(resolution: %fdppx)", js.Global().Get("devicePixelRatio").Float()))
		var cb js.Func
		cb = js.FuncOf(func(js.Value, []js.Value) any {
			q.Call("removeEventListener", "change", cb)
			cb.Release()
			mu.Lock()
			fit()
			mu.Unlock()
			watchDPR()
			return nil
		})
		q.Call("addEventListener", "change", cb)
	}
	watchDPR()

	coords := func(e js.Value) (int, int) {
		rect := canvas.Call("getBoundingClientRect")
		sx := float64(canvas.Get("width").Int()) / rect.Get("width").Float()
		sy := float64(canvas.Get("height").Int()) / rect.Get("height").Float()
		return int((e.Get("clientX").Float() - rect.Get("left").Float()) * sx),
			int((e.Get("clientY").Float() - rect.Get("top").Float()) * sy)
	}
	on := func(name string, h func(x, y int) bool) {
		canvas.Call("addEventListener", name, js.FuncOf(func(_ js.Value, a []js.Value) any {
			if len(a) > 0 {
				x, y := coords(a[0])
				do(func() bool { return h(x, y) })
			}
			return nil
		}))
	}
	canvas.Call("addEventListener", "mousedown", js.FuncOf(func(_ js.Value, a []js.Value) any {
		if len(a) > 0 && a[0].Get("button").Int() == 0 {
			x, y := coords(a[0])
			do(func() bool { return scene.HandleClick(x, y) })
		}
		return nil
	}))
	// The pointer over the panel's divider shows the toolkit's resize cursor.
	canvas.Call("addEventListener", "mousemove", js.FuncOf(func(_ js.Value, a []js.Value) any {
		if len(a) > 0 {
			x, y := coords(a[0])
			do(func() bool {
				changed := scene.HandleMove(x, y)
				canvas.Get("style").Set("cursor", scene.Cursor(x, y))
				return changed
			})
		}
		return nil
	}))
	on("mouseup", scene.HandleRelease)
	canvas.Call("addEventListener", "wheel", js.FuncOf(func(_ js.Value, a []js.Value) any {
		if len(a) == 0 {
			return nil
		}
		e := a[0]
		e.Call("preventDefault")
		x, y := coords(e)
		d := 3
		if e.Get("deltaY").Float() < 0 {
			d = -3
		}
		do(func() bool { return scene.HandleScroll(x, y, d) })
		return nil
	}), map[string]any{"passive": false})
	js.Global().Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, a []js.Value) any {
		if len(a) == 0 {
			return nil
		}
		e := a[0]
		key := e.Get("key").String()
		do(func() bool {
			var changed bool
			if len([]rune(key)) == 1 && !e.Get("ctrlKey").Bool() && !e.Get("metaKey").Bool() && !e.Get("altKey").Bool() {
				changed = scene.HandleChar(key)
			} else {
				changed = scene.HandleKeyDown(key)
			}
			if changed {
				e.Call("preventDefault")
			}
			return changed
		})
		return nil
	}))

	// The system's light/dark preference drives the Slate variant, now and when
	// it changes.
	dark := js.Global().Call("matchMedia", "(prefers-color-scheme: dark)")
	follow := func() { do(func() bool { scene.VM().Prefs.SetSystemDark(dark.Get("matches").Bool()); return true }) }
	dark.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any { follow(); return nil }))
	follow()
	// The browser's languages, most preferred first, pick the language while the
	// choice is Auto.
	var navLangs []string
	if ls := js.Global().Get("navigator").Get("languages"); ls.Truthy() {
		for i := 0; i < ls.Length(); i++ {
			navLangs = append(navLangs, ls.Index(i).String())
		}
	}
	do(func() bool { scene.VM().Prefs.SetNavLangs(navLangs); return true })
	// The theme and language choices are the owner's: kept by the server, sent the
	// same way as a colour.
	scene.VM().Prefs.Send = func(theme, lang string) {
		go func() {
			c := conn.Load()
			if c == nil {
				return // not connected: the choice stays on this page until the next change
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = c.SetPrefs(ctx, theme, lang)
		}()
	}
	// The Rename dialog asks the server over the same connection as the stream. The
	// request goes out from its own goroutine: the browser's event loop has to
	// keep running for it to be sent, so the click handler must not wait for it.
	scene.VM().Rename.Send = func(key, name string) {
		go func() {
			var err error
			if c := conn.Load(); c == nil {
				err = errors.New(ui.T("err.notconnected"))
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err = c.Rename(ctx, key, name)
			}
			do(func() bool { scene.VM().Rename.Done(err); return true })
		}()
	}
	// The Colour dialog does the same: one request, answered through Done.
	scene.VM().Color.Send = func(key, color string) {
		go func() {
			var err error
			if c := conn.Load(); c == nil {
				err = errors.New(ui.T("err.notconnected"))
			} else {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				err = c.SetColor(ctx, key, color)
			}
			do(func() bool { scene.VM().Color.Done(err); return true })
		}()
	}
	scene.SetStore(localStore{})
	go watch(scene, feed, do)
	select {} // park forever so the callbacks live
}

// renders counts the full redraws of the canvas.
var renders atomic.Int64

// conn is the live connection to the server, nil while there is none.
var conn atomic.Pointer[wire.Client]

// localStore keeps the viewer's layout in the page's localStorage. Every access
// is guarded: a private window, blocked site data or a full quota make the
// browser throw, and the layout is then simply not kept.
type localStore struct{}

func (localStore) Get(key string) (v string, ok bool) {
	defer func() {
		if recover() != nil {
			v, ok = "", false
		}
	}()
	r := js.Global().Get("localStorage").Call("getItem", key)
	if r.IsNull() || r.IsUndefined() {
		return "", false
	}
	return r.String(), true
}

func (localStore) Set(key, value string) {
	defer func() { _ = recover() }()
	js.Global().Get("localStorage").Call("setItem", key, value)
}

// watch keeps a gRPC Watch stream open to the page's own origin, and shows
// each snapshot; when the stream ends it says so and retries.
func watch(scene *ui.Scene, feed *ui.Feed, do func(func() bool)) {
	loc := js.Global().Get("location")
	scheme := "ws://"
	if loc.Get("protocol").String() == "https:" {
		scheme = "wss://"
	}
	url := scheme + loc.Get("host").String() + "/grpc"
	status := func(key, detail string) { do(func() bool { scene.VM().SetConn(key, detail); return false }) }
	for {
		c, err := wire.Dial(url)
		if err != nil {
			status("dial", err.Error())
			time.Sleep(3 * time.Second)
			continue
		}
		status("connected", "")
		conn.Store(c)
		err = c.Watch(context.Background(), func(s model.Snapshot) {
			do(func() bool { feed.Offer(s); return false })
		})
		conn.Store(nil)
		_ = c.Close()
		if err != nil {
			status("lost", err.Error())
		} else {
			status("ended", "")
		}
		time.Sleep(2 * time.Second)
	}
}
