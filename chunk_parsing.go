package aiff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
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
	// Apple specific: packed struct AudioChannelLayout of CoreAudio
	case chanID:
		// See https://github.com/nu774/qaac/blob/ce73aac9bfba459c525eec5350da6346ebf547cf/chanmap.cpp
		// for format information
		chunk.Done()
	default:
		if Debug {
			fmt.Printf("skipping unknown chunk %q\n", string(chunk.ID[:]))
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
	commentsBody := make([]byte, chunk.Size)
	_, err := chunk.Read(commentsBody)
	if err != nil {
		return err
	}
	br := bytes.NewReader(commentsBody)
	var nbrComments uint16
	binary.Read(br, binary.BigEndian, &nbrComments)
	for i := 0; i < int(nbrComments); i++ {
		// TODO extract marker id and timestamp
		br.Seek(8, io.SeekCurrent)
		b, _ := br.ReadByte()
		textB := make([]byte, int(b))
		br.Read(textB)
		d.Comments = append(d.Comments, string(bytes.TrimRight(textB, "\x00")))
	}
	chunk.Done()
	return nil
}
