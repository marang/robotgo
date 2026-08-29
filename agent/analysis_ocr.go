//go:build ocr && cgo

package agent

/*
#cgo pkg-config: tesseract lept
#include <stdlib.h>
#include <string.h>
#include <tesseract/capi.h>
#include <leptonica/allheaders.h>

static void robotgoDestroyPix(PIX *pix) {
	if (pix != NULL) {
		pixDestroy(&pix);
	}
}
*/
import "C"

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"strings"
	"unsafe"
)

const (
	ocrBackendAvailable = true
	ocrBackendName      = OCRAnalysisBackend
	ocrModelName        = OCRAnalysisModel
)

type tesseractMemoryAnalyzer struct{}

func defaultOCRAnalyzer() ocrAnalyzer { return tesseractMemoryAnalyzer{} }

func (tesseractMemoryAnalyzer) Analyze(ctx context.Context, source *image.RGBA, languages []string) ([]rawOCRBox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, source); err != nil {
		return nil, err
	}
	wipeMutableImage(source)
	data := encoded.Bytes()
	defer clear(data)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pix := C.pixReadMem((*C.l_uint8)(unsafe.Pointer(&data[0])), C.size_t(len(data)))
	if pix == nil {
		return nil, fmt.Errorf("decode in-memory OCR image")
	}
	defer C.robotgoDestroyPix(pix)
	clear(data)
	api := C.TessBaseAPICreate()
	if api == nil {
		return nil, fmt.Errorf("initialize in-memory OCR backend")
	}
	defer func() {
		C.TessBaseAPIEnd(api)
		C.TessBaseAPIDelete(api)
	}()
	language := C.CString(strings.Join(languages, "+"))
	defer C.free(unsafe.Pointer(language))
	if status := C.TessBaseAPIInit3(api, nil, language); status != 0 {
		return nil, fmt.Errorf("initialize in-memory OCR language model")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	C.TessBaseAPISetImage2(api, pix)
	if status := C.TessBaseAPIRecognize(api, nil); status != 0 {
		return nil, fmt.Errorf("run in-memory OCR recognition")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	iterator := C.TessBaseAPIGetIterator(api)
	if iterator == nil {
		return []rawOCRBox{}, nil
	}
	defer C.TessResultIteratorDelete(iterator)
	result := make([]rawOCRBox, 0, 64)
	for {
		if err := ctx.Err(); err != nil {
			clearRawOCRBoxes(result)
			return nil, err
		}
		word := C.TessResultIteratorGetUTF8Text(iterator, C.RIL_WORD)
		if word != nil {
			length := C.strlen(word)
			wordTruncated := length > C.size_t(maxAgentAnalysisTextBytes)
			if wordTruncated {
				length = C.size_t(maxAgentAnalysisTextBytes)
			}
			text := C.GoBytes(unsafe.Pointer(word), C.int(length))
			C.TessDeleteText(word)
			var x1, y1, x2, y2 C.int
			if C.TessPageIteratorBoundingBox(
				(*C.TessPageIterator)(unsafe.Pointer(iterator)), C.RIL_WORD,
				&x1, &y1, &x2, &y2,
			) != 0 {
				if len(result) < maxAgentAnalysisBoxes {
					result = append(result, rawOCRBox{
						text: text, bounds: image.Rect(int(x1), int(y1), int(x2), int(y2)),
						confidence: float64(C.TessResultIteratorConfidence(iterator, C.RIL_WORD)) / 100,
						truncated:  wordTruncated,
					})
				} else {
					clear(text)
					if len(result) > 0 {
						result[len(result)-1].truncated = true
					}
				}
			} else {
				clear(text)
			}
		}
		if C.TessResultIteratorNext(iterator, C.RIL_WORD) == 0 {
			break
		}
	}
	return result, nil
}
