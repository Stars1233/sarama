//go:build !functional

package sarama

import (
	"testing"
	"time"
)

var (
	emptyMessage = []byte{
		167, 236, 104, 3, // CRC
		0x00,                   // magic version byte
		0x00,                   // attribute flags
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0xFF, 0xFF, 0xFF, 0xFF,
	} // value

	emptyV1Message = []byte{
		204, 47, 121, 217, // CRC
		0x01,                                           // magic version byte
		0x00,                                           // attribute flags
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // timestamp
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0xFF, 0xFF, 0xFF, 0xFF,
	} // value

	emptyV2Message = []byte{
		167, 236, 104, 3, // CRC
		0x02,                   // magic version byte
		0x00,                   // attribute flags
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0xFF, 0xFF, 0xFF, 0xFF,
	} // value

	emptyGzipMessage = []byte{
		196, 46, 92, 177, // CRC
		0x00,                   // magic version byte
		0x01,                   // attribute flags
		0xFF, 0xFF, 0xFF, 0xFF, // key
		// value
		0x00, 0x00, 0x00, 0x14,
		0x1f, 0x8b,
		0x08,
		0, 0, 9, 110, 136, 0, 255, 3, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}

	emptyLZ4Message = []byte{
		132, 219, 238, 101, // CRC
		0x01,                          // version byte
		0x03,                          // attribute flags: lz4
		0, 0, 1, 88, 141, 205, 89, 56, // timestamp
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0x00, 0x00, 0x00, 0x0f, // len
		0x04, 0x22, 0x4D, 0x18, // LZ4 magic number
		100,                  // LZ4 flags: version 01, block independent, content checksum
		112, 185, 0, 0, 0, 0, // LZ4 data
		5, 93, 204, 2, // LZ4 checksum
	}

	emptyZSTDMessage = []byte{
		180, 172, 84, 179, // CRC
		0x01,                          // version byte
		0x04,                          // attribute flags: zstd
		0, 0, 1, 88, 141, 205, 89, 56, // timestamp
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0x00, 0x00, 0x00, 0x09, // len
		// ZSTD data
		0x28, 0xb5, 0x2f, 0xfd, 0x20, 0x00, 0x01, 0x00, 0x00,
	}

	emptyBulkSnappyMessage = []byte{
		180, 47, 53, 209, // CRC
		0x00,                   // magic version byte
		0x02,                   // attribute flags
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0, 0, 0, 42,
		130, 83, 78, 65, 80, 80, 89, 0, // SNAPPY magic
		0, 0, 0, 1, // min version
		0, 0, 0, 1, // default version
		0, 0, 0, 22, 52, 0, 0, 25, 1, 16, 14, 227, 138, 104, 118, 25, 15, 13, 1, 8, 1, 0, 0, 62, 26, 0,
	}

	emptyBulkGzipMessage = []byte{
		139, 160, 63, 141, // CRC
		0x00,                   // magic version byte
		0x01,                   // attribute flags
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0x00, 0x00, 0x00, 0x27, // len
		0x1f, 0x8b, // Gzip Magic
		0x08, // deflate compressed
		0, 0, 0, 0, 0, 0, 0, 99, 96, 128, 3, 190, 202, 112, 143, 7, 12, 12, 255, 129, 0, 33, 200, 192, 136, 41, 3, 0, 199, 226, 155, 70, 52, 0, 0, 0,
	}

	emptyBulkLZ4Message = []byte{
		246, 12, 188, 129, // CRC
		0x01,                                  // Version
		0x03,                                  // attribute flags (LZ4)
		255, 255, 249, 209, 212, 181, 73, 201, // timestamp
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0x00, 0x00, 0x00, 0x47, // len
		0x04, 0x22, 0x4D, 0x18, // magic number lz4
		100, // lz4 flags 01100100
		// version: 01, block indep: 1, block checksum: 0, content size: 0, content checksum: 1, reserved: 00
		112, 185, 52, 0, 0, 128, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 14, 121, 87, 72, 224, 0, 0, 255, 255, 255, 255, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 14, 121, 87, 72, 224, 0, 0, 255, 255, 255, 255, 0, 0, 0, 0, 0, 0, 0, 0,
		71, 129, 23, 111, // LZ4 checksum
	}

	emptyBulkZSTDMessage = []byte{
		203, 151, 133, 28, // CRC
		0x01,                                  // Version
		0x04,                                  // attribute flags (ZSTD)
		255, 255, 249, 209, 212, 181, 73, 201, // timestamp
		0xFF, 0xFF, 0xFF, 0xFF, // key
		0x00, 0x00, 0x00, 0x26, // len
		// ZSTD data
		0x28, 0xb5, 0x2f, 0xfd, 0x24, 0x34, 0xcd, 0x0, 0x0, 0x78, 0x0, 0x0, 0xe, 0x79, 0x57, 0x48, 0xe0, 0x0, 0x0, 0xff, 0xff, 0xff, 0xff, 0x0, 0x1, 0x3, 0x0, 0x3d, 0xbd, 0x0, 0x3b, 0x15, 0x0, 0xb, 0xd2, 0x34, 0xc1, 0x78,
	}
)

func TestMessageEncoding(t *testing.T) {
	message := Message{}
	testEncodable(t, "empty", &message, emptyMessage)

	message.Value = []byte{}
	message.Codec = CompressionGZIP
	testEncodable(t, "empty gzip", &message, emptyGzipMessage)

	message.Value = []byte{}
	message.Codec = CompressionLZ4
	message.Timestamp = time.Unix(1479847795, 0)
	message.Version = 1
	testEncodable(t, "empty lz4", &message, emptyLZ4Message)

	message.Value = []byte{}
	message.Codec = CompressionZSTD
	message.Timestamp = time.Unix(1479847795, 0)
	message.Version = 1
	testEncodable(t, "empty zstd", &message, emptyZSTDMessage)
}

