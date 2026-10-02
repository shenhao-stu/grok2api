package web

import (
	"bytes"
	"context"
	"github.com/chenyme/grok2api/backend/internal/infra/provider"
	"image"
	"image/png"
	"testing"
)

func TestLargeFirstFrameStripsPaddingWithoutChangingSource(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 4, 3))); err != nil {
		t.Fatal(err)
	}
	source := append(data.Bytes(), make([]byte, firstFrameUploadBytes)...)
	result, err := prepareFirstFrame(context.Background(), provider.ImageInput{Filename: "large.png", MIMEType: "image/png", Data: source})
	if err != nil || result.MIMEType != "image/jpeg" || len(result.Data) > firstFrameUploadBytes {
		t.Fatal(result.MIMEType, err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil || config.Width != 4 || config.Height != 3 || !bytes.Equal(source[:data.Len()], data.Bytes()) {
		t.Fatal("source or dimensions changed", err)
	}
}

func TestCancelledLargeFirstFrameDoesNotDecode(t *testing.T) {
	for i := 0; i < cap(firstFrameDecoders); i++ {
		firstFrameDecoders <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(firstFrameDecoders); i++ {
			<-firstFrameDecoders
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := prepareFirstFrame(ctx, provider.ImageInput{Data: make([]byte, firstFrameUploadBytes+1)}); err != context.Canceled {
		t.Fatal(err)
	}
}
