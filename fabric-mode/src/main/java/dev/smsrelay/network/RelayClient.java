package dev.smsrelay.network;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import dev.smsrelay.protocol.Channels;
import dev.smsrelay.protocol.RelayFrame;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import java.io.BufferedInputStream;
import java.io.BufferedOutputStream;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.ConcurrentLinkedQueue;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicLong;

/**
 * Owns the relay TCP socket. All blocking socket I/O happens on the two
 * dedicated relay threads; world/entity mutations are never performed here.
 */
public final class RelayClient implements AutoCloseable {
    private static final Logger LOGGER = LoggerFactory.getLogger("smsrelay/network");
    private static final int OUTBOUND_CAPACITY = 4096;

    private final String host;
    private final int port;
    private final int maxFrameBytes;
    private final BlockingQueue<RelayFrame> outbound = new LinkedBlockingQueue<>(OUTBOUND_CAPACITY);
    private final ConcurrentLinkedQueue<Inbound> inbound = new ConcurrentLinkedQueue<>();
    private final CountDownLatch socketReady = new CountDownLatch(1);
    private final AtomicLong sequence = new AtomicLong();
    private final AtomicBoolean running = new AtomicBoolean();
    private final AtomicBoolean registered = new AtomicBoolean();
    private final AtomicBoolean connectedOnce = new AtomicBoolean();
    private volatile Socket socket;
    private volatile Throwable lastFailure;
    private Thread writerThread;
    private Thread readerThread;

    public RelayClient(String host, int port, int maxFrameBytes) {
        this.host = host;
        this.port = port;
        this.maxFrameBytes = maxFrameBytes;
    }

    public void start(String uuid, String username, String gameMode, String world) {
        if (!running.compareAndSet(false, true)) {
            return;
        }
        JsonObject connect = new JsonObject();
        connect.addProperty("uuid", uuid);
        connect.addProperty("username", username);
        connect.addProperty("game_mode", gameMode);
        connect.addProperty("world", world);

        writerThread = new Thread(() -> writeLoop(connect), "SMS-Relay-Writer");
        writerThread.setDaemon(true);
        readerThread = new Thread(this::readLoop, "SMS-Relay-Reader");
        readerThread.setDaemon(true);
        writerThread.start();
        readerThread.start();
    }

    private void writeLoop(JsonObject connectPayload) {
        try {
            Socket newSocket = new Socket();
            newSocket.setTcpNoDelay(true);
            newSocket.setKeepAlive(true);
            newSocket.connect(new InetSocketAddress(host, port), 5000);
            socket = newSocket;
            OutputStream output = new BufferedOutputStream(newSocket.getOutputStream());
            write(output, new RelayFrame(Channels.CONNECT, nextSequence(), now(), jsonBytes(connectPayload)));
            connectedOnce.set(true);
            socketReady.countDown();
            LOGGER.info("Connected to relay {}:{}", host, port);

            while (running.get()) {
                RelayFrame frame = outbound.poll(500, TimeUnit.MILLISECONDS);
                if (frame != null) {
                    write(output, frame);
                }
            }
            RelayFrame remaining;
            while ((remaining = outbound.poll()) != null) {
                write(output, remaining);
            }
        } catch (Exception e) {
            if (running.get()) {
                LOGGER.warn("Relay writer stopped: {}", e.toString());
            }
            lastFailure = e;
        } finally {
            running.set(false);
            registered.set(false);
            socketReady.countDown();
            closeSocket();
        }
    }

    private void readLoop() {
        try {
            if (!socketReady.await(10, TimeUnit.SECONDS)) {
                return;
            }
            Socket current = socket;
            if (current == null) {
                return;
            }
            InputStream input = new BufferedInputStream(current.getInputStream());
            while (running.get()) {
                RelayFrame frame = RelayFrame.readFrom(input, maxFrameBytes);
                if (frame.channel().equals(Channels.PING)) {
                    sendRaw(Channels.PONG, frame.payload());
                    continue;
                }
                if (frame.channel().equals(Channels.CONNECT_ACK)) {
                    registered.set(true);
                }
                JsonObject json = parse(frame);
                if (json != null) {
                    inbound.add(new Inbound(frame.channel(), json, frame.timestamp()));
                }
            }
        } catch (Exception e) {
            if (running.get()) {
                LOGGER.warn("Relay reader stopped: {}", e.toString());
            }
            lastFailure = e;
        } finally {
            running.set(false);
            registered.set(false);
            socketReady.countDown();
            closeSocket();
        }
    }

    private static JsonObject parse(RelayFrame frame) {
        if (frame.payload().length == 0) {
            return new JsonObject();
        }
        try {
            return JsonParser.parseString(new String(frame.payload(), StandardCharsets.UTF_8)).getAsJsonObject();
        } catch (RuntimeException e) {
            LOGGER.warn("Ignoring malformed {} payload: {}", frame.channel(), e.getMessage());
            return null;
        }
    }

    private static byte[] jsonBytes(JsonObject object) {
        return object.toString().getBytes(StandardCharsets.UTF_8);
    }

    private static void write(OutputStream output, RelayFrame frame) throws Exception {
        frame.writeTo(output);
        output.flush();
    }

    public boolean send(String channel, JsonObject payload) {
        return sendRaw(channel, jsonBytes(payload));
    }

    public boolean sendRaw(String channel, byte[] payload) {
        if (!running.get()) {
            return false;
        }
        RelayFrame frame = new RelayFrame(channel, nextSequence(), now(), payload);
        if (!outbound.offer(frame)) {
            LOGGER.warn("Outbound relay queue full, dropping {}", channel);
            return false;
        }
        return true;
    }

    public Inbound pollInbound() {
        return inbound.poll();
    }

    public boolean isRunning() {
        return running.get();
    }

    public boolean isRegistered() {
        return registered.get();
    }

    public boolean wasEverConnected() {
        return connectedOnce.get();
    }

    public Throwable lastFailure() {
        return lastFailure;
    }

    private long nextSequence() {
        return sequence.incrementAndGet();
    }

    private static long now() {
        return System.currentTimeMillis();
    }

    public void stop(String reason) {
        if (!running.getAndSet(false)) {
            return;
        }
        JsonObject payload = new JsonObject();
        if (reason != null && !reason.isBlank()) {
            payload.addProperty("reason", reason);
        }
        if (socket != null) {
            RelayFrame frame = new RelayFrame(Channels.DISCONNECT, nextSequence(), now(), jsonBytes(payload));
            outbound.offer(frame);
        }
        registered.set(false);
        closeSocket();
        if (writerThread != null) {
            writerThread.interrupt();
        }
        if (readerThread != null) {
            readerThread.interrupt();
        }
    }

    private void closeSocket() {
        Socket current = socket;
        if (current != null) {
            try {
                current.close();
            } catch (Exception ignored) {
            }
        }
    }

    @Override
    public void close() {
        stop("client closed");
    }

    public record Inbound(String channel, JsonObject payload, long serverTimestamp) {
    }
}
