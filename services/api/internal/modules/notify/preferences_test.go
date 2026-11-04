package notify

import "testing"

func TestPreferenceStatesReportStoredOptOut(t *testing.T) {
	got := preferenceStates([]Preference{{Category: CategoryMarketing, Channel: ChannelPush, Allowed: false}})
	if len(got) != len(ChannelsFor(CategoryMarketing)) {
		t.Fatalf("want one row per marketing channel, got %+v", got)
	}
	for _, p := range got {
		want := p.Channel != ChannelPush
		if p.Category != CategoryMarketing || !p.Optional || p.Allowed != want {
			t.Errorf("%s: %+v", p.Channel, p)
		}
	}
}