func TestMessageDecoding(t *testing.T) {
	message := Message{}
	testDecodable(t, "empty", &message, emptyMessage)
	if message.Codec != CompressionNone {
		t.Error("Decoding produced compression codec where there was none.")
	}
	if message.Key != nil {
		t.Error("Decoding produced key where there was none.")
	}
	if message.Value != nil {
		t.Error("Decoding produced value where there was none.")
	}
	if message.Set != nil {
		t.Error("Decoding produced set where there was none.")
	}

	testDecodable(t, "empty gzip", &message, emptyGzipMessage)
	if message.Codec != CompressionGZIP {
		t.Error("Decoding produced incorrect compression codec (was gzip).")
	}
	if message.Key != nil {
		t.Error("Decoding produced key where there was none.")
	}
	if message.Value == nil || len(message.Value) != 0 {
		t.Error("Decoding produced nil or content-ful value where there was an empty array.")
	}
}

func TestMessageDecodingBulkSnappy(t *testing.T) {
	message := Message{}
	testDecodable(t, "bulk snappy", &message, emptyBulkSnappyMessage)
	if message.Codec != CompressionSnappy {
		t.Errorf("Decoding produced codec %d, but expected %d.", message.Codec, CompressionSnappy)
	}
	if message.Key != nil {
		t.Errorf("Decoding produced key %+v, but none was expected.", message.Key)
	}
	if message.Set == nil {
		t.Error("Decoding produced no set, but one was expected.")
	} else if len(message.Set.Messages) != 2 {
		t.Errorf("Decoding produced a set with %d messages, but 2 were expected.", len(message.Set.Messages))
	}
}

func TestMessageDecodingBulkGzip(t *testing.T) {
	message := Message{}
	testDecodable(t, "bulk gzip", &message, emptyBulkGzipMessage)
	if message.Codec != CompressionGZIP {
		t.Errorf("Decoding produced codec %d, but expected %d.", message.Codec, CompressionGZIP)
	}
	if message.Key != nil {
		t.Errorf("Decoding produced key %+v, but none was expected.", message.Key)
	}
	if message.Set == nil {
		t.Error("Decoding produced no set, but one was expected.")
	} else if len(message.Set.Messages) != 2 {
		t.Errorf("Decoding produced a set with %d messages, but 2 were expected.", len(message.Set.Messages))
	}
}

func TestMessageDecodingBulkLZ4(t *testing.T) {
	message := Message{}
	testDecodable(t, "bulk lz4", &message, emptyBulkLZ4Message)
	if message.Codec != CompressionLZ4 {
		t.Errorf("Decoding produced codec %d, but expected %d.", message.Codec, CompressionLZ4)
	}
	if message.Key != nil {
		t.Errorf("Decoding produced key %+v, but none was expected.", message.Key)
	}
	if message.Set == nil {
		t.Error("Decoding produced no set, but one was expected.")
	} else if len(message.Set.Messages) != 2 {
		t.Errorf("Decoding produced a set with %d messages, but 2 were expected.", len(message.Set.Messages))
	}
}

func TestMessageDecodingBulkZSTD(t *testing.T) {
	message := Message{}
	testDecodable(t, "bulk zstd", &message, emptyBulkZSTDMessage)
	if message.Codec != CompressionZSTD {
		t.Errorf("Decoding produced codec %d, but expected %d.", message.Codec, CompressionZSTD)
	}
	if message.Key != nil {
		t.Errorf("Decoding produced key %+v, but none was expected.", message.Key)
	}
	if message.Set == nil {
		t.Error("Decoding produced no set, but one was expected.")
	} else if len(message.Set.Messages) != 2 {
		t.Errorf("Decoding produced a set with %d messages, but 2 were expected.", len(message.Set.Messages))
	}
}

func TestMessageDecodingVersion1(t *testing.T) {
	message := Message{Version: 1}
	testDecodable(t, "decoding empty v1 message", &message, emptyV1Message)
}

func TestMessageDecodingUnknownVersions(t *testing.T) {
	message := Message{Version: 2}
	err := decode(emptyV2Message, &message, nil)
	if err == nil {
		t.Error("Decoding did not produce an error for an unknown magic byte")
	}
	if err.Error() != "kafka: error decoding packet: unknown magic byte (2)" {
		t.Error("Decoding an unknown magic byte produced an unknown error ", err)
	}
}

func TestCompressionCodecUnmarshal(t *testing.T) {
	cases := []struct {
		Input         string
		Expected      CompressionCodec
		ExpectedError bool
	}{
		{"none", CompressionNone, false},
		{"zstd", CompressionZSTD, false},
		{"gzip", CompressionGZIP, false},
		{"unknown", CompressionNone, true},
	}
	for _, c := range cases {
		var cc CompressionCodec
		err := cc.UnmarshalText([]byte(c.Input))
		if err != nil && !c.ExpectedError {
			t.Errorf("UnmarshalText(%q) error:\n%+v", c.Input, err)
			continue
		}
		if err == nil && c.ExpectedError {
			t.Errorf("UnmarshalText(%q) got %v but expected error", c.Input, cc)
			continue
		}
		if cc != c.Expected {
			t.Errorf("UnmarshalText(%q) got %v but expected %v", c.Input, cc, c.Expected)
			continue
		}
	}
}
