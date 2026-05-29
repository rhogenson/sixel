// Package sixel can be used to render image.Image to the terminal using
// various strategies (including sixel).
package sixel

import (
	"bufio"
	"cmp"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"maps"
	"math"
	"slices"
)

func scale100(c int64) int8 {
	return int8(c * 100 / 0xff)
}

func scaleFFFF(c int8) uint32 {
	return uint32(c) * 0xffff / 100
}

type sixelRGB struct {
	r, g, b int8
}

func (c sixelRGB) RGBA() (r, g, b, a uint32) {
	return scaleFFFF(c.r), scaleFFFF(c.g), scaleFFFF(c.b), 0xffff
}

func bucketRange(colors []color.RGBA) (rRange, gRange, bRange uint8) {
	if len(colors) == 0 {
		return 0, 0, 0
	}
	var minR, minG, minB uint8 = math.MaxUint8, math.MaxUint8, math.MaxUint8
	var maxR, maxG, maxB uint8
	for _, c := range colors {
		minR, maxR = min(minR, c.R), max(maxR, c.R)
		minG, maxG = min(minG, c.G), max(maxG, c.G)
		minB, maxB = min(minB, c.B), max(maxB, c.B)
	}
	return maxR - minR, maxG - minG, maxB - minB
}

func cutOnce(colors []color.RGBA) [2][]color.RGBA {
	rRange, gRange, bRange := bucketRange(colors)
	if rRange >= gRange && rRange >= bRange {
		slices.SortFunc(colors, func(x, y color.RGBA) int {
			return cmp.Or(cmp.Compare(x.R, y.R), cmp.Compare(x.G, y.G), cmp.Compare(x.B, y.B))
		})
	} else if gRange >= rRange && gRange >= bRange {
		slices.SortFunc(colors, func(x, y color.RGBA) int {
			return cmp.Or(cmp.Compare(x.G, y.G), cmp.Compare(x.R, y.R), cmp.Compare(x.B, y.B))
		})
	} else {
		slices.SortFunc(colors, func(x, y color.RGBA) int {
			return cmp.Or(cmp.Compare(x.B, y.B), cmp.Compare(x.R, y.R), cmp.Compare(x.G, y.G))
		})
	}
	return [...][]color.RGBA{colors[:len(colors)/2], colors[len(colors)/2:]}
}

func colorAvg(colors []color.RGBA) sixelRGB {
	var r, g, b int64
	for _, c := range colors {
		r += int64(c.R)
		g += int64(c.G)
		b += int64(c.B)
	}
	n := int64(len(colors))
	return sixelRGB{r: scale100(r / n), g: scale100(g / n), b: scale100(b / n)}
}

func medianCut(img image.Image) color.Palette {
	var colors []color.RGBA
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			if a > 0 {
				colors = append(colors, color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: 0xff})
			}
		}
	}
	buckets := [][]color.RGBA{colors}
	for len(buckets) < 255 {
		var bestRange uint8
		var bestIdx int
		for i, b := range buckets {
			r := max(bucketRange(b))
			if r >= bestRange {
				bestRange = r
				bestIdx = i
			}
		}
		split := cutOnce(buckets[bestIdx])
		buckets = slices.Replace(buckets, bestIdx, bestIdx+1, split[:]...)
	}
	var paletteRGB []sixelRGB
	for _, bucket := range buckets {
		if len(bucket) > 0 {
			paletteRGB = append(paletteRGB, colorAvg(bucket))
		}
	}
	slices.SortFunc(paletteRGB, func(x, y sixelRGB) int {
		return cmp.Or(cmp.Compare(x.r, y.r), cmp.Compare(x.g, y.g), cmp.Compare(x.b, y.b))
	})
	paletteRGB = slices.Compact(paletteRGB)
	palette := slices.Grow(color.Palette{color.Transparent}, len(paletteRGB))
	for _, c := range paletteRGB {
		palette = append(palette, c)
	}
	return palette
}

// Print renders the given image as a sixel.
func Print(w io.Writer, img image.Image) error {
	palette := medianCut(img)
	palettized := image.NewPaletted(img.Bounds(), palette)
	draw.FloydSteinberg.Draw(palettized, palettized.Bounds(), img, image.Point{})
	bw := bufio.NewWriter(w)
	if _, err := bw.WriteString("\033P7;1q"); err != nil {
		return err
	}
	for i, c := range palette[1:] {
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
		colors := make(map[uint8]bool)
		for y := y0; y <= y0+6; y++ {
			for x := palettized.Bounds().Min.X; x < palettized.Bounds().Max.X; x++ {
				if c := palettized.ColorIndexAt(x, y); c != 0 {
					colors[c] = true
				}
			}
		}
		for i, c := range slices.Sorted(maps.Keys(colors)) {
			if i > 0 {
				if _, err := bw.WriteString("$"); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(bw, "#%d", c-1); err != nil {
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
