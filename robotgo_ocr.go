// Copyright (c) 2016-2025 AtomAI, All rights reserved.
//
// See the COPYRIGHT file at the top-level directory of this distribution and at
// https://github.com/go-vgo/robotgo/blob/master/LICENSE
//
// Licensed under the Apache License, Version 2.0 <LICENSE-APACHE or
// http://www.apache.org/licenses/LICENSE-2.0>
//
// This file may not be copied, modified, or distributed
// except according to those terms.

//go:build ocr && cgo
// +build ocr,cgo

package robotgo

import (
	"context"
	"time"

	"github.com/otiai10/gosseract/v2"
)

const defaultOCRTimeout = 2 * time.Minute

// GetText get the image text by tesseract ocr
func GetText(imgPath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultOCRTimeout)
	defer cancel()
	return GetTextContext(ctx, imgPath, args...)
}

// GetTextContext extracts image text with the in-process Gosseract backend.
// The native Tesseract call itself is synchronous, but cancellation is checked
// before it starts and before its result is returned.
func GetTextContext(ctx context.Context, imgPath string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var lang = "eng"

	if len(args) > 0 {
		lang = args[0]
		if lang == "zh" {
			lang = "chi_sim"
		}
	}

	client := gosseract.NewClient()
	defer client.Close()

	client.SetImage(imgPath)
	client.SetLanguage(lang)
	text, err := client.Text()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	return text, err
}
