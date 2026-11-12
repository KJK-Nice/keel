package native

import (
	"testing"
	"time"

	"github.com/egoist/mygo/i18n"
)

func init() { // the tests read English with a 24-hour clock unless one sets another region
	regionFn = func() i18n.Region { return i18n.Region{Locale: "en-US", HourCycle: 24} }
}

func withRegion(t *testing.T, r i18n.Region) {
	old := regionFn
	regionFn = func() i18n.Region { return r }
	t.Cleanup(func() { regionFn = old })
}

func TestClockFollowsRegion(t *testing.T) {
	at := time.Date(2026, time.October, 12, 14, 5, 9, 0, time.Local)
	for _, c := range []struct {
		r                i18n.Region
		hm, hms, md, mdy string
	}{
		{i18n.Region{Locale: "en-US", HourCycle: 24}, "14:05", "14:05:09", "Oct 12", "Oct 12, 2026"},
		{i18n.Region{Locale: "en-US", HourCycle: 12}, "2:05 PM", "2:05:09 PM", "Oct 12", "Oct 12, 2026"},
		{i18n.Region{Locale: "en-GB", HourCycle: 24}, "14:05", "14:05:09", "12 Oct", "12 Oct 2026"},
	} {
		withRegion(t, c.r)
		for name, got := range map[string][2]string{
			"hm": {clockHM(at), c.hm}, "hms": {clockHMS(at), c.hms}, "md": {clockMD(at), c.md}, "mdy": {clockMDY(at), c.mdy},
		} {
			if got[0] != got[1] {
				t.Errorf("%+v %s = %q, want %q", c.r, name, got[0], got[1])
			}
		}
	}
	withRegion(t, i18n.Region{})
	if got := clockHM(at); got == "" {
		t.Error("the zero region writes nothing")
	}
}
