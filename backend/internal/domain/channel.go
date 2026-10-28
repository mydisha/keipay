package domain

// Channel is a normalized payment method code. Providers map it to their own
// codes; the set a provider supports is in its capabilities.
type Channel string

// Channels, in display order.
const (
	ChannelQRIS             Channel = "QRIS"
	ChannelVABCA            Channel = "VA_BCA"
	ChannelVABNI            Channel = "VA_BNI"
	ChannelVABRI            Channel = "VA_BRI"
	ChannelVAMandiri        Channel = "VA_MANDIRI"
	ChannelVAPermata        Channel = "VA_PERMATA"
	ChannelVABSI            Channel = "VA_BSI"
	ChannelEwalletOVO       Channel = "EWALLET_OVO"
	ChannelEwalletDANA      Channel = "EWALLET_DANA"
	ChannelEwalletShopeePay Channel = "EWALLET_SHOPEEPAY"
	ChannelEwalletLinkAja   Channel = "EWALLET_LINKAJA"
	ChannelCard             Channel = "CARD"
	ChannelRetailAlfamart   Channel = "RETAIL_ALFAMART"
	ChannelRetailIndomaret  Channel = "RETAIL_INDOMARET"
)

var channelLabels = map[Channel]string{
	ChannelQRIS:             "QRIS",
	ChannelVABCA:            "BCA Virtual Account",
	ChannelVABNI:            "BNI Virtual Account",
	ChannelVABRI:            "BRI Virtual Account",
	ChannelVAMandiri:        "Mandiri Virtual Account",
	ChannelVAPermata:        "Permata Virtual Account",
	ChannelVABSI:            "BSI Virtual Account",
	ChannelEwalletOVO:       "OVO",
	ChannelEwalletDANA:      "DANA",
	ChannelEwalletShopeePay: "ShopeePay",
	ChannelEwalletLinkAja:   "LinkAja",
	ChannelCard:             "Credit or debit card",
	ChannelRetailAlfamart:   "Alfamart",
	ChannelRetailIndomaret:  "Indomaret",
}

// AllChannels returns every known channel in display order.
func AllChannels() []Channel {
	return []Channel{
		ChannelQRIS, ChannelVABCA, ChannelVABNI, ChannelVABRI, ChannelVAMandiri, ChannelVAPermata, ChannelVABSI,
		ChannelEwalletOVO, ChannelEwalletDANA, ChannelEwalletShopeePay, ChannelEwalletLinkAja,
		ChannelCard, ChannelRetailAlfamart, ChannelRetailIndomaret,
	}
}

// Valid reports whether c is a known channel.
func (c Channel) Valid() bool {
	_, ok := channelLabels[c]
	return ok
}

// Label is the display name of c, or the code itself when c is unknown.
func (c Channel) Label() string {
	if l, ok := channelLabels[c]; ok {
		return l
	}
	return string(c)
}
