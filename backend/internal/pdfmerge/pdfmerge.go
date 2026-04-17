package pdfmerge

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"

	"github.com/jung-kurt/gofpdf/v2"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"product-collection-form/backend/internal/httpapi/limits"
)

const (
	pageWMM       = 210.0
	pageHMM       = 297.0
	marginMM      = 10.0
	largePixelCap = 8000
	maxSideAfter  = 2000
)

type Service struct {
	limits *limits.Manager
}

func New(limiter *limits.Manager) *Service {
	return &Service{limits: limiter}
}

func (s *Service) Build(ctx context.Context, imagePaths []string, w io.Writer) (int, error) {
	pdf := gofpdf.NewCustom(&gofpdf.InitType{
		UnitStr: "mm",
		Size:    gofpdf.SizeType{Wd: pageWMM, Ht: pageHMM},
	})
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)

	for i, path := range imagePaths {
		if err := ctx.Err(); err != nil {
			return 0, err
		}

		data, widthPx, heightPx, err := s.prepareJPEG(ctx, path)
		if err != nil {
			return 0, err
		}

		name := fmt.Sprintf("page-%d", i+1)
		pdf.AddPageFormat("P", gofpdf.SizeType{Wd: pageWMM, Ht: pageHMM})
		opts := gofpdf.ImageOptions{ImageType: "JPG", ReadDpi: false}
		pdf.RegisterImageOptionsReader(name, opts, bytes.NewReader(data))

		scale := min((pageWMM-2*marginMM)/widthPx, (pageHMM-2*marginMM)/heightPx)
		renderW := widthPx * scale
		renderH := heightPx * scale
		x := (pageWMM - renderW) / 2
		y := (pageHMM - renderH) / 2
		pdf.ImageOptions(name, x, y, renderW, renderH, false, opts, 0, "")
	}

	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := pdf.Output(w); err != nil {
		return 0, fmt.Errorf("write pdf: %w", err)
	}
	return len(imagePaths), nil
}

func (s *Service) prepareJPEG(ctx context.Context, path string) ([]byte, float64, float64, error) {
	if s.limits != nil {
		release, err := s.limits.ImageDecode.Acquire(ctx)
		if err != nil {
			return nil, 0, 0, err
		}
		defer release()
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("open image %q: %w", path, err)
	}
	defer file.Close()

	cfg, _, err := image.DecodeConfig(file)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("decode image config %q: %w", path, err)
	}

	if _, err := file.Seek(0, 0); err != nil {
		return nil, 0, 0, fmt.Errorf("rewind image %q: %w", path, err)
	}

	if cfg.Width > largePixelCap || cfg.Height > largePixelCap {
		img, _, err := image.Decode(file)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("decode large image %q: %w", path, err)
		}
		resized, width, height := resizeDown(img, maxSideAfter)
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: 85}); err != nil {
			return nil, 0, 0, fmt.Errorf("encode resized image %q: %w", path, err)
		}
		img = nil
		resized = nil
		return buf.Bytes(), width, height, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read image %q: %w", path, err)
	}
	return data, float64(cfg.Width), float64(cfg.Height), nil
}

func resizeDown(src image.Image, maxSide int) (image.Image, float64, float64) {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	longest := max(width, height)
	if longest <= maxSide {
		return src, float64(width), float64(height)
	}

	scale := float64(maxSide) / float64(longest)
	dstW := max(1, int(float64(width)*scale))
	dstH := max(1, int(float64(height)*scale))
	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, draw.Over, nil)
	return dst, float64(dstW), float64(dstH)
}
