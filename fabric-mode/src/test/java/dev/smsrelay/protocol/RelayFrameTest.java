package dev.smsrelay.protocol;

import org.junit.jupiter.api.Test;

import java.io.ByteArrayInputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.HexFormat;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

class RelayFrameTest {
    @Test
    void encodesExactlyLikeGoImplementation() {
        RelayFrame frame = new RelayFrame(
                "sms:ping",
                1L,
                1_234_567_890_123L,
                "{\"time\":1}".getBytes(StandardCharsets.UTF_8));

        String expected = "534d533100000024000800000000000000010000011f71fb04cb"
                + "736d733a70696e677b2274696d65223a317d";
        assertEquals(expected, HexFormat.of().formatHex(frame.encode()));
        assertEquals(44, frame.encode().length);
    }

    @Test
    void roundTripsThroughStream() throws IOException {
        byte[] payload = new byte[]{0, 1, 2, 3, (byte) 0xff};
        RelayFrame original = new RelayFrame("sms:chunk_response", Long.MAX_VALUE, 42L, payload);
        RelayFrame decoded = RelayFrame.readFrom(new ByteArrayInputStream(original.encode()), 2048);
        assertEquals(original, decoded);
        assertArrayEquals(payload, decoded.payload());
    }

    @Test
    void rejectsBadMagic() {
        byte[] bytes = new RelayFrame("sms:ping", 1, 2, new byte[0]).encode();
        bytes[0] = 0;
        assertThrows(IOException.class,
                () -> RelayFrame.readFrom(new ByteArrayInputStream(bytes), 2048));
    }

    @Test
    void rejectsOversizedFrame() {
        byte[] bytes = new RelayFrame("sms:ping", 1, 2, new byte[64]).encode();
        assertThrows(IOException.class,
                () -> RelayFrame.readFrom(new ByteArrayInputStream(bytes), 32));
    }

    @Test
    void rejectsInvalidChannelLength() {
        assertThrows(IllegalArgumentException.class,
                () -> new RelayFrame("", 0, 0, new byte[0]));
        assertThrows(IllegalArgumentException.class,
                () -> new RelayFrame("x".repeat(65), 0, 0, new byte[0]));
    }
}
