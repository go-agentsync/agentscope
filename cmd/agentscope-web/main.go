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
	"sync"
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
	)
	render := func() {
		scene.Draw(local)
		js.CopyBytesToJS(dst, local)
		ctx2d.Call("putImageData", imageData, 0, 0)
	}
	// fit makes the canvas the viewport, in device pixels, and lays the scene out
	// again. The caller holds the lock.
	fit := func() {
		dpr := js.Global().Get("devicePixelRatio").Float()
		if dpr <= 0 {
			dpr = 1
		}
		root := doc.Get("documentElement")
		w := int(float64(root.Get("clientWidth").Int())*dpr + 0.5)
		h := int(float64(root.Get("clientHeight").Int())*dpr + 0.5)
		if w < 1 || h < 1 {
			return
		}
		canvas.Set("width", w)
		canvas.Set("height", h)
		local = make([]byte, 4*w*h)
		imageData = ctx2d.Call("createImageData", w, h)
		dst = imageData.Get("data")
		scene.Resize(w, h, dpr)
		render()
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
		if fn() || scene.Dirty() {
			render()
		}
	}

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
	on("mousemove", scene.HandleMove)
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
	follow := func() { do(func() bool { scene.VM().Dark.Set(dark.Get("matches").Bool()); return true }) }
	dark.Call("addEventListener", "change", js.FuncOf(func(js.Value, []js.Value) any { follow(); return nil }))
	follow()
	go watch(scene, do)
	select {} // park forever so the callbacks live
}

// watch keeps a gRPC Watch stream open to the page's own origin, and shows
// each snapshot; when the stream ends it says so and retries.
func watch(scene *ui.Scene, do func(func() bool)) {
	loc := js.Global().Get("location")
	scheme := "ws://"
	if loc.Get("protocol").String() == "https:" {
		scheme = "wss://"
	}
	url := scheme + loc.Get("host").String() + "/grpc"
	status := func(s string) { do(func() bool { scene.VM().Conn.Set(s); return false }) }
	for {
		c, err := wire.Dial(url)
		if err != nil {
			status("cannot dial: " + err.Error())
			time.Sleep(3 * time.Second)
			continue
		}
		status("connected")
		err = c.Watch(context.Background(), func(s model.Snapshot) {
			do(func() bool { scene.VM().Apply(s); return false })
		})
		_ = c.Close()
		if err != nil {
			status("lost: " + err.Error())
		} else {
			status("stream ended")
		}
		time.Sleep(2 * time.Second)
	}
}
