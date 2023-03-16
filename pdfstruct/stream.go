package pdfstruct

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
)

// Decompress removes any compression and/or encoding from the stream data.
// Some encoding methods need to know the size (in bytes) of a "row" in the data
// for decoding, so that is a parameter.
func (s *Stream) Decompress(rowsize int) error {
	var filters []string
	var parms []Dict

	// What decoding method is needed?
	switch flist := s.Dict["Filter"].(type) {
	case nil:
		// Not encoded, nothing to do.
		return nil
	case Name:
		// A single decoding method.
		filters = []string{string(flist)}
		switch p := s.Dict["DecodeParms"].(type) {
		case nil:
			break
		case Dict:
			parms = []Dict{p}
		default:
			return errors.New("stream /DecodeParms is not a dictionary")
		}
	case Array:
		// A list of multiple decoding methods, one after another.
		for _, n := range flist {
			if n, ok := n.(Name); ok {
				filters = append(filters, string(n))
			} else {
				return errors.New("stream /Filter entry is not a /Name")
			}
		}
		switch pa := s.Dict["DecodeParms"].(type) {
		case nil:
			break
		case Array:
			if len(pa) != len(flist) {
				return errors.New("stream /DecodeParms is array with wrong length")
			}
			parms = make([]Dict, len(pa))
			for i, p := range pa {
				if p, ok := p.(Dict); ok {
					parms[i] = p
				} else {
					return errors.New("stream /DecodeParams entry is not a dict")
				}
			}
		default:
			return errors.New("stream /DecodeParams is not an array")
		}
	default:
		return errors.New("stream /Filter is not a /Name or array")
	}
	// Apply the decoding methods, in order.
	for i, filter := range filters {
		var dp Dict
		if parms != nil {
			dp = parms[i]
		}
		switch filter {
		case "FlateDecode":
			if err := decompressFlateStream(s, dp, rowsize); err != nil {
				return err
			}
		default:
			return fmt.Errorf("stream /Filter encoding /%s is not supported", filter)
		}
	}
	delete(s.Dict, "Filter") // so we don't do it again
	return nil
}

// decompressFlateStream applies the "FlateDecode" method to the stream.
func decompressFlateStream(s *Stream, parms Dict, rowsize int) (err error) {
	// First, deflate the stream.
	dr, err := zlib.NewReader(bytes.NewReader(s.Data))
	if err != nil {
		return fmt.Errorf("running FlateDecode on stream: %s", err)
	}
	var buf bytes.Buffer
	if _, err = io.Copy(&buf, dr); err != nil {
		return fmt.Errorf("running FlateDecode on stream: %s", err)
	}
	dr.Close()
	s.Data = buf.Bytes()
	// Next, if the DecodeParams contains a Predictor algorithm, we have to
	// reverse that.
	if parms != nil {
		switch pred := parms["Predictor"].(type) {
		case nil:
			break
		case int:
			switch pred {
			case 1:
				// Identity — no predictor algorithm
				break
			case 10, 11, 12, 13, 14, 15:
				// PNG predictor algorithms.  Which one doesn't
				// matter; we decode them all the same.
				if rowsize == 0 {
					return errors.New("rowsize is needed for stream decoding and was not provided")
				}
				if s.Data, err = unpredictPNG(s.Data, rowsize); err != nil {
					return err
				}
			default:
				return fmt.Errorf("FlateDecode predictor %d is not supported", pred)
			}
		default:
			return errors.New("FlateDecode predictor is not an integer")
		}
	}
	return nil
}

// unpredictPNG reverses the PNG predictor algorithm on the data.  Each row of
// the data is preceded by a byte saying how that row was encoded.
func unpredictPNG(stream []byte, rowsize int) ([]byte, error) {
	if len(stream)%(rowsize+1) != 0 {
		return nil, errors.New("stream length is not a multiple of row length")
	}
	rows := len(stream) / (rowsize + 1)
	var out int
	for row := 0; row < rows; row++ {
		in := row * (rowsize + 1)
		switch stream[in] {
		case 0:
			// Zero means the row was not encoded, so just copy it
			// to the output.
			copy(stream[out:], stream[in+1:in+rowsize+1])
		case 2:
			// Two says that each row's values are subtracted from
			// the previous row's values, so we reverse that by
			// adding the previous row's values.
			if row == 0 {
				copy(stream[out:], stream[in+1:in+rowsize+1])
			} else {
				for b := 0; b < rowsize; b++ {
					stream[out+b] = stream[in+b+1] + stream[out+b-rowsize]
				}
			}
		default:
			return nil, fmt.Errorf("unexpected PNG filter type %d", stream[in])
		}
		out += rowsize
	}
	return stream[:out], nil
}
