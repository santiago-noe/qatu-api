package imaging

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/santiago-noe/qatu-api/internal/core/domain"
)

var (
	red  = color.RGBA{R: 220, A: 255}
	blue = color.RGBA{B: 220, A: 255}
)

// halves es una imagen w×h con la mitad izquierda roja y la derecha azul.
func halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			if x < w/2 {
				img.Set(x, y, red)
			} else {
				img.Set(x, y, blue)
			}
		}
	}
	return img
}

// withOrientation arma un JPEG con un bloque EXIF (Motorola) que solo trae Orientation y una
// etiqueta de GPS falsa, para comprobar que nada del EXIF sobrevive.
func withOrientation(t *testing.T, img image.Image, orientation uint16) []byte {
	t.Helper()
	var plain bytes.Buffer
	if err := jpeg.Encode(&plain, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08") // big endian, IFD0 en el byte 8
	tiff = binary.BigEndian.AppendUint16(tiff, 2)
	tiff = append(tiff, 0x01, 0x12, 0x00, 0x03, 0, 0, 0, 1) // Orientation, SHORT, 1 valor
	tiff = binary.BigEndian.AppendUint16(tiff, orientation)
	tiff = append(tiff, 0, 0)
	tiff = append(tiff, 0x88, 0x25, 0x00, 0x04, 0, 0, 0, 1, 0, 0, 0, 0) // GPSInfo (falso)
	tiff = append(tiff, 0, 0, 0, 0)
	payload := append([]byte("Exif\x00\x00"), tiff...)
	app1 := []byte{0xFF, 0xE1}
	app1 = binary.BigEndian.AppendUint16(app1, uint16(len(payload)+2))
	app1 = append(app1, payload...)
	raw := plain.Bytes()
	return append(append(append([]byte{}, raw[:2]...), app1...), raw[2:]...)
}

func decode(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func near(c color.Color, want color.RGBA) bool {
	r, g, b, _ := c.RGBA()
	d := func(a uint32, b uint8) bool { v := int(a>>8) - int(b); return v > -40 && v < 40 }
	return d(r, want.R) && d(g, want.G) && d(b, want.B)
}

func TestProcessSizesAndNoUpscale(t *testing.T) {
	var src bytes.Buffer
	_ = png.Encode(&src, halves(1000, 500))
	out, err := New().Process(src.Bytes(), []int{320, 800, 1600}, domain.MaxPhotoPixels)
	if err != nil {
		t.Fatal(err)
	}
	if out.Width != 1000 || out.Height != 500 {
		t.Fatalf("la más grande no se amplía: %dx%d", out.Width, out.Height)
	}
	for width, want := range map[int][2]int{320: {320, 160}, 800: {800, 400}, 1600: {1000, 500}} {
		b := decode(t, out.Variants[width]).Bounds()
		if b.Dx() != want[0] || b.Dy() != want[1] {
			t.Errorf("%d: %dx%d, quiero %v", width, b.Dx(), b.Dy(), want)
		}
	}
}

func TestProcessAppliesOrientationAndDropsEXIF(t *testing.T) {
	// Guardada acostada (40×20) con Orientation 6: se muestra girada 90° a la derecha (20×40).
	data := withOrientation(t, halves(40, 20), 6)
	if o := exifOrientation(data); o != 6 {
		t.Fatalf("orientación leída = %d", o)
	}
	out, err := New().Process(data, []int{320}, domain.MaxPhotoPixels)
	if err != nil {
		t.Fatal(err)
	}
	img := decode(t, out.Variants[320])
	if b := img.Bounds(); b.Dx() != 20 || b.Dy() != 40 || out.Width != 20 || out.Height != 40 {
		t.Fatalf("enderezada: %v (%dx%d)", b, out.Width, out.Height)
	}
	// Al girar a la derecha, la izquierda (roja) queda arriba.
	if !near(img.At(10, 5), red) || !near(img.At(10, 35), blue) {
		t.Fatalf("colores: arriba %v, abajo %v", img.At(10, 5), img.At(10, 35))
	}
	if bytes.Contains(out.Variants[320], []byte("Exif")) {
		t.Fatal("la foto procesada no lleva EXIF")
	}
}

func TestOrientAllCases(t *testing.T) {
	// Un píxel marcado en (0, 0) de una imagen 3×2 termina en cada esquina según la orientación.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, red)
	want := map[int]image.Point{1: {0, 0}, 2: {2, 0}, 3: {2, 1}, 4: {0, 1}, 5: {0, 0}, 6: {1, 0}, 7: {1, 2}, 8: {0, 2}}
	for o, p := range want {
		out := orient(src, o)
		if out.RGBAAt(p.X, p.Y) != red {
			t.Errorf("orientación %d: el píxel no está en %v", o, p)
		}
	}
}

func TestProcessRejects(t *testing.T) {
	var small bytes.Buffer
	_ = png.Encode(&small, halves(20, 20))
	if _, err := New().Process(small.Bytes(), []int{320}, 100); !errors.Is(err, domain.ErrPhotoInvalid) {
		t.Fatal("más píxeles de los permitidos: se rechaza antes de decodificar")
	}
	for name, data := range map[string][]byte{
		"texto":        []byte("no soy una imagen"),
		"JPEG cortado": small.Bytes()[:40],
		"vacío":        nil,
	} {
		if _, err := New().Process(data, []int{320}, domain.MaxPhotoPixels); !errors.Is(err, domain.ErrPhotoInvalid) {
			t.Errorf("%s: quiero ErrPhotoInvalid, llegó %v", name, err)
		}
	}
}

func TestExifOrientationDefaults(t *testing.T) {
	for name, data := range map[string][]byte{
		"no es JPEG":        []byte("\x89PNG"),
		"JPEG sin EXIF":     {0xFF, 0xD8, 0xFF, 0xDA, 0, 2},
		"segmento truncado": {0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF},
	} {
		if o := exifOrientation(data); o != 1 {
			t.Errorf("%s: %d", name, o)
		}
	}
}
