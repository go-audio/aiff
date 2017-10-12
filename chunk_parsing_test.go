package aiff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppleInfo(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		hasInfo bool
		info    AppleMetadata
		tempo   float64
	}{
		{"no apple metadata", "fixtures/kick.aif", false, AppleMetadata{}, -1},
		{"full data", "fixtures/ring.aif", true, AppleMetadata{
			Beats:       3,
			Note:        48,
			Scale:       2,
			Numerator:   4,
			Denominator: 4,
			IsLooping:   false,
			Tags:        []string{"Sound Effect", "Mech/Tech", "Single"},
		},
			90.14},
		{"descriptor", "fixtures/98_G.aif", true, AppleMetadata{
			Beats:       44,
			Note:        48,
			Scale:       2,
			Numerator:   4,
			Denominator: 4,
			IsLooping:   false,
			Tags:        []string{"Other Instrument", "Single"},
		},
			97.92},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, _ := filepath.Abs(tt.input)
			t.Log(path)
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			d := NewDecoder(f)
			if err := d.Drain(); err != nil {
				t.Fatalf("draining %s failed - %s\n", path, err)
			}
			if tt.hasInfo != d.HasAppleInfo {
				t.Fatalf("%s was expected to have Apple info set to %v but was %v", path, tt.hasInfo, d.HasAppleInfo)
			}
			if d.HasAppleInfo {
				if tt.info.Beats != d.AppleInfo.Beats {
					t.Fatalf("expected to have %d beats but got %d", tt.info.Beats, d.AppleInfo.Beats)
				}
				if tt.info.Note != d.AppleInfo.Note {
					t.Fatalf("expected to have root note set to %d but got %d", tt.info.Note, d.AppleInfo.Note)
				}
				if tt.info.Scale != d.AppleInfo.Scale {
					t.Fatalf("expected to have its scale set to %d but got %d", tt.info.Scale, d.AppleInfo.Scale)
				}
				if tt.info.Numerator != d.AppleInfo.Numerator {
					t.Fatalf("expected to have its Numerator set to %d but got %d", tt.info.Numerator, d.AppleInfo.Numerator)
				}
				if tt.info.Denominator != d.AppleInfo.Denominator {
					t.Fatalf("expected to have its denominator set to %d but got %d", tt.info.Denominator, d.AppleInfo.Denominator)
				}
				if tt.info.IsLooping != d.AppleInfo.IsLooping {
					t.Fatalf("expected to have its looping set to %t but got %t", tt.info.IsLooping, d.AppleInfo.IsLooping)
				}
				if len(tt.info.Tags) != len(d.AppleInfo.Tags) {
					t.Fatalf("expected %d tags but got %d", len(tt.info.Tags), len(d.AppleInfo.Tags))
				}
				for i, tag := range tt.info.Tags {
					if tag != d.AppleInfo.Tags[i] {
						t.Fatalf("expected tag %d to be %q but got %q", i, tag, d.AppleInfo.Tags[i])
					}
				}
			}
			if tt.tempo != d.Tempo() {
				t.Fatalf("expected a tempo of %v but got %v", tt.tempo, d.Tempo())
			}
		})
	}
}
