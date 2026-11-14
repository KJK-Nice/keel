package native

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
)

// The window writes times in the system's regional formats (the order of day and month, 12 or 24 hours, the month
// names). MyGo reads them
// (App.Region) and writes them (i18n.FormatDate); a test sets regionFn.
var regionFn = func() i18n.Region { return mygo.App.Region() }

func clockFmt(t time.Time, skeleton string) string {
	r := regionFn()
	loc := r.Locale
	if loc == "" {
		loc = "en-US"
	}
	return i18n.FormatDate(loc, t.Local(), i18n.DateOptions{Skeleton: skeleton, HourCycle: r.HourCycle})
}

func clockHM(t time.Time) string  { return clockFmt(t, "jm") }    // 14:05, or 2:05 PM
func clockHMS(t time.Time) string { return clockFmt(t, "jms") }   // 14:05:09, or 2:05:09 PM
func clockMD(t time.Time) string  { return clockFmt(t, "MMMd") }  // Oct 12, or 12 Oct
func clockMDY(t time.Time) string { return clockFmt(t, "yMMMd") } // Oct 12, 2026, or 12 Oct 2026
