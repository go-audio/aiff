package aiff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
)

// parseChunk processes a chunk and stores the valuable information
// on the decoder if supported.
// Note that the audio chunk isn't processed using this approach.
func (d *Decoder) parseChunk(chunk *Chunk) error {
	if chunk == nil {
		return nil
	}

	switch chunk.ID {
	// common chunk parsing
	case COMMID:
		if d.commSize > 0 {
			chunk.Done()
		}
		if err := d.parseCommChunk(uint32(chunk.Size)); err != nil {
			return err
		}
		// if we found the sound data before the COMM,
		// we need to rewind the reader so we can properly
		// set the clip reader.
		if d.rewindBytes > 0 {
			d.r.Seek(-d.rewindBytes, 1)
			d.rewindBytes = 0
		}
	// audio content, should be read a different way
	case SSNDID:
		chunk.Done()
	// Comments Chunk
	case COMTID:
		if err := d.parseCommentsChunk(chunk); err != nil {
			fmt.Println("failed to read comments", err)
		}
	// Apple/Logic specific chunk
	case bascID:
		if err := d.parseBascChunk(chunk); err != nil {
			fmt.Println("failed to read BASC chunk", err)
		}
	// Apple specific: packed struct AudioChannelLayout of CoreAudio
	case chanID:
		// See https://github.com/nu774/qaac/blob/ce73aac9bfba459c525eec5350da6346ebf547cf/chanmap.cpp
		// for format information
		chunk.Done()
	// Apple specific transient data
	case trnsID:
		// TODO extract and store the transients
		/*
			var v1 uint16
			var sensitivity uint16 // 0 to 100 %
			var transientDivisions uint16 // 1 = whole note
			var v3 uint16
			var v5 uint16
			var v4 uint16
			var nbSlice uint16
			int loopSize = v4 * d.AppleInfo.Beats * 2
			for s := 0; s < nbSlice; s++{
				slicepos := 24 * s + 0x4c;
				var sv1 uint16
				var sv2 uint16
				var sampleBegin uint32
			}
		*/
		chunk.Done()
	// Apple specific categorization
	case cateID:
		if err := d.parseCateChunk(chunk); err != nil {
			fmt.Println("failed to read CATE chunk", err)
		}
		chunk.Done()
	default:
		if Debug {
			fmt.Printf("skipping unknown chunk %#v\n", chunk.ID[:])
		}
		// if we read SSN but didn't read the COMM, we need to track location
		if d.SampleRate == 0 {
			d.rewindBytes += int64(chunk.Size)
		}
		chunk.Done()
	}
	return nil
}

// parseCommentsChunk processes the comments chunk and adds comments as strings
// to the decoder and drains the chunk.
func (d *Decoder) parseCommentsChunk(chunk *Chunk) error {
	if chunk.ID != COMTID {
		return fmt.Errorf("unexpected comments chunk ID: %q", chunk.ID)
	}

	br := bytes.NewBuffer(make([]byte, 0, chunk.Size))
	var n int64
	n, d.err = io.CopyN(br, d.r, int64(chunk.Size))
	if d.err != nil {
		return d.err
	}
	if n < int64(chunk.Size) {
		br.Truncate(int(n))
	}

	var nbrComments uint16
	binary.Read(br, binary.BigEndian, &nbrComments)
	for i := 0; i < int(nbrComments); i++ {
		// TODO extract marker id and timestamp
		io.CopyN(ioutil.Discard, br, 8) // equivalent of br.Seek(8, io.SeekCurrent) but bytes buffer doesn't implement seek
		b, _ := br.ReadByte()
		textB := make([]byte, int(b))
		br.Read(textB)
		d.Comments = append(d.Comments, string(bytes.TrimRight(textB, "\x00")))
	}

	return nil
}

// parseBascChunk processes the Apple specific BASC chunk
func (d *Decoder) parseBascChunk(chunk *Chunk) error {
	if chunk.ID != bascID {
		return fmt.Errorf("unexpected BASC chunk ID: %q", chunk.ID)
	}
	d.HasAppleInfo = true

	var version uint32
	binary.Read(chunk.R, binary.BigEndian, &version)
	binary.Read(chunk.R, binary.BigEndian, &d.AppleInfo.Beats)
	binary.Read(chunk.R, binary.BigEndian, &d.AppleInfo.Note)
	binary.Read(chunk.R, binary.BigEndian, &d.AppleInfo.Scale)
	binary.Read(chunk.R, binary.BigEndian, &d.AppleInfo.Numerator)
	binary.Read(chunk.R, binary.BigEndian, &d.AppleInfo.Denominator)
	chunk.ReadByte()
	var loopFlag uint16
	binary.Read(chunk.R, binary.BigEndian, &loopFlag)
	// 1  = loop; 2 = one shot
	if loopFlag == 1 {
		d.AppleInfo.IsLooping = true
	}
	chunk.Done()
	return nil
}

func (d *Decoder) parseCateChunk(chunk *Chunk) error {
	if chunk.ID != cateID {
		return fmt.Errorf("unexpected CATE chunk ID: %q", chunk.ID)
	}
	var err error
	d.HasAppleInfo = true

	// skip 4
	tmp := make([]byte, 4)
	if _, err = chunk.R.Read(tmp); err != nil {
		return err
	}

	tmp = make([]byte, 50)
	// 4 main categories: instrument, instrument category, style, substyle
	for i := 0; i < 4; i++ {
		if _, err = chunk.R.Read(tmp); err != nil {
			return err
		}
		if tmp[0] > 0 {
			d.AppleInfo.Tags = append(d.AppleInfo.Tags, nullTermStr(tmp))
		}
	}

	// skip 16
	tmp = make([]byte, 16)
	if _, err = chunk.R.Read(tmp); err != nil {
		return err
	}

	var numDescriptors int16
	binary.Read(chunk.R, binary.BigEndian, &numDescriptors)
	tmp = make([]byte, 50)
	for i := 0; i < int(numDescriptors); i++ {
		if _, err = chunk.R.Read(tmp); err != nil {
			return err
		}
		if tmp[0] > 0 {
			d.AppleInfo.Tags = append(d.AppleInfo.Tags, nullTermStr(tmp))
		}
	}

	chunk.Done()
	return nil
}
