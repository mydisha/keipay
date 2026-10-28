package domain

import "testing"

func TestChannels(t *testing.T) {
	all := AllChannels()
	if len(all) != 14 || all[0] != ChannelQRIS || all[13] != ChannelRetailIndomaret {
		t.Fatalf("channels = %v", all)
	}
	seen := map[Channel]bool{}
	for _, c := range all {
		if !c.Valid() || c.Label() == "" || c.Label() == string(c) && c != ChannelQRIS || seen[c] {
			t.Errorf("channel %s: valid=%v label=%q", c, c.Valid(), c.Label())
		}
		seen[c] = true
	}
	if ChannelVABCA.Label() != "BCA Virtual Account" || ChannelCard.Label() != "Credit or debit card" {
		t.Fatal("labels")
	}
	if Channel("VA_XYZ").Valid() || Channel("VA_XYZ").Label() != "VA_XYZ" {
		t.Fatal("unknown channel")
	}
}
