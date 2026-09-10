package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"strconv"
)

func main() {
	sizes := []int{192, 512}
	bg := color.RGBA{R: 9, G: 9, B: 11, A: 255}     // zinc-950
	fg := color.RGBA{R: 249, G: 115, B: 22, A: 255} // orange-500

	for _, size := range sizes {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		for y := 0; y < size; y++ {
			for x := 0; x < size; x++ {
				img.Set(x, y, bg)
			}
		}
		drawLetterP(img, fg, size)

		path := "web/public/icon-" + strconv.Itoa(size) + ".png"
		f, err := os.Create(path)
		if err != nil {
			log.Fatalf("genicons: create %s: %v", path, err)
		}
		if err := png.Encode(f, img); err != nil {
			log.Fatalf("genicons: encode %s: %v", path, err)
		}
		f.Close()
		log.Printf("genicons: wrote %s", path)
	}
}

// drawLetterP draws a simple filled "P" glyph as a placeholder mark,
// built from rectangles (no font dependency).
func drawLetterP(img *image.RGBA, fg color.RGBA, size int) {
	m := size / 8
	stemW := size / 6
	bowlH := size/2 - m

	fillRect(img, fg, m, m, m+stemW, size-m)
	fillRect(img, fg, m, m, size-m, m+stemW)
	fillRect(img, fg, size-m-stemW, m, size-m, m+bowlH)
	fillRect(img, fg, m, m+bowlH-stemW, size-m, m+bowlH)
}

func fillRect(img *image.RGBA, c color.RGBA, x0, y0, x1, y1 int) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			img.Set(x, y, c)
		}
	}
}
