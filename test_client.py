#!/usr/bin/env python3
"""
sms-server için basit test client'ı.
Wire format: magic(4) + body_len(4) + channel_len(2) + sequence(8) +
timestamp(8) + channel(C) + payload(P), hepsi big-endian.
"""
import json
import socket
import struct
import threading
import time
import uuid as uuidlib

MAGIC = 0x534D5331
HOST = "127.0.0.1"
PORT = 25566


class Conn:
    def __init__(self, name):
        self.name = name
        self.sock = socket.create_connection((HOST, PORT), timeout=5)
        self.sock.settimeout(2.0)
        self.seq = 0
        self.lock = threading.Lock()
        self.incoming = []
        self.stop = False
        self.reader = threading.Thread(target=self._read_loop, daemon=True)
        self.reader.start()

    def send(self, channel, payload_obj):
        payload = json.dumps(payload_obj).encode("utf-8")
        channel_b = channel.encode("utf-8")
        self.seq += 1
        body = (
            struct.pack(">H", len(channel_b))
            + struct.pack(">Q", self.seq)
            + struct.pack(">Q", int(time.time() * 1000))
            + channel_b
            + payload
        )
        frame = struct.pack(">II", MAGIC, len(body)) + body
        with self.lock:
            self.sock.sendall(frame)

    def _read_loop(self):
        buf = b""
        while not self.stop:
            try:
                chunk = self.sock.recv(65536)
            except socket.timeout:
                continue
            except OSError:
                return
            if not chunk:
                return
            buf += chunk
            while True:
                if len(buf) < 8:
                    break
                magic, body_len = struct.unpack(">II", buf[:8])
                if magic != MAGIC:
                    print(f"[{self.name}] BAD MAGIC, dropping buffer")
                    buf = b""
                    break
                total = 8 + body_len
                if len(buf) < total:
                    break
                body = buf[8:total]
                buf = buf[total:]
                channel_len = struct.unpack(">H", body[0:2])[0]
                seq = struct.unpack(">Q", body[2:10])[0]
                ts = struct.unpack(">Q", body[10:18])[0]
                channel = body[18 : 18 + channel_len].decode("utf-8")
                payload = body[18 + channel_len :]
                try:
                    obj = json.loads(payload) if payload else {}
                except Exception:
                    obj = {"_raw_len": len(payload)}
                print(f"[{self.name}] <- {channel} seq={seq} {obj}")
                self.incoming.append((channel, obj))

    def close(self):
        self.stop = True
        try:
            self.sock.close()
        except OSError:
            pass


def connect(name, game_mode="survival", world="overworld"):
    c = Conn(name)
    c.send(
        "sms:connect",
        {
            "uuid": uuidlib.uuid4().hex,
            "username": name,
            "game_mode": game_mode,
            "world": world,
        },
    )
    time.sleep(0.3)
    return c


def scenario_basic_connect():
    print("\n=== SENARYO 1: temel bağlanma + connect_ack/world_state/player_list ===")
    a = connect("Ahmet")
    time.sleep(0.5)
    a.close()


def scenario_chunk_authority_handoff():
    print("\n=== SENARYO 2: chunk authority + request/response handoff ===")
    a = connect("Ahmet")
    b = connect("Mehmet")

    # Ahmet chunk 10'u üretir
    a.send("sms:chunk_produced", {"chunk_id": "overworld:10,0", "summary": "AAAA"})
    time.sleep(0.3)

    # Mehmet aynı chunk'ı tekrar üretmeye çalışırsa conflict almalı (Ahmet online)
    b.send("sms:chunk_produced", {"chunk_id": "overworld:10,0", "summary": "BBBB"})
    time.sleep(0.3)

    # Mehmet chunk 10'u Ahmet'ten talep eder
    b.send("sms:chunk_request", {"chunk_id": "overworld:10,0"})
    time.sleep(0.3)

    # Ahmet'in client'ı isteği alıp cevap vermeli (biz elle simüle ediyoruz)
    a.send(
        "sms:chunk_response",
        {"chunk_id": "overworld:10,0", "data": "ZmFrZS1jaHVuay1kYXRh"},
    )
    time.sleep(0.3)

    print("--- Ahmet ayrılıyor, chunk authority offline olmalı ---")
    a.close()
    time.sleep(1.5)  # sunucunun disconnect'i işleyip state'i güncellemesi için bolca pay

    # Mehmet şimdi aynı chunk'ı üretirse devralabilmeli (offline authority -> ChunkTakenOver)
    print("--- Mehmet chunk 10'u tekrar üretmeyi dener (devralma beklentisi) ---")
    b.send("sms:chunk_produced", {"chunk_id": "overworld:10,0", "summary": "CCCC"})
    time.sleep(0.5)
    b.close()
    time.sleep(0.3)  # Mehmet'in kendi close'u sonraki senaryoyu etkilemesin


