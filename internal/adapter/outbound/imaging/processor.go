// Package imaging implementa port.ImageProcessor en Go puro: lee JPEG, PNG y WebP, endereza la foto
// según su orientación EXIF y genera cada ancho en JPEG. Volver a codificar descarta todos los
// metadatos (EXIF con la ubicación GPS del celular, modelo, fecha).
package imaging

import (
	"bytes"
	"cmp"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // registra el decodificador PNG
	"slices"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registra el decodificador WebP

	"github.com/santiago-noe/qatu-api/internal/core/domain"
	"github.com/santiago-noe/qatu-api/internal/core/port"
)

// jpegQuality: buen equilibrio entre peso y nitidez para fotos de herramientas.
const jpegQuality = 82

type Processor struct{}

func New() Processor { return Processor{} }

func (Processor) Process(data []byte, widths []int, maxPixels int) (port.ProcessedImage, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return port.ProcessedImage{}, domain.ErrPhotoInvalid
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return port.ProcessedImage{}, domain.ErrPhotoInvalid
	}
	orientation := exifOrientation(data)
	rotated := orientation >= 5 // 5 a 8 intercambian ancho y alto

	// Del más grande al más chico: cada tamaño se reduce desde el anterior (una sola pasada cara).
	sorted := slices.SortedFunc(slices.Values(widths), func(a, b int) int { return cmp.Compare(b, a) })
	out := port.ProcessedImage{Variants: make(map[int][]byte, len(widths))}
	current := src
	for i, width := range sorted {
		w, h := current.Bounds().Dx(), current.Bounds().Dy()
		if rotated {
			w, h = h, w
		}
		// Ancho final (ya enderezada), sin ampliar nunca.
		tw := min(width, w)
		th := max(1, h*tw/w)
		sw, sh := tw, th
		if rotated {
			sw, sh = th, tw
		}
		resized := resize(current, sw, sh)
		current = resized
		final := orient(resized, orientation)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, final, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return port.ProcessedImage{}, err
		}
		out.Variants[width] = buf.Bytes()
		if i == 0 {
			out.Width, out.Height = final.Bounds().Dx(), final.Bounds().Dy()
		}
	}
	return out, nil
}

// resize escala sobre fondo blanco (un PNG transparente no queda negro en JPEG).
func resize(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if src.Bounds().Dx() == w && src.Bounds().Dy() == h {
		draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Over)
		return dst
	}
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// orient aplica la orientación EXIF (1 a 8): lo guardado en (x, y) se muestra en otra posición.
func orient(img *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return img
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	ow, oh := w, h
	if o >= 5 {
		ow, oh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		for x := range w {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			si := img.PixOffset(x, y)
			di := dst.PixOffset(dx, dy)
			copy(dst.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return dst
}
