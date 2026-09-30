package imaging

import "encoding/binary"

// exifOrientation lee la etiqueta Orientation (0x0112) del EXIF de un JPEG; 1 (normal) si no hay
// EXIF, no es un JPEG o el bloque está roto. Solo se lee ese dato: el resto del EXIF se descarta.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xDA || marker == 0xD9 { // empiezan los datos de la imagen: ya no hay EXIF
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		end := i + 2 + size
		if size < 2 || end > len(data) {
			return 1
		}
		if marker == 0xE1 {
			if o, ok := orientationFromAPP1(data[i+4 : end]); ok {
				return o
			}
		}
		i = end
	}
	return 1
}

func orientationFromAPP1(seg []byte) (int, bool) {
	if len(seg) < 14 || string(seg[:6]) != "Exif\x00\x00" {
		return 0, false
	}
	tiff := seg[6:]
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 0, false
	}
	ifd := int(order.Uint32(tiff[4:]))
	if ifd+2 > len(tiff) {
		return 0, false
	}
	entries := int(order.Uint16(tiff[ifd:]))
	for e := range entries {
		at := ifd + 2 + e*12
		if at+12 > len(tiff) {
			return 0, false
		}
		if order.Uint16(tiff[at:]) == 0x0112 {
			o := int(order.Uint16(tiff[at+8:]))
			if o >= 1 && o <= 8 {
				return o, true
			}
			return 0, false
		}
	}
	return 0, false
}