def scenario_speed_hack():
    print("\n=== SENARYO 3: hız hilesi tespiti (survival'da imkansız sıçrama) ===")
    a = connect("HileciAhmet", game_mode="survival")
    a.send("sms:position", {"x": 0, "y": 64, "z": 0, "yaw": 0, "pitch": 0, "on_ground": True})
    time.sleep(0.2)
    # Survival max hız ~24 blok/s + 1 tolerans. 0.2s içinde 500 blok -> çok yüksek hız.
    for i in range(4):
        a.send(
            "sms:position",
            {"x": 500 * (i + 1), "y": 64, "z": 0, "yaw": 0, "pitch": 0, "on_ground": True},
        )
        time.sleep(0.15)
    time.sleep(0.5)
    print("--- Beklenti: birkaç ihlalden sonra sms:correction paketi gelmeli ---")
    a.close()


def scenario_reach_violation():
    print("\n=== SENARYO 4: reach (etkileşim mesafesi) ihlali ===")
    a = connect("UzaktanKiranAhmet")
    a.send("sms:position", {"x": 0, "y": 64, "z": 0, "yaw": 0, "pitch": 0, "on_ground": True})
    time.sleep(0.2)
    # max-reach varsayılan 6 blok; 100 blok uzaktaki bloğu kırmayı dene
    a.send(
        "sms:block_change",
        {"world": "overworld", "x": 100, "y": 64, "z": 0, "block_id": "minecraft:stone"},
    )
    time.sleep(0.3)
    print("--- Beklenti: sms:error code=reach gelmeli ---")
    a.close()


def scenario_attack_cooldown():
    print("\n=== SENARYO 5: saldırı cooldown ihlali ===")
    a = connect("HizliVurucuAhmet")
    a.send("sms:position", {"x": 0, "y": 64, "z": 0, "yaw": 0, "pitch": 0, "on_ground": True})
    time.sleep(0.2)
    for i in range(3):
        a.send("sms:attack", {"x": 1, "y": 64, "z": 0})
        time.sleep(0.05)  # cooldown 300ms, 50ms arayla spam
    time.sleep(0.3)
    print("--- Beklenti: ilk saldırı kabul, sonrakiler sms:error code=attack_cooldown ---")
    a.close()


def scenario_rate_limit():
    print("\n=== SENARYO 6: paket rate limit ihlali ===")
    a = connect("SpamciAhmet")
    for i in range(400):
        a.send("sms:position", {"x": 0, "y": 64, "z": 0, "yaw": 0, "pitch": 0, "on_ground": True})
    time.sleep(0.5)
    print("--- Beklenti: sms:error code=rate_limited en az bir kez gelmeli ---")
    a.close()


if __name__ == "__main__":
    scenario_basic_connect()
    time.sleep(0.5)
    scenario_chunk_authority_handoff()
    time.sleep(0.5)
    scenario_speed_hack()
    time.sleep(0.5)
    scenario_reach_violation()
    time.sleep(0.5)
    scenario_attack_cooldown()
    time.sleep(0.5)
    scenario_rate_limit()
    print("\nTüm senaryolar tamamlandı.")
