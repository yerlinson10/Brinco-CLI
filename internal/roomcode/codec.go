package roomcode

import (
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/vmihailenco/msgpack/v5"
)

const zstdLevel = 22

var (
	encoderOnce sync.Once
	encoder     *zstd.Encoder
	encoderErr  error

	decoderOnce sync.Once
	decoder     *zstd.Decoder
	decoderErr  error
)

func encodeZstd(v any) (string, error) {
	encoderOnce.Do(func() {
		encoder, encoderErr = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.EncoderLevelFromZstd(zstdLevel)))
	})
	if encoderErr != nil {
		return "", fmt.Errorf("init zstd encoder: %w", encoderErr)
	}
	raw, err := msgpack.Marshal(v)
	if err != nil {
		return "", err
	}
	compressed := encoder.EncodeAll(raw, nil)
	return base64.RawURLEncoding.EncodeToString(compressed), nil
}

func decodeZstd(payload string, out any) error {
	decoderOnce.Do(func() {
		decoder, decoderErr = zstd.NewReader(nil)
	})
	if decoderErr != nil {
		return fmt.Errorf("init zstd decoder: %w", decoderErr)
	}
	compressed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return err
	}
	raw, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return err
	}
	return msgpack.Unmarshal(raw, out)
}

func Encode(v any) (string, error) {
	return encodeZstd(v)
}

func Decode(payload string, out any) error {
	return decodeZstd(payload, out)
}
