package ui

import (
	"strings"
	"testing"

	"keel/components/icon"
)

func TestComboboxMarkup(t *testing.T) {
	out := renderString(t, Combobox(ComboboxProps{
		ID:          "host-jump",
		Name:        "jumpHost",
		Placeholder: "Search hosts",
		Value:       "h2",
		None:        "None",
		DescribedBy: "host-jump-help",
		Options: []ComboOption{
			{Value: "h1", Label: "bastion <a>", Desc: "ops@10.0.0.5", DescMono: true, Icon: icon.Server()},
			{Value: "h2", Label: "db"},
			{Value: "h3", Label: "old", Disabled: true},
		},
	}))
	for _, want := range []string{
		`class="k-combobox" data-combobox`,
		`role="combobox"`,
		`aria-autocomplete="list"`,
		`aria-expanded="false"`,
		`aria-controls="host-jump-list"`,
		`aria-describedby="host-jump-help"`,
		`autocapitalize="off"`,
		`autocorrect="off"`,
		`spellcheck="false"`,
		`autocomplete="off"`,
		`value="db"`,
		`placeholder="Search hosts"`,
		`type="hidden" name="jumpHost" value="h2" data-combobox-value`,
		`popover="manual" hidden`,
		`role="listbox" id="host-jump-list"`,
		`id="host-jump-list-o0" data-value="" aria-selected="false"`,
		`id="host-jump-list-o2" data-value="h2" aria-selected="true"`,
		`data-value="h3" aria-selected="false" aria-disabled="true"`,
		`bastion &lt;a&gt;`,
		`class="k-combobox-desc k-combobox-desc--mono">ops@10.0.0.5`,
		`class="k-i k-combobox-icon"`,
		`class="k-menu k-combobox-pop"`,
		`class="k-menu-item k-combobox-option"`,
		`<p class="k-combobox-empty" hidden>No matches</p>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("combobox lacks %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "<select") || strings.Contains(out, "<a>") {
		t.Error("combobox must not render a select or unescaped text")
	}
	// The text input has no name: only the hidden value posts.
	if strings.Count(out, `name=`) != 1 {
		t.Errorf("want one named input, got %d", strings.Count(out, `name=`))
	}
}

func TestComboboxEmptyAndUnknownValue(t *testing.T) {
	out := renderString(t, Combobox(ComboboxProps{ID: "tunnel-host", Name: "hostId", Value: "gone", Placeholder: "Search hosts", EmptyLabel: "Add an SSH host first", Required: true}))
	for _, want := range []string{
		`placeholder="Add an SSH host first"`,
		`data-placeholder="Search hosts"`,
		`<p class="k-combobox-empty">Add an SSH host first</p>`,
		`name="hostId" value="" data-combobox-value`,
		` required`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("empty combobox lacks %q\n%s", want, out)
		}
	}
	none := renderString(t, Combobox(ComboboxProps{ID: "x", None: "None", Options: []ComboOption{{Value: "", Label: "No group"}}}))
	if strings.Contains(none, ">None<") || !strings.Contains(none, ">No group<") {
		t.Error("a caller's own empty option replaces None")
	}
}

func TestSegControlPostsItsValue(t *testing.T) {
	out := renderString(t, segField("ident-kind", "Kind", "kind", "key", []SegOption{{"password", "Password"}, {"key", "Private key"}}))
	for _, want := range []string{
		`class="k-seg k-seg--field" id="ident-kind" role="group" data-seg aria-labelledby="ident-kind-label"`,
		`id="ident-kind-label"`,
		`data-seg-value="password" aria-pressed="false"`,
		`data-seg-value="key" aria-pressed="true"`,
		`type="hidden" name="kind" value="key" data-seg-input`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("seg lacks %q\n%s", want, out)
		}
	}
}

// Every value picker in the page is a k-combobox or a k-seg: no select is
// left (the inventory in the combobox PR).
func TestPickersAreComboboxes(t *testing.T) {
	var b strings.Builder
	for _, c := range []string{
		renderString(t, Dialogs(PageProps{})),
		renderString(t, Pages(PageProps{})),
		renderString(t, groupExportDrawer()),
	} {
		b.WriteString(c)
	}
	out := b.String()
	if strings.Contains(out, "<select") || strings.Contains(out, "data-tui-select") {
		t.Error("a select is left")
	}
	for _, id := range []string{"host-parent-group", "host-jump", "host-identity", "group-parent", "tunnel-host", "set-font", "agent-set-model", "agent-set-history", "group-export-runbook"} {
		if !strings.Contains(out, `id="`+id+`" role="combobox"`) {
			t.Errorf("%s is not a combobox", id)
		}
	}
	for _, id := range []string{"host-auth-type", "ident-kind", "tunnel-kind", "set-theme", "set-smooth"} {
		if !strings.Contains(out, `id="`+id+`" role="group" data-seg`) {
			t.Errorf("%s is not a k-seg", id)
		}
	}
}
