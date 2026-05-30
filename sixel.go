// Package sixel can be used to render image.Image to the terminal using
// various strategies (including sixel).
package sixel

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"image/draw"
	"io"
	"slices"
)

type sixelRGB struct {
	r, g, b int8
}

func (c sixelRGB) RGBA() (r, g, b, a uint32) {
	return uint32(c.r) * 0xffff / 100, uint32(c.g) * 0xffff / 100, uint32(c.b) * 0xffff / 100, 0xffff
}

var defaultSixelPalette color.Palette

func init() {
	defaultSixelPalette = slices.Grow(color.Palette{color.Transparent}, len(palette.WebSafe))
	for _, c := range palette.WebSafe {
		r, g, b, _ := c.RGBA()
		defaultSixelPalette = append(defaultSixelPalette, sixelRGB{int8(r * 100 / 0xffff), int8(g * 100 / 0xffff), int8(b * 100 / 0xffff)})
	}
}

func sixelizePalette(p color.Palette) color.Palette {
	var result color.Palette
	includeTransparent := false
	for _, c := range p {
		r, g, b, a := c.RGBA()
		if a == 0 {
			includeTransparent = true
			continue
		}
		result = append(result, sixelRGB{r: int8(r * 100 / 0xffff), g: int8(g * 100 / 0xffff), b: int8(b * 100 / 0xffff)})
	}
	if includeTransparent {
		result = append(color.Palette{color.Transparent}, result...)
	}
	return result
}

// Print renders the given image as a sixel. The given palette will be used if
// provided, otherwise a default palette will be selected.
func Print(w io.Writer, img image.Image, p color.Palette) error {
	if len(p) == 0 {
		p = defaultSixelPalette
	} else {
		p = sixelizePalette(p)
	}
	palettized := image.NewPaletted(img.Bounds(), p)
	draw.FloydSteinberg.Draw(palettized, palettized.Bounds(), img, image.Point{})
	bw := bufio.NewWriter(w)
	if _, err := bw.WriteString("\033P7;1q"); err != nil {
		return err
	}
	for i, c := range palettized.ColorModel().(color.Palette) {
		if c == color.Transparent {
			continue
		}
		sc := c.(sixelRGB)
		if _, err := fmt.Fprintf(bw, "#%d;2;%d;%d;%d", i, sc.r, sc.g, sc.b); err != nil {
			return err
		}
	}
	for row := 0; row < palettized.Bounds().Dy(); row += 6 {
		y0 := palettized.Bounds().Min.Y + row
		if row > 0 {
			if _, err := bw.WriteString("-"); err != nil {
				return err
			}
		}
		colors := make([]bool, 256)
		for y := y0; y < y0+6; y++ {
			for x := palettized.Bounds().Min.X; x < palettized.Bounds().Max.X; x++ {
				colors[palettized.ColorIndexAt(x, y)] = true
			}
		}
		if p[0] == color.Transparent {
			colors[0] = false
		}

		first := true
		for ci, present := range colors {
			c := uint8(ci)
			if !present {
				continue
			}
			if !first {
				if _, err := bw.WriteString("$"); err != nil {
					return err
				}
			}
			first = false
			if _, err := fmt.Fprintf(bw, "#%d", c); err != nil {
				return err
			}
			var (
				lastChar    byte
				lastCharLen int
			)
			for x := palettized.Bounds().Min.X; x < palettized.Bounds().Max.X; x++ {
				var char byte
				for j := range 6 {
					y := y0 + j
					if palettized.ColorIndexAt(x, y) == c {
						char |= 1 << j
					}
				}
				char += '?'
				if lastCharLen > 0 && lastChar != char {
					if lastCharLen < 4 {
						for range lastCharLen {
							if err := bw.WriteByte(lastChar); err != nil {
								return err
							}
						}
					} else {
						if _, err := fmt.Fprintf(bw, "!%d%c", lastCharLen, lastChar); err != nil {
							return err
						}
					}
					lastCharLen = 0
				}
				lastChar = char
				lastCharLen++
			}
			if lastCharLen > 0 && lastChar != '?' {
				if lastCharLen < 4 {
					for range lastCharLen {
						if err := bw.WriteByte(lastChar); err != nil {
							return err
						}
					}
				} else {
					if _, err := fmt.Fprintf(bw, "!%d%c", lastCharLen, lastChar); err != nil {
						return err
					}
				}
			}
		}
	}
	if _, err := bw.WriteString("\033\\"); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return nil
}

func isFullyTransparent(c color.Color) bool {
	_, _, _, a := c.RGBA()
	return a == 0
}

