package browser

import (
	"context"
	"errors"

	"aacpanel/internal/browserlink"
)

func (b *chrome) input(ctx context.Context, i browserlink.Input) error {
	if err := i.Check(); err != nil {
		return err
	}
	sid := b.session()
	if sid == "" && i.Kind != "new-tab" && i.Kind != "select-tab" {
		return errors.New("the browser has no selected tab")
	}
	b.mu.Lock()
	size := b.size
	generation := b.generation
	b.mu.Unlock()
	switch i.Kind {
	case "touch", "mouse", "wheel":
		if i.Generation != generation {
			return errors.New("the page changed before this input arrived")
		}
	}
	call := func(method string, p interface{}) error { return b.call(ctx, sid, method, p, nil) }
	switch i.Kind {
	case "text":
		return call("Input.insertText", map[string]string{"text": i.Text})
	case "key":
		for _, phase := range []string{"keyDown", "keyUp"} {
			if err := call("Input.dispatchKeyEvent", map[string]interface{}{
				"type":                  phase,
				"key":                   i.Key,
				"windowsVirtualKeyCode": browserlink.Keys[i.Key],
				"modifiers":             i.Modifiers,
			}); err != nil {
				return err
			}
		}
		return nil
	case "touch", "mouse", "wheel":
		if i.X < 0 || i.Y < 0 || i.X > float64(size.Width) || i.Y > float64(size.Height) {
			return errors.New("the input is outside the browser viewport")
		}
		if i.Kind == "touch" {
			points := []map[string]float64{}
			phase := "touchEnd"
			if i.Phase == "start" || i.Phase == "move" {
				phase = map[string]string{"start": "touchStart", "move": "touchMove"}[i.Phase]
				points = append(points, map[string]float64{"x": i.X, "y": i.Y})
			}
			if i.Phase == "cancel" {
				phase = "touchCancel"
			}
			return call("Input.dispatchTouchEvent", map[string]interface{}{"type": phase, "touchPoints": points})
		}
		if i.Kind == "wheel" {
			return call("Input.dispatchMouseEvent", map[string]interface{}{"type": "mouseWheel", "x": i.X, "y": i.Y, "deltaX": i.DX, "deltaY": i.DY})
		}
		phase := map[string]string{"down": "mousePressed", "up": "mouseReleased", "move": "mouseMoved"}[i.Phase]
		return call("Input.dispatchMouseEvent", map[string]interface{}{
			"type":       phase,
			"x":          i.X,
			"y":          i.Y,
			"button":     i.Button,
			"clickCount": 1,
			"modifiers":  i.Modifiers,
		})
	case "navigate":
		return call("Page.navigate", map[string]string{"url": i.URL})
	case "reload":
		return call("Page.reload", struct{}{})
	case "back", "forward":
		var h struct {
			CurrentIndex int `json:"currentIndex"`
			Entries      []struct {
				ID  int    `json:"id"`
				URL string `json:"url"`
			} `json:"entries"`
		}
		if err := b.call(ctx, sid, "Page.getNavigationHistory", struct{}{}, &h); err != nil {
			return err
		}
		n := h.CurrentIndex - 1
		if i.Kind == "forward" {
			n = h.CurrentIndex + 1
		}
		if n < 0 || n >= len(h.Entries) {
			return nil
		}
		return call("Page.navigateToHistoryEntry", map[string]int{"entryId": h.Entries[n].ID})
	case "new-tab":
		var result struct {
			TargetID string `json:"targetId"`
		}
		if err := b.call(ctx, "", "Target.createTarget", map[string]string{"url": "about:blank"}, &result); err != nil {
			return err
		}
		if err := b.ready(ctx, result.TargetID); err != nil {
			return err
		}
		i.Tab = result.TargetID
	case "close-tab":
		b.mu.Lock()
		_, ok := b.tabs[i.Tab]
		current := b.current
		b.mu.Unlock()
		if !ok {
			return errors.New("the browser tab is gone")
		}
		if err := b.call(ctx, "", "Target.closeTarget", map[string]string{"targetId": i.Tab}, nil); err != nil {
			return err
		}
		b.mu.Lock()
		delete(b.tabs, i.Tab)
		var next string
		for id := range b.tabs {
			next = id
			break
		}
		b.mu.Unlock()
		if current != i.Tab {
			return nil
		}
		if next == "" {
			return b.input(ctx, browserlink.Input{Kind: "new-tab"})
		}
		i.Tab = next
	}
	if i.Kind == "select-tab" || i.Kind == "new-tab" || i.Kind == "close-tab" {
		b.mu.Lock()
		_, ok := b.tabs[i.Tab]
		b.mu.Unlock()
		if !ok {
			return errors.New("the browser tab is gone")
		}
		return b.selectTab(ctx, i.Tab, size)
	}
	return nil
}
