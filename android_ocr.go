package gnosis

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unicode/utf16"

	ocr "github.com/Allod-Solutions/go-ocr"
)

type androidOCR struct {
	engine *ocr.Engine
}

func (d *AndroidDevice) RecognizeScreenText(screenshot []byte) ([]TextRegion, error) {
	if d.ocr.engine == nil {
		runtimeFile := "onnxruntime.dll"
		switch runtime.GOOS {
		case "linux":
			runtimeFile = "libonnxruntime.so"
		case "darwin":
			runtimeFile = "libonnxruntime.dylib"
		}
		detector := filepath.Join(d.assetsDir, "ch_PP-OCRv5_mobile_det.onnx")
		recognizer := filepath.Join(d.assetsDir, "ch_PP-OCRv5_rec_mobile_infer.onnx")
		dictionary := filepath.Join(d.assetsDir, "ppocrv5_dict.txt")
		library := filepath.Join(d.assetsDir, runtimeFile)
		for _, path := range []string{library, detector, recognizer, dictionary} {
			if _, err := os.Stat(path); err != nil {
				return nil, fmt.Errorf("screen text recognition is unavailable: %w", err)
			}
		}
		charset, err := preparePPOCRCharset(dictionary)
		if err != nil {
			return nil, fmt.Errorf("screen text recognition is unavailable: %w", err)
		}
		defer os.Remove(charset)
		engine, err := ocr.New(ocr.Options{
			RuntimeLibPath: library,
			DetModelPath:   ortModelPath(detector),
			RecModelPath:   ortModelPath(recognizer),
			RecCharsetPath: charset,
			BlankIndex:     0,
		})
		if err != nil {
			return nil, fmt.Errorf("screen text recognition is unavailable: %w", err)
		}
		d.ocr.engine = engine
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lines, err := d.ocr.engine.RecognizeLines(ctx, screenshot)
	if err != nil {
		return nil, fmt.Errorf("recognize screen text: %w", err)
	}
	regions := make([]TextRegion, 0, len(lines))
	for _, line := range lines {
		corners := line.Box.Corners
		minX, maxX := corners[0].X, corners[0].X
		minY, maxY := corners[0].Y, corners[0].Y
		for _, p := range corners[1:] {
			minX, maxX = min(minX, p.X), max(maxX, p.X)
			minY, maxY = min(minY, p.Y), max(maxY, p.Y)
		}
		regions = append(regions, TextRegion{
			Text: line.Text,
			Bounds: image.Rect(
				int(math.Floor(float64(minX))), int(math.Floor(float64(minY))),
				int(math.Ceil(float64(maxX))), int(math.Ceil(float64(maxY))),
			),
		})
	}
	return regions, nil
}

// PP-OCRv5's Chinese mobile CTC model prepends a blank and appends a
// space to the published dictionary. go-ocr expects every output class.
func preparePPOCRCharset(path string) (string, error) {
	dictionary, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	dictionary = bytes.TrimRight(dictionary, "\r\n")
	if len(dictionary) == 0 || bytes.Count(dictionary, []byte{'\n'}) != 18382 {
		return "", errors.New("expected the 18,383-entry PP-OCRv5 Chinese dictionary")
	}
	file, err := os.CreateTemp("", "gnosis-ppocrv5-*.txt")
	if err != nil {
		return "", err
	}
	name := file.Name()
	var contents bytes.Buffer
	contents.WriteByte('\n')
	contents.Write(dictionary)
	contents.WriteString("\n \n")
	_, writeErr := file.Write(contents.Bytes())
	if err := errors.Join(writeErr, file.Close()); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

// The pinned go-ocr version forwards model path bytes to ORT's CreateSession.
// On Windows that C API takes a NUL-terminated UTF-16 path.
func ortModelPath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	units := utf16.Encode([]rune(path))
	encoded := make([]byte, 0, 2*(len(units)+1))
	for _, unit := range units {
		encoded = binary.LittleEndian.AppendUint16(encoded, unit)
	}
	return string(append(encoded, 0, 0))
}

func (o *androidOCR) close() {
	if o.engine != nil {
		_ = o.engine.Close()
		o.engine = nil
	}
}
