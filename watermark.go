package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"os"
	"strings"
)

// ==================================================
// EMBEDDED WATERMARK BADGE
// ==================================================
// Small QuickProof logo + "Powered by QuickQuoteTool.ca" caption,
// pre-rendered as a single transparent PNG (static/watermark/watermark.png).
// Keeping the badge pre-rendered avoids pulling in a font-rendering
// dependency here — this file only needs the standard library.

//go:embed static/watermark/watermark.png
var watermarkPNG []byte

// Badge width as a fraction of the photo's width. Kept small so the mark
// stays discreet in a corner while still being legible.
const watermarkWidthRatio = 0.08

// Margin from the photo edges, as a fraction of the photo's width.
const watermarkMarginRatio = 0.012

// watermarkPhoto opens the image at path, stamps the QuickProof badge in
// the bottom-right corner, and overwrites the file in place. It is called
// once per saved photo, right after the upload is written to disk — so the
// same stamped file is what later gets attached to the outgoing email.
//
// Unsupported/undecodable files are left untouched (returns nil) so a
// single bad file never blocks the rest of the upload.
func watermarkPhoto(path string) error {

	original, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("watermark: open %s: %w", path, err)
	}

	img, format, decodeErr := image.Decode(original)
	original.Close()

	if decodeErr != nil {
		// Not a decodable/supported image — skip silently, the original
		// upload stays exactly as-is.
		return nil
	}

	badgeImg, err := png.Decode(bytes.NewReader(watermarkPNG))
	if err != nil {
		return fmt.Errorf("watermark: decode embedded badge: %w", err)
	}

	bounds := img.Bounds()
	photoW := bounds.Dx()

	badgeW := int(float64(photoW) * watermarkWidthRatio)
	if badgeW < 40 {
		// Photo is tiny (thumbnail-sized) — not worth stamping.
		return nil
	}

	badgeBounds := badgeImg.Bounds()
	scale := float64(badgeW) / float64(badgeBounds.Dx())
	badgeH := int(float64(badgeBounds.Dy()) * scale)

	if badgeH < 1 {
		return nil
	}

	scaledBadge := resizeRGBA(badgeImg, badgeW, badgeH)

	margin := int(float64(photoW) * watermarkMarginRatio)

	dst := image.NewRGBA(bounds)
	draw.Draw(dst, bounds, img, bounds.Min, draw.Src)

	position := image.Pt(
		bounds.Max.X-badgeW-margin,
		bounds.Max.Y-badgeH-margin,
	)

	draw.Draw(
		dst,
		image.Rect(position.X, position.Y, position.X+badgeW, position.Y+badgeH),
		scaledBadge,
		image.Point{},
		draw.Over,
	)

	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("watermark: create %s: %w", path, err)
	}
	defer out.Close()

	lowerPath := strings.ToLower(path)

	switch {
	case strings.HasSuffix(lowerPath, ".png"):
		err = png.Encode(out, dst)
	case format == "png":
		err = png.Encode(out, dst)
	default:
		// .jpg/.jpeg and anything else supported falls back to JPEG, which
		// covers the vast majority of camera uploads.
		err = jpeg.Encode(out, dst, &jpeg.Options{Quality: 90})
	}

	if err != nil {
		return fmt.Errorf("watermark: encode %s: %w", path, err)
	}

	return nil
}

// resizeRGBA downscales src into a targetW x targetH image using a simple
// box filter (area averaging over premultiplied alpha). This is only used
// for shrinking the badge, and needs no dependency beyond the standard
// library — good enough quality for a small corner watermark.
func resizeRGBA(src image.Image, targetW, targetH int) *image.RGBA {

	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))

	for y := 0; y < targetH; y++ {

		sy0 := y * srcH / targetH
		sy1 := (y + 1) * srcH / targetH
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}

		for x := 0; x < targetW; x++ {

			sx0 := x * srcW / targetW
			sx1 := (x + 1) * srcW / targetW
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}

			var rSum, gSum, bSum, aSum, count uint64

			for sy := sy0; sy < sy1 && sy < srcH; sy++ {
				for sx := sx0; sx < sx1 && sx < srcW; sx++ {

					r, g, b, a := src.At(
						srcBounds.Min.X+sx,
						srcBounds.Min.Y+sy,
					).RGBA()

					rSum += uint64(r)
					gSum += uint64(g)
					bSum += uint64(b)
					aSum += uint64(a)
					count++
				}
			}

			if count == 0 {
				continue
			}

			dst.SetRGBA(x, y, color.RGBA{
				R: uint8((rSum / count) >> 8),
				G: uint8((gSum / count) >> 8),
				B: uint8((bSum / count) >> 8),
				A: uint8((aSum / count) >> 8),
			})
		}
	}

	return dst
}
