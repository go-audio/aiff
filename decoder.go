package aiff

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"time"

	"bytes"

	"github.com/go-audio/audio"
)

// Decoder is the wrapper structure for the AIFF container
type Decoder struct {
	r io.ReadSeeker

	// ID is always 'FORM'. This indicates that this is a FORM chunk
	ID [4]byte
	// Size contains the size of data portion of the 'FORM' chunk.
	// Note that the data portion has been
	// broken into two parts, formType and chunks
	Size uint32
	// Form describes what's in the 'FORM' chunk. For Audio IFF files,
	// formType (aka Format) is always 'AIFF'.
	// This indicates that the chunks within the FORM pertain to sampled sound.
	Form [4]byte

	// Data coming from the COMM chunk
	commSize        uint32
	NumChans        uint16
	NumSampleFrames uint32
	BitDepth        uint16
	SampleRate      int
	//
	PCMSize  uint32
	PCMChunk *Chunk
	//
	Comments []string

	// AIFC data
	Encoding     [4]byte
	EncodingName string

	// Apple specific
	HasAppleInfo bool
	AppleInfo    AppleMetadata

	err             error
	pcmDataAccessed bool

	byteOrder binary.ByteOrder

	// read the file information to setup the audio clip
	// find the beginning of the SSND chunk and set the clip reader to it.
	rewindBytes int64
}

// NewDecoder creates a new reader reading the given reader and pushing audio data to the given channel.
// It is the caller's responsibility to call Close on the reader when done.
func NewDecoder(r io.ReadSeeker) *Decoder {
	return &Decoder{r: r, byteOrder: binary.BigEndian}
}

// SampleBitDepth returns the bit depth encoding of each sample.
func (d *Decoder) SampleBitDepth() int32 {
	if d == nil {
		return 0
	}
	return int32(d.BitDepth)
}

// PCMLen returns the total number of bytes in the PCM data chunk
func (d *Decoder) PCMLen() int64 {
	if d == nil {
		return 0
	}
	return int64(d.PCMSize)
}

// Err returns the first non-EOF error that was encountered by the Decoder.
func (d *Decoder) Err() error {
	if d.err == io.EOF {
		return nil
	}
	return d.err
}

// EOF returns positively if the underlying reader reached the end of file.
func (d *Decoder) EOF() bool {
	if d == nil || d.err == io.EOF {
		return true
	}
	return false
}

// WasPCMAccessed returns positively if the PCM data was previously accessed.
func (d *Decoder) WasPCMAccessed() bool {
	if d == nil {
		return false
	}
	return d.pcmDataAccessed
}

// Format returns the audio format of the decoded content.
func (d *Decoder) Format() *audio.Format {
	if d == nil {
		return nil
	}
	return &audio.Format{
		NumChannels: int(d.NumChans),
		SampleRate:  int(d.SampleRate),
	}
}

// NextChunk returns the next available chunk
func (d *Decoder) NextChunk() (*Chunk, error) {
	// we need to read the info so we have access to the encoding.
	if d.ReadInfo(); d.err != nil {
		d.err = fmt.Errorf("failed to read info - %v", d.err)
		return nil, d.err
	}

	var (
		id   [4]byte
		size uint32
	)

	id, size, d.err = d.iDnSize()
	if size%2 != 0 {
		// realign, the encoder lied about the size of the chunk header :(
		size++
	}
	if d.err != nil {
		if d.err == io.EOF || d.err == io.ErrUnexpectedEOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("error reading chunk header - %v", d.err)
	}

	c := &Chunk{
		ID:   id,
		Size: int(size),
		R:    io.LimitReader(d.r, int64(size)),
	}

	return c, d.err
}