// PrintBlock renders the given image using block characters. Yeah I know it's
// not a sixel but it could be used as a fallback if sixel isn't supported.
func PrintBlock(w io.Writer, img image.Image) error {
	bw := bufio.NewWriter(w)
	for row := 0; row < img.Bounds().Dy(); row += 2 {
		y := img.Bounds().Min.Y + row
		if row > 0 {
			if _, err := bw.WriteString("\n"); err != nil {
				return err
			}
		}
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			hi := img.At(x, y)
			if isFullyTransparent(hi) {
				if y+1 >= img.Bounds().Max.Y || isFullyTransparent(img.At(x, y+1)) {
					if _, err := bw.WriteString("\033[49m "); err != nil {
						return err
					}
				} else {
					r, g, b, _ := img.At(x, y+1).RGBA()
					if _, err := fmt.Fprintf(bw, "\033[38;2;%d;%d;%dm\033[49m▄", r>>8, g>>8, b>>8); err != nil {
						return err
					}
				}
			} else {
				if y+1 < img.Bounds().Max.Y && !isFullyTransparent(img.At(x, y+1)) {
					r, g, b, _ := img.At(x, y+1).RGBA()
					if _, err := fmt.Fprintf(bw, "\033[48;2;%d;%d;%dm", r>>8, g>>8, b>>8); err != nil {
						return err
					}
				} else {
					if _, err := bw.WriteString("\033[49m"); err != nil {
						return err
					}
				}
				r, g, b, _ := hi.RGBA()
				if _, err := fmt.Fprintf(bw, "\033[38;2;%d;%d;%dm▀", r>>8, g>>8, b>>8); err != nil {
					return err
				}
			}
		}
		if _, err := bw.WriteString("\033[39m\033[49m"); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return nil
}

type xtermColor int8

func (c xtermColor) RGBA() (r, g, b, a uint32) {
	var col color.RGBA
	switch c {
	case 0:
		col = color.RGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff}
	case 1:
		col = color.RGBA{R: 0xcd, G: 0x00, B: 0x00, A: 0xff}
	case 2:
		col = color.RGBA{R: 0x00, G: 0xcd, B: 0x00, A: 0xff}
	case 3:
		col = color.RGBA{R: 0xcd, G: 0xcd, B: 0x00, A: 0xff}
	case 4:
		col = color.RGBA{R: 0x00, G: 0x00, B: 0xee, A: 0xff}
	case 5:
		col = color.RGBA{R: 0xcd, G: 0x00, B: 0xcd, A: 0xff}
	case 6:
		col = color.RGBA{R: 0x00, G: 0xcd, B: 0xcd, A: 0xff}
	case 7:
		col = color.RGBA{R: 0xe5, G: 0xe5, B: 0xe5, A: 0xff}
	case 60:
		col = color.RGBA{R: 0x7f, G: 0x7f, B: 0x7f, A: 0xff}
	case 61:
		col = color.RGBA{R: 0xff, G: 0x00, B: 0x00, A: 0xff}
	case 62:
		col = color.RGBA{R: 0x00, G: 0xff, B: 0x00, A: 0xff}
	case 63:
		col = color.RGBA{R: 0xff, G: 0xff, B: 0x00, A: 0xff}
	case 64:
		col = color.RGBA{R: 0x5c, G: 0x5c, B: 0xff, A: 0xff}
	case 65:
		col = color.RGBA{R: 0xff, G: 0x00, B: 0xff, A: 0xff}
	case 66:
		col = color.RGBA{R: 0x00, G: 0xff, B: 0xff, A: 0xff}
	case 67:
		col = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	default:
		panic("not an xterm color")
	}
	return col.RGBA()
}

var xtermPalette = color.Palette{color.Transparent, xtermColor(0), xtermColor(1), xtermColor(2), xtermColor(3), xtermColor(4), xtermColor(5), xtermColor(6), xtermColor(7), xtermColor(60), xtermColor(61), xtermColor(62), xtermColor(63), xtermColor(64), xtermColor(65), xtermColor(66), xtermColor(67)}

// PrintXterm16 renders the given image using the basic XTerm 16 colors. This
// should have great compatibility and it looks pretty impressively bad.
func PrintXTerm16(w io.Writer, img image.Image) error {
	palettized := image.NewPaletted(img.Bounds(), xtermPalette)
	draw.FloydSteinberg.Draw(palettized, palettized.Bounds(), img, image.Point{})
	bw := bufio.NewWriter(w)
	for row := 0; row < palettized.Bounds().Dy(); row++ {
		y := palettized.Bounds().Min.Y + row
		if row > 0 {
			if _, err := bw.WriteString("\n"); err != nil {
				return err
			}
		}
		for x := palettized.Bounds().Min.X; x < palettized.Bounds().Max.X; x++ {
			c := palettized.At(x, y)
			if c == color.Transparent {
				if _, err := bw.WriteString("\033[49m "); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(bw, "\033[%dm ", 40+c.(xtermColor)); err != nil {
					return err
				}
			}
		}
		if _, err := bw.WriteString("\033[49m"); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return nil
}
