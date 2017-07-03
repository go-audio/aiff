package aiff

type AppleMetadata struct {
	Beats       uint32
	Note        uint16
	Scale       uint16
	Numerator   uint16
	Denominator uint16
	Looping     bool
}