// IsValidFile verifies that the file is valid/readable.
func (d *Decoder) IsValidFile() bool {
	d.ReadInfo()
	if d.err != nil {
		return false
	}
	if d.NumChans < 1 {
		return false
	}
	if d.BitDepth < 8 {
		return false
	}
	if d, err := d.Duration(); err != nil || d <= 0 {
		return false
	}
	switch d.Encoding {
	case encSowt, encNone, encNotSet:
	default:
		return false
	}

	return true
}

// Duration returns the time duration for the current AIFF container
func (d *Decoder) Duration() (time.Duration, error) {
	if d == nil {
		return 0, errors.New("can't calculate the duration of a nil pointer")
	}
	d.ReadInfo()
	if err := d.Err(); err != nil {
		return 0, err
	}
	duration := time.Duration(float64(d.NumSampleFrames) / float64(d.SampleRate) * float64(time.Second))
	return duration, nil
}

// Tempo returns a tempo when available, otherwise -1
func (d *Decoder) Tempo() float64 {
	if d == nil || !d.HasAppleInfo || d.AppleInfo.Beats < 1 {
		return -1
	}
	duration, err := d.Duration()
	if err != nil {
		return -1
	}
	return round(float64(d.AppleInfo.Beats)/(duration.Seconds()/60.0), 2)
}

