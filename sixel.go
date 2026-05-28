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
)

func scale100(c uint32) int {
	return int(c) * 100 / 0xffff
}

var sixelPalette = append(color.Palette{color.Transparent}, palette.WebSafe...)

// Print renders the given image as a sixel.
func Print(w io.Writer, img image.Image) error {
	palettized := image.NewPaletted(img.Bounds(), sixelPalette)
	draw.FloydSteinberg.Draw(palettized, palettized.Bounds(), img, image.Point{})
	bw := bufio.NewWriter(w)
	if _, err := io.WriteString(bw, "\033P7;1q"); err != nil {
		return err
	}
	for i, c := range sixelPalette[1:] {
		r, g, b, _ := c.RGBA()
		if _, err := fmt.Fprintf(bw, "#%d;2;%d;%d;%d", i, scale100(r), scale100(g), scale100(b)); err != nil {
			return err
		}
	}
	for row := 0; row < palettized.Bounds().Dy(); row++ {
		y := palettized.Bounds().Min.Y + row
		if row > 0 {
			if row%6 == 0 {
				if _, err := io.WriteString(bw, "-"); err != nil {
					return err
				}
			} else {
				if _, err := io.WriteString(bw, "$"); err != nil {
					return err
				}
			}
		}
		for x := palettized.Bounds().Min.X; x < palettized.Bounds().Max.X; x++ {
			c := palettized.ColorIndexAt(x, y)
			if c == 0 {
				if _, err := fmt.Fprint(bw, "?"); err != nil {
					return err
				}
			} else {
				char := '?' + 1<<(row%6)
				if _, err := fmt.Fprintf(bw, "#%d%c", c-1, char); err != nil {
					return err
				}
			}
		}
	}
	if _, err := io.WriteString(bw, "\033\\"); err != nil {
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
			if _, err := io.WriteString(bw, "\n"); err != nil {
				return err
			}
		}
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			hi := img.At(x, y)
			if isFullyTransparent(hi) {
				if y+1 >= img.Bounds().Max.Y || isFullyTransparent(img.At(x, y+1)) {
					if _, err := io.WriteString(bw, "\033[49m "); err != nil {
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
					if _, err := io.WriteString(bw, "\033[49m"); err != nil {
						return err
					}
				}
				r, g, b, _ := hi.RGBA()
				if _, err := fmt.Fprintf(bw, "\033[38;2;%d;%d;%dm▀", r>>8, g>>8, b>>8); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(bw, "\033[39m\033[49m"); err != nil {
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
			if _, err := io.WriteString(bw, "\n"); err != nil {
				return err
			}
		}
		for x := palettized.Bounds().Min.X; x < palettized.Bounds().Max.X; x++ {
			c := palettized.At(x, y)
			if c == color.Transparent {
				if _, err := io.WriteString(bw, "\033[49m "); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(bw, "\033[%dm ", 40+c.(xtermColor)); err != nil {
					return err
				}
			}
		}
		if _, err := io.WriteString(bw, "\033[49m"); err != nil {
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	return nil
}
