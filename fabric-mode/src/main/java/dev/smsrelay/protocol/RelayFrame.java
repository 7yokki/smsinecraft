package dev.smsrelay.protocol;

import java.io.EOFException;
import java.io.IOException;
import java.io.InputStream;
import java.io.OutputStream;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.Arrays;

/**
 * Exact Java counterpart of the Go relay wire frame.
 *
 * <pre>
 * magic(4) 0x534D5331 | body_len(4) | channel_len(2) | sequence(8) |
 * timestamp_ms(8) | channel | payload
 * </pre>
 *
 * All integers are big endian. body_len covers everything after the 8 byte
 * prefix: channel_len + sequence + timestamp + channel + payload.
 */
public final class RelayFrame {
    public static final int MAGIC = 0x534D5331;
    public static final int MAX_CHANNEL_LEN = 64;
    private static final int FIXED_BODY_SIZE = 18;

    private final String channel;
    private final long sequence;
    private final long timestamp;
    private final byte[] payload;

    public RelayFrame(String channel, long sequence, long timestamp, byte[] payload) {
        if (channel == null || channel.isEmpty() || channel.length() > MAX_CHANNEL_LEN) {
            throw new IllegalArgumentException("invalid channel length");
        }
        this.channel = channel;
        this.sequence = sequence;
        this.timestamp = timestamp;
        this.payload = payload == null ? new byte[0] : payload.clone();
    }

    public String channel() {
        return channel;
    }

    public long sequence() {
        return sequence;
    }

    public long timestamp() {
        return timestamp;
    }

    public byte[] payload() {
        return payload.clone();
    }

    public byte[] encode() {
        byte[] channelBytes = channel.getBytes(StandardCharsets.UTF_8);
        if (channelBytes.length < 1 || channelBytes.length > MAX_CHANNEL_LEN) {
            throw new IllegalArgumentException("invalid channel byte length");
        }
        int bodyLen = FIXED_BODY_SIZE + channelBytes.length + payload.length;
        ByteBuffer buffer = ByteBuffer.allocate(8 + bodyLen);
        buffer.putInt(MAGIC);
        buffer.putInt(bodyLen);
        buffer.putShort((short) channelBytes.length);
        buffer.putLong(sequence);
        buffer.putLong(timestamp);
        buffer.put(channelBytes);
        buffer.put(payload);
        return buffer.array();
    }

    public void writeTo(OutputStream output) throws IOException {
        output.write(encode());
    }

    public static RelayFrame readFrom(InputStream input, int maxFrameBytes) throws IOException {
        byte[] prefix = readFully(input, 8);
        ByteBuffer header = ByteBuffer.wrap(prefix);
        if (header.getInt() != MAGIC) {
            throw new IOException("protocol: bad magic");
        }
        long bodyLenLong = Integer.toUnsignedLong(header.getInt());
        if (bodyLenLong > maxFrameBytes || bodyLenLong + 8 > maxFrameBytes) {
            throw new IOException("protocol: frame exceeds limit");
        }
        if (bodyLenLong < FIXED_BODY_SIZE + 1) {
            throw new IOException("protocol: invalid channel");
        }

        int bodyLen = (int) bodyLenLong;
        ByteBuffer body = ByteBuffer.wrap(readFully(input, bodyLen));
        int channelLen = Short.toUnsignedInt(body.getShort());
        if (channelLen < 1 || channelLen > MAX_CHANNEL_LEN) {
            throw new IOException("protocol: invalid channel");
        }
        if (FIXED_BODY_SIZE + channelLen > bodyLen) {
            throw new IOException("protocol: invalid channel");
        }

        long sequence = body.getLong();
        long timestamp = body.getLong();
        byte[] channelBytes = new byte[channelLen];
        body.get(channelBytes);
        byte[] payload = new byte[body.remaining()];
        body.get(payload);
        return new RelayFrame(new String(channelBytes, StandardCharsets.UTF_8), sequence, timestamp, payload);
    }

    private static byte[] readFully(InputStream input, int length) throws IOException {
        byte[] data = new byte[length];
        int offset = 0;
        while (offset < length) {
            int read = input.read(data, offset, length - offset);
            if (read < 0) {
                throw new EOFException("unexpected end of stream");
            }
            if (read == 0) {
                continue;
            }
            offset += read;
        }
        return data;
    }

    @Override
    public String toString() {
        return "RelayFrame{channel='" + channel + "', sequence=" + sequence
                + ", timestamp=" + timestamp + ", payload=" + payload.length + " bytes}";
    }

    @Override
    public boolean equals(Object other) {
        if (this == other) {
            return true;
        }
        if (!(other instanceof RelayFrame frame)) {
            return false;
        }
        return sequence == frame.sequence
                && timestamp == frame.timestamp
                && channel.equals(frame.channel)
                && Arrays.equals(payload, frame.payload);
    }

    @Override
    public int hashCode() {
        int result = channel.hashCode();
        result = 31 * result + Long.hashCode(sequence);
        result = 31 * result + Long.hashCode(timestamp);
        result = 31 * result + Arrays.hashCode(payload);
        return result;
    }
}