// Drain parses the remaining chunks
func (d *Decoder) Drain() error {
	var chunk *Chunk
	for d.err == nil {
		chunk, d.err = d.NextChunk()
		if d.err != nil {
			if d.err == io.EOF {
				return nil
			}
			return d.err
		}
		if err := d.parseChunk(chunk); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
	return d.err
}

// FwdToPCM forwards the underlying reader until the start of the PCM chunk.
// If the PCM chunk was already read, no data will be found (you need to rewind).
func (d *Decoder) FwdToPCM() error {
	if d.err = d.readHeaders(); d.err != nil {
		d.err = fmt.Errorf("failed to read header - %v", d.err)
		return nil
	}

	var chunk *Chunk
	for d.err == nil {
		chunk, d.err = d.NextChunk()
		if d.err != nil {
			d.err = fmt.Errorf("failed to read next chunk: %v", d.err)
			return d.err
		}

		if chunk.ID == SSNDID {
			//            SSND chunk: Must be defined
			//   0      4 bytes  "SSND"
			//   4      4 bytes  <Chunk size(x)>
			//   8      4 bytes  <Offset(n)>
			//  12      4 bytes  <block size>
			//  16     (n)bytes  Comment
			//  16+(n) (s)bytes  <Sample data>

			var offset uint32
			if d.err = chunk.ReadBE(&offset); d.err != nil {
				d.err = fmt.Errorf("PCM offset failed to parse - %s", d.err)
				return d.err
			}

			if d.err = chunk.ReadBE(&d.PCMSize); d.err != nil {
				d.err = fmt.Errorf("PCMSize failed to parse - %s", d.err)
				return d.err
			}
			if offset > 0 {
				d.PCMSize -= offset
				// skip pcm comment
				buf := make([]byte, offset)
				if err := chunk.ReadBE(&buf); err != nil {
					d.err = fmt.Errorf("failed to read the offsetted buffer - %v", err)
					return d.err
				}
			}
			d.PCMChunk = chunk
			d.pcmDataAccessed = true
			if d.err != nil {
				d.err = fmt.Errorf("failed to read the SSND chunk - %v", d.err)
			}
			return d.err
		}

		if err := d.parseChunk(chunk); err != nil {
			return fmt.Errorf("failed to parse the chunk - %v", err)
		}
	}
	return d.err
}

// Reset resets the decoder (and rewind the underlying reader)
func (d *Decoder) Reset() {
	d.ID = [4]byte{}
	d.Size = 0
	d.Form = [4]byte{}
	d.commSize = 0
	d.NumChans = 0
	d.NumSampleFrames = 0
	d.BitDepth = 0
	d.SampleRate = 0
	d.Encoding = [4]byte{}
	d.EncodingName = ""
	d.err = nil
	d.pcmDataAccessed = false
	d.r.Seek(0, 0)
}

// Seek provides access to the cursor position in the PCM data
func (d *Decoder) Seek(offset int64, whence int) (int64, error) {
	return d.r.Seek(offset, whence)
}

// FullPCMBuffer is an inneficient way to access all the PCM data contained in the
// audio container. The entire PCM data is held in memory.
// Consider using Buffer() instead.
func (d *Decoder) FullPCMBuffer() (*audio.IntBuffer, error) {
	if !d.WasPCMAccessed() {
		err := d.FwdToPCM()
		if err != nil {
			return nil, fmt.Errorf("failed to forward to PCM: %v", err)
		}
	}
	format := &audio.Format{
		NumChannels: int(d.NumChans),
		SampleRate:  int(d.SampleRate),
	}

	chunkSize := 4096
	buf := &audio.IntBuffer{Data: make([]int, chunkSize),
		Format:         format,
		SourceBitDepth: int(d.BitDepth),
	}
	decodeF, err := sampleDecodeFunc(buf.SourceBitDepth, d.byteOrder)
	if err != nil {
		return nil, fmt.Errorf("could not get sample decode func %v", err)
	}

	sampleBuf := make([]byte, 4, 4)
	n := 0
	i := 0
	chunkSize = 2048 * bytesPerSample(buf.SourceBitDepth)
	sizeToRead := chunkSize
	var innerErr error
	for err == nil {
		// to avoid doing too many small reads (bad performance)
		// we are loading part of the chunk in memory and reading from there
		sizeToRead = chunkSize
		if adjust := sizeToRead % bytesPerSample(buf.SourceBitDepth); adjust != 0 {
			fmt.Fprintf(os.Stderr, "should be 0: %d %d %d\n", adjust, sizeToRead, buf.SourceBitDepth)
		}

		if leftOverSize := d.PCMChunk.Size - d.PCMChunk.Pos; leftOverSize < chunkSize {
			sizeToRead = leftOverSize
		}
		if sizeToRead < 1 {
			break
		}
		optBuf := make([]byte, sizeToRead)
		n, err = d.PCMChunk.Read(optBuf)
		if err != nil {
			// fmt.Println("-->", sizeToRead, err)
			break
		}
		if n != sizeToRead {
			optBuf = optBuf[:n]
		}

		bufReader := bytes.NewReader(optBuf)
		for innerErr == nil {
			buf.Data[i], innerErr = decodeF(bufReader, sampleBuf)
			if innerErr != nil {
				if innerErr == io.EOF {
					innerErr = nil
				}
				break
			}
			i++
			// grow the underlying slice if needed
			if i >= len(buf.Data) {
				buf.Data = append(buf.Data, make([]int, chunkSize)...)
			}
		}
	}
	buf.Data = buf.Data[:i]

	if err == io.EOF {
		err = nil
	}

	return buf, err
}

// PCMBuffer populates the passed PCM buffer and returns the number of samples
// read and a potential error. If the reader reaches EOF, an io.EOF error will be returned.
func (d *Decoder) PCMBuffer(buf *audio.IntBuffer) (n int, err error) {
	if buf == nil {
		return 0, nil
	}

	if !d.WasPCMAccessed() {
		err = d.FwdToPCM()
		if err != nil {
			return 0, err
		}
	}

	// TODO: avoid a potentially unecessary allocation
	format := &audio.Format{
		NumChannels: int(d.NumChans),
		SampleRate:  int(d.SampleRate),
	}

	buf.SourceBitDepth = int(d.BitDepth)
	decodeF, err := sampleDecodeFunc(buf.SourceBitDepth, d.byteOrder)
	if err != nil {
		return 0, fmt.Errorf("could not get sample decode func %v", err)
	}

	bPerSample := bytesPerSample(int(d.BitDepth))
	// populate a file buffer to avoid multiple very small reads
	// we need to cap the buffer size to not be bigger than the pcm chunk.
	size := len(buf.Data) * bPerSample
	tmpBuf := make([]byte, size)
	var m int
	m, err = d.PCMChunk.R.Read(tmpBuf)
	if err != nil {
		if err == io.EOF {
			return m, nil
		}
		return m, err
	}
	if m == 0 {
		return m, nil
	}
	bufR := bytes.NewReader(tmpBuf[:m])
	sampleBuf := make([]byte, bPerSample, bPerSample)
	var misaligned bool
	if m%bPerSample > 0 {
		misaligned = true
	}

	// Note that we populate the buffer even if the
	// size of the buffer doesn't fit an even number of frames.
	for n = 0; n < len(buf.Data); n++ {
		buf.Data[n], err = decodeF(bufR, sampleBuf)
		if err != nil {
			// the last sample isn't a full sample but just padding.
			if misaligned {
				n--
			}
			break
		}
	}
	buf.Format = format
	if err == io.EOF {
		err = nil
	}

	return n, err
}

// String implements the Stringer interface.
func (d *Decoder) String() string {
	out := fmt.Sprintf("Format: %s - ", d.Form)
	if d.Form == aifcID {
		out += fmt.Sprintf("%s - ", string(d.Encoding[:]))
	}
	if d.SampleRate != 0 {
		out += fmt.Sprintf("%d channels @ %d / %d bits - ", d.NumChans, d.SampleRate, d.BitDepth)
		dur, _ := d.Duration()
		out += fmt.Sprintf("Duration: %f seconds\n", dur.Seconds())
	}
	if len(d.Comments) > 0 {
		for _, comment := range d.Comments {
			out += fmt.Sprintln(comment)
		}
	}
	if d.HasAppleInfo {
		out += fmt.Sprintln("Key note:", AppleNoteToPitch(d.AppleInfo.Note))
		out += fmt.Sprintln("Scale:", AppleScaleToString(d.AppleInfo.Scale))
		out += fmt.Sprintf("Tempo: %.2f BPM\n", d.Tempo())
		out += fmt.Sprintf("Number of beats: %d\n", d.AppleInfo.Beats)
		out += fmt.Sprintf("Time signature: %d/%d\n", d.AppleInfo.Numerator, d.AppleInfo.Denominator)
		var format string
		if d.AppleInfo.IsLooping {
			format = "loop"
		} else {
			format = "one-shot"
		}
		out += fmt.Sprintln("Sample format:", format)
		if len(d.AppleInfo.Tags) > 0 {
			out += "Tags:\n"
			for _, tag := range d.AppleInfo.Tags {
				out += fmt.Sprintln("\t" + tag)
			}
		}
	}
	return out
}

// iDnSize returns the next ID + block size
func (d *Decoder) iDnSize() ([4]byte, uint32, error) {
	var ID [4]byte
	var blockSize uint32
	if d.err = binary.Read(d.r, binary.BigEndian, &ID); d.err != nil {
		return ID, blockSize, d.err
	}
	if d.err = binary.Read(d.r, binary.BigEndian, &blockSize); d.err != nil {
		return ID, blockSize, d.err
	}
	return ID, blockSize, nil
}

// readHeaders is safe to call multiple times
// byte size of the header: 12
func (d *Decoder) readHeaders() error {
	// prevent the headers to be re-read
	if d.Size > 0 {
		return nil
	}
	var n int64
	size := 12 // 4 + 4 + 4
	src := bytes.NewBuffer(make([]byte, 0, size))
	n, d.err = io.CopyN(src, d.r, int64(size))
	if n < int64(size) {
		src.Truncate(int(n))
	}

	if d.err = binary.Read(src, binary.BigEndian, &d.ID); d.err != nil {
		return d.err
	}
	// Must start by a FORM header/ID
	if d.ID != formID {
		d.err = fmt.Errorf("%s - %#v", ErrFmtNotSupported, d.ID)
		return d.err
	}

	if d.err = binary.Read(src, binary.BigEndian, &d.Size); d.err != nil {
		return d.err
	}
	if d.err = binary.Read(src, binary.BigEndian, &d.Form); d.err != nil {
		return d.err
	}

	// Must be a AIFF or AIFC form type
	if d.Form != aiffID && d.Form != aifcID {
		d.err = fmt.Errorf("%s - %#v", ErrFmtNotSupported, d.Form)
		return d.err
	}

	return d.err
}

// ReadInfo reads the underlying reader to extract information.
// This method is safe to call multiple times.
func (d *Decoder) ReadInfo() {
	if d == nil || d.SampleRate > 0 {
		return
	}
	if d.err = d.readHeaders(); d.err != nil {
		d.err = fmt.Errorf("failed to read header - %v", d.err)
		return
	}

	var (
		id          [4]byte
		size        uint32
		rewindBytes int64
	)
	for d.err != io.EOF {
		id, size, d.err = d.iDnSize()
		if d.err != nil {
			d.err = fmt.Errorf("error reading chunk header - %v", d.err)
			break
		}
		switch id {
		case COMMID:
			d.parseCommChunk(size)
			// if we found other chunks before the COMM,
			// we need to rewind the reader so we can properly
			// read the rest later.
			if rewindBytes > 0 {
				// we need to rewind rewindBytes+size of chunk ID and size
				d.r.Seek(-(rewindBytes + int64(size) + 8), io.SeekCurrent)
				rewindBytes = 0
			}
			return
		case COMTID:
			chunk := &Chunk{
				ID:   id,
				Size: int(size),
				R:    io.LimitReader(d.r, int64(size)),
			}
			if err := d.parseCommentsChunk(chunk); err != nil {
				fmt.Fprintf(os.Stderr, "failed to read comments (ignored) - %v", err)
			}
		default:
			// we haven't read the COMM chunk yet, we need to track location to rewind
			if d.SampleRate == 0 {
				rewindBytes += int64(size) + 8 // we add 8 for the ID and size of this chunk
			}
			if d.err = d.jumpTo(int(size)); d.err != nil {
				return
			}
		}
	}
}

func (d *Decoder) parseCommChunk(size uint32) error {
	// don't re-parse the comm chunk
	if d.NumChans > 0 {
		return nil
	}
	d.commSize = size

	var n int64
	src := bytes.NewBuffer(make([]byte, 0, size))
	n, d.err = io.CopyN(src, d.r, int64(size))
	if n < int64(size) {
		src.Truncate(int(n))
	}

	if d.err = binary.Read(src, binary.BigEndian, &d.NumChans); d.err != nil {
		d.err = fmt.Errorf("num of channels failed to parse - %s", d.err)
		return d.err
	}
	if d.err = binary.Read(src, binary.BigEndian, &d.NumSampleFrames); d.err != nil {
		d.err = fmt.Errorf("num of sample frames failed to parse - %s", d.err)
		return d.err
	}
	if d.err = binary.Read(src, binary.BigEndian, &d.BitDepth); d.err != nil {
		d.err = fmt.Errorf("sample size failed to parse - %s", d.err)
		return d.err
	}
	var srBytes [10]byte
	if d.err = binary.Read(src, binary.BigEndian, &srBytes); d.err != nil {
		d.err = fmt.Errorf("sample rate failed to parse - %s", d.err)
		return d.err
	}
	d.SampleRate = audio.IEEEFloatToInt(srBytes)

	read := 18

	if d.Form == aifcID {
		if d.err = binary.Read(src, binary.BigEndian, &d.Encoding); d.err != nil {
			d.err = fmt.Errorf("AIFC encoding failed to parse - %s", d.err)
			return d.err
		}
		if d.Encoding == encSowt {
			d.byteOrder = binary.LittleEndian
		}
		// pascal style string with the description of the encoding
		var encNameSize uint8
		if d.err = binary.Read(src, binary.BigEndian, &encNameSize); d.err != nil {
			d.err = fmt.Errorf("AIFC encoding failed to parse - %s", d.err)
			return d.err
		}
		read += 5
		if encNameSize > 0 {
			desc := make([]byte, encNameSize)
			if d.err = binary.Read(src, binary.BigEndian, &desc); d.err != nil {
				d.err = fmt.Errorf("AIFC encoding failed to parse - %s", d.err)
				return d.err
			}
			d.EncodingName = string(desc[:encNameSize])
			read += int(encNameSize)
		}
	}
	if read < int(size) {
		io.CopyN(ioutil.Discard, src, int64(int(size)-read))
	}

	return d.err
}

// jumpTo advances the reader to the amount of bytes provided
func (d *Decoder) jumpTo(bytesAhead int) error {
	var err error
	if bytesAhead > 0 {
		_, err = d.r.Seek(int64(bytesAhead), io.SeekCurrent)
		// TODO: benchmark against
		// _, err = io.CopyN(ioutil.Discard, d.r, int64(bytesAhead))
	}
	return err
}

func bytesPerSample(bitDepth int) int {
	return bitDepth / 8
}

func sampleDecodeFunc(bitDepth int, byteOrder binary.ByteOrder) (func(io.Reader, []byte) (int, error), error) {
	switch bitDepth {
	case 8:
		// 8bit values are unsigned
		return func(r io.Reader, buf []byte) (int, error) {
			_, err := r.Read(buf[:1])
			return int(buf[0]), err
		}, nil
	case 16:
		return func(r io.Reader, buf []byte) (int, error) {
			_, err := r.Read(buf[:2])
			return int(int16(byteOrder.Uint16(buf[:2]))), err
		}, nil
	case 24:
		if byteOrder == binary.BigEndian {
			return func(r io.Reader, buf []byte) (int, error) {
				_, err := r.Read(buf[:3])
				if err != nil {
					return 0, err
				}
				return int(audio.Int24BETo32(buf[:3])), nil
			}, nil
		}
		return func(r io.Reader, buf []byte) (int, error) {
			_, err := r.Read(buf[:3])
			if err != nil {
				return 0, err
			}
			return int(audio.Int24LETo32(buf[:3])), nil
		}, nil
	case 32:
		return func(r io.Reader, buf []byte) (int, error) {
			_, err := r.Read(buf[:4])
			return int(int32(byteOrder.Uint32(buf[:4]))), err
		}, nil
	default:
		return nil, fmt.Errorf("%v bit depth not supported", bitDepth)
	}
}

func sampleFloat64DecodeFunc(bitDepth int, byteOrder binary.ByteOrder) (func(io.Reader) (float64, error), error) {
	switch bitDepth {
	case 8:
		// 8bit values are unsigned
		return func(r io.Reader) (float64, error) {
			var v uint8
			err := binary.Read(r, byteOrder, &v)
			return float64(v), err
		}, nil
	case 16:
		return func(r io.Reader) (float64, error) {
			var v int16
			err := binary.Read(r, byteOrder, &v)
			return float64(v), err
		}, nil
	case 24:
		return func(r io.Reader) (float64, error) {
			// TODO: check if the conversion might not be inversed depending on
			// the encoding (BE vs LE)
			var output int32
			d := make([]byte, 3)
			_, err := r.Read(d)
			if err != nil {
				return 0, err
			}
			output |= int32(d[2]) << 0
			output |= int32(d[1]) << 8
			output |= int32(d[0]) << 16
			return float64(output), nil
		}, nil
	case 32:
		return func(r io.Reader) (float64, error) {
			var v float32
			err := binary.Read(r, byteOrder, &v)
			return float64(v), err
		}, nil
	default:
		return nil, fmt.Errorf("%v bit depth not supported", bitDepth)
	}
}
