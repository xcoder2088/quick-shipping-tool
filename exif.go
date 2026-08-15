// Copyright (c) 2025-2026 Francois "Brad" Bradette. All rights reserved.
// Proprietary and confidential. Not open source. See LICENSE.
package main

import (
	"encoding/binary"
	"errors"
	"image"
)

// ==================================================
// EXIF ORIENTATION (rotation fix for phone photos)
// ==================================================
// Phones don't physically rotate the pixel data when a photo is taken in
// portrait — they save the raw sensor data and add an invisible EXIF
// "Orientation" tag telling viewers how to display it correctly.
//
// Decoding an image (image.Decode) and re-encoding it (as watermarkPhoto
// does, to stamp the badge) throws that tag away, since Go's standard
// image/jpeg package does not read or preserve EXIF. The result: the
// pixels themselves were never rotated, so the saved file comes out
// sideways once the tag that used to fix it is gone.
//
// readExifOrientation reads just the one EXIF tag we need directly from
// the raw JPEG bytes, with no external dependency. Returns 1 (normal) if
// the file isn't a JPEG, has no EXIF data, or anything looks unexpected —
// callers should treat any error/1 as "nothing to correct".

func readExifOrientation(data []byte) int {

	orientation, err := parseExifOrientation(data)
	if err != nil || orientation < 1 || orientation > 8 {
		return 1
	}

	return orientation
}

func parseExifOrientation(data []byte) (int, error) {

	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1, errors.New("not a JPEG")
	}

	pos := 2

	for pos+4 <= len(data) {

		if data[pos] != 0xFF {
			return 1, errors.New("malformed marker")
		}

		marker := data[pos+1]

		// Start of Scan — image data begins, no more markers to check
		if marker == 0xDA {
			break
		}

		segmentLen := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		segmentStart := pos + 4

		if segmentLen < 2 || segmentStart+segmentLen-2 > len(data) {
			return 1, errors.New("bad segment length")
		}

		// APP1 marker containing "Exif\0\0"
		if marker == 0xE1 &&
			segmentLen >= 8 &&
			string(data[segmentStart:segmentStart+6]) == "Exif\x00\x00" {

			return parseTIFFOrientation(data[segmentStart+6 : segmentStart+segmentLen-2])
		}

		pos = segmentStart + segmentLen - 2
	}

	return 1, errors.New("no exif segment found")
}

func parseTIFFOrientation(tiff []byte) (int, error) {

	if len(tiff) < 8 {
		return 1, errors.New("tiff header too short")
	}

	var byteOrder binary.ByteOrder

	switch string(tiff[0:2]) {
	case "II":
		byteOrder = binary.LittleEndian
	case "MM":
		byteOrder = binary.BigEndian
	default:
		return 1, errors.New("unknown byte order")
	}

	ifdOffset := int(byteOrder.Uint32(tiff[4:8]))
	if ifdOffset+2 > len(tiff) {
		return 1, errors.New("bad ifd offset")
	}

	entryCount := int(byteOrder.Uint16(tiff[ifdOffset : ifdOffset+2]))
	entriesStart := ifdOffset + 2

	for i := 0; i < entryCount; i++ {

		entryStart := entriesStart + i*12
		if entryStart+12 > len(tiff) {
			break
		}

		tag := byteOrder.Uint16(tiff[entryStart : entryStart+2])

		// 0x0112 = Orientation tag, type SHORT, value in the first 2 bytes
		// of the 4-byte value field
		if tag == 0x0112 {
			value := byteOrder.Uint16(tiff[entryStart+8 : entryStart+10])
			return int(value), nil
		}
	}

	return 1, errors.New("orientation tag not present")
}

// applyExifOrientation physically rotates/flips img so the pixel data
// matches what the EXIF tag says it should look like, then the tag itself
// becomes unnecessary (and is dropped, since we re-encode as plain JPEG).
func applyExifOrientation(img image.Image, orientation int) image.Image {

	switch orientation {

	case 2:
		return flipHorizontal(img)
	case 3:
		return rotate180(img)
	case 4:
		return flipVertical(img)
	case 5:
		return flipHorizontal(rotate90CW(img))
	case 6:
		return rotate90CW(img)
	case 7:
		return flipHorizontal(rotate90CCW(img))
	case 8:
		return rotate90CCW(img)
	default:
		return img // 1, or anything unrecognized — already correct
	}
}

func rotate90CW(img image.Image) image.Image {

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, h, w))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(h-1-y, x, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}

	return dst
}

func rotate90CCW(img image.Image) image.Image {

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, h, w))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(y, w-1-x, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}

	return dst
}

func rotate180(img image.Image) image.Image {

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, h-1-y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}

	return dst
}

func flipHorizontal(img image.Image) image.Image {

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(w-1-x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}

	return dst
}

func flipVertical(img image.Image) image.Image {

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()

	dst := image.NewRGBA(image.Rect(0, 0, w, h))

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dst.Set(x, h-1-y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}

	return dst
}
