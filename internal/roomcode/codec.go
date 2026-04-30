package roomcode

import (
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/klauspost/compress/zstd"
	"github.com/vmihailenco/msgpack/v5"
)

const zstdLevel = 22
const maxCompressedPayloadBytes = 64 * 1024 // 64 KiB

var (
	encoderOnce sync.Once
	encoder     *zstd.Encoder
	encoderErr  error

	decoderOnce sync.Once
	decoder     *zstd.Decoder
	decoderErr  error

	errEmptyPayload  = errors.New("payload is empty")
	errNilOutput     = errors.New("output target is nil")
	errInvalidOutput = errors.New("output target must be a non-nil pointer")
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
		return "", fmt.Errorf("marshal msgpack payload: %w", err)
	}
	compressed := encoder.EncodeAll(raw, nil)
	return base64.RawURLEncoding.EncodeToString(compressed), nil
}

func decodeZstd(payload string, out any) error {
	if payload == "" {
		return errEmptyPayload
	}
	if err := validateDecodeOutput(out); err != nil {
		return err
	}

	decoderOnce.Do(func() {
		decoder, decoderErr = zstd.NewReader(nil)
	})
	if decoderErr != nil {
		return fmt.Errorf("init zstd decoder: %w", decoderErr)
	}
	compressed, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return fmt.Errorf("decode base64 payload: %w", err)
	}
	if len(compressed) > maxCompressedPayloadBytes {
		return fmt.Errorf("compressed payload too large: %d bytes (max %d)", len(compressed), maxCompressedPayloadBytes)
	}
	raw, err := decoder.DecodeAll(compressed, nil)
	if err != nil {
		return fmt.Errorf("decompress zstd payload: %w", err)
	}
	if err := msgpack.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unmarshal msgpack payload: %w", err)
	}
	return nil
}

func Encode(v any) (string, error) {
	return encodeZstd(v)
}

func Decode(payload string, out any) error {
	return decodeZstd(payload, out)
}

func validateDecodeOutput(out any) error {
	if out == nil {
		return errNilOutput
	}
	target := reflect.ValueOf(out)
	if target.Kind() != reflect.Ptr || target.IsNil() {
		return errInvalidOutput
	}
	return nil
}
