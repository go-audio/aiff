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

// AppleScaleToString converts the scale information into a string representation.
func AppleScaleToString(scale uint16) string {
	switch scale {
	case 1:
		return "minor"
	case 2:
		return "major"
	case 4:
		return "minor + major"
	default:
		return ""
	}
}

// AppleNoteToPitch returns the pitch for the stored note.
func AppleNoteToPitch(note uint16) string {
	switch note {
	case 48:
		return "C"
	case 49:
		return "C#"
	case 50:
		return "D"
	case 51:
		return "D#"
	case 52:
		return "E"
	case 53:
		return "F"
	case 54:
		return "F#"
	case 55:
		return "G"
	case 56:
		return "G#"
	case 57:
		return "A"
	case 58:
		return "A#"
	case 59:
		return "B"
	default:
		return ""
	}
}
