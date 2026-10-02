package web

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"

	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const firstFrameUploadBytes = 8 << 20

var firstFrameDecoders = make(chan struct{}, 2)

// Large source images remain accepted locally. Forward a bounded first frame,
// avoiding multipart overhead and metadata exceeding the upstream body limit.
func prepareFirstFrame(ctx context.Context, input provider.ImageInput) (provider.ImageInput, error) {
	if len(input.Data) <= firstFrameUploadBytes {
		return input, nil
	}
	select {
	case firstFrameDecoders <- struct{}{}:
		defer func() { <-firstFrameDecoders }()
	case <-ctx.Done():
		return provider.ImageInput{}, ctx.Err()
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(input.Data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 16_000_000 {
		return provider.ImageInput{}, fmt.Errorf("video image must fit 16 megapixels and 8192 pixels per side")
	}
	source, _, err := image.Decode(bytes.NewReader(input.Data))
	if err != nil {
		return provider.ImageInput{}, fmt.Errorf("invalid video image")
	}
	width, height := config.Width, config.Height
	if longest := max(width, height); longest > 1600 {
		width = max(1, width*1600/longest)
		height = max(1, height*1600/longest)
	}
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), image.White, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(canvas, canvas.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 88}); err != nil || encoded.Len() > firstFrameUploadBytes {
		return provider.ImageInput{}, fmt.Errorf("could not prepare bounded video image")
	}
	return provider.ImageInput{Filename: "first-frame.jpg", MIMEType: "image/jpeg", Data: encoded.Bytes()}, nil
}
