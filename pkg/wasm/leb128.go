package wasm

// AppendUleb128 encodes an unsigned 32-bit integer into LEB128 format and appends it to buf.
func AppendUleb128(buf []byte, val uint32) []byte {
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if val == 0 {
			break
		}
	}
	return buf
}

// AppendUleb128_64 encodes an unsigned 64-bit integer into LEB128 format and appends it to buf.
func AppendUleb128_64(buf []byte, val uint64) []byte {
	for {
		b := byte(val & 0x7F)
		val >>= 7
		if val != 0 {
			b |= 0x80
		}
		buf = append(buf, b)
		if val == 0 {
			break
		}
	}
	return buf
}

// AppendSleb128 encodes a signed 32-bit integer into two's complement LEB128 format and appends it to buf.
func AppendSleb128(buf []byte, val int32) []byte {
	more := true
	for more {
		b := byte(val & 0x7F)
		val >>= 7
		signBit := (b & 0x40) != 0

		if (val == 0 && !signBit) || (val == -1 && signBit) {
			more = false
		} else {
			b |= 0x80
		}
		buf = append(buf, b)
	}
	return buf
}

// AppendSleb128_64 encodes a signed 64-bit integer into two's complement LEB128 format and appends it to buf.
func AppendSleb128_64(buf []byte, val int64) []byte {
	more := true
	for more {
		b := byte(val & 0x7F)
		val >>= 7
		signBit := (b & 0x40) != 0

		if (val == 0 && !signBit) || (val == -1 && signBit) {
			more = false
		} else {
			b |= 0x80
		}
		buf = append(buf, b)
	}
	return buf
}
