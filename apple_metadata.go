package aiff

// AppleMetadata is a list of custom fields sometimes set by Apple specific
// progams such as Logic.
type AppleMetadata struct {
	// Beats is the number of beats in the sample
	Beats uint32
	// Note is the root key of the sample (48 = C)
	Note uint16
	// Scale is the musical scale; 0 = neither, 1 = minor, 2 = major, 4 = both
	Scale uint16
	// Numerator of the time signature
	Numerator uint16
	// Denominator of the time signature
	Denominator uint16
	// IsLooping indicates if the sample is a loop or not
	IsLooping bool
	// Tags are tags related to the content of the file
	Tags []string
}
