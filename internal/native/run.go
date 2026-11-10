package native

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Run opens the window and blocks until the app quits.
func Run() error {
	mygo.App.WhenReady(func() {
		mygo.NewWindow(mygo.WindowOptions{
			Title: "Keel", Width: 1280, Height: 800,
			Content: ui.View(func(c *ui.Context) { ui.Text(c, "Keel") }),
		})
	})
	return mygo.App.Run()
}
