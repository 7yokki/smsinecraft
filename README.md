# sms-server

Minecraft benzeri bir oyun için **oyun motoru çalıştırmayan**, yalnızca istemciler
arasında koordinasyon ve paket yönlendirme yapan TCP relay sunucusu.

Sunucu chunk üretmez, blok/fizik/mob AI simüle etmez ve dünya geometrisi
tutmaz. Dünyanın asıl sahibi istemcilerdir; sunucu yalnızca "kim, nerede, hangi
veriye sahip" bilgisini ve kısa süreli önbellekleri tutar.

## Neden Go?

- `goroutine` + `channel` ile istemci başına okuma, yazma ve keep-alive akışı
  doğrudan ve okunaklı biçimde ayrılabiliyor.
- Harici bağımlılık yok; yalnızca Go standart kütüphanesi kullanılıyor.
- Tek statik binary, düşük başlangıç maliyeti ve öngörülebilir bellek kullanımı.
- Rust/tokio da uygun olurdu; ancak bu boyutta bir relay için Go'nun eşzamanlılık
  modeli ve standart `net` paketi daha az kod ve daha kolay bakım sağlıyor.

## Derleme ve çalıştırma

```bash
go build -o bin/sms-server ./cmd/sms-server
./bin/sms-server -addr :25566
```

Testler:

```bash
go test ./...
go test -race ./...
```

## Mimari

```
cmd/sms-server/       giriş noktası, flag'ler, sinyal yönetimi
internal/config/      tüm limitler ve varsayılanlar
internal/protocol/    tel formatı, kanal adları, JSON payload tipleri
internal/state/       oyuncu/chunk/entity/blok olayı/dünya state'i
internal/anticheat/   hız, dikey hareket, reach, cooldown, rate limit
internal/server/      TCP accept, client okuma/yazma, routing, relay
```

Her bağlantı için:

- bir okuma döngüsü (paketleri parse eder ve yönlendirir),
- bir yazma goroutine'i (sunucudan istemciye push; tek yazar),
- bir heartbeat goroutine'i (uygulama seviyesinde ping),

çalışır. Yazma tarafı thread-safe bir kanal üzerinden beslenir; aynı sokete eş
zamanlı yazma yapılmaz. Yavaş istemcinin kuyruğu belirlenen byte bütçesini
aşarsa bağlantı kapatılır; böylece bellek sınırsız büyümez.

## Tel formatı

Tüm sayılar **big endian**'dır. Bir frame:

| Alan | Boyut | Açıklama |
|---|---:|---|
| magic | 4 | `0x534D5331` (`"SMS1"`) |
| body_len | 4 | `channel_len + payload_len + 18` |
| channel_len | 2 | Kanal adı uzunluğu (1..64) |
| sequence | 8 | İstemci/sunucu tarafından atanabilir; sunucu kendi sırasını üretir |
| timestamp_ms | 8 | Unix epoch milisaniye; sunucu kendi zamanını yazar |
| channel | C | UTF-8 kanal adı, örn. `sms:position` |
| payload | P | Opak byte dizisi |

Toplam frame boyutu `8 + body_len`'dir. Varsayılan üst sınır 2 MiB'dir
(`-max-frame`). Kontrol mesajlarının payload'ı JSON'dur; chunk içeriği gibi
büyük veriler JSON içinde base64 olarak taşınır (`[]byte` standardı). Gelecekte
payload'ı binary formata çevirmek için yalnızca ilgili handler değişir.

Fabric modu bu frame'i kendi plugin-channel wrapper'ı içinde taşıyabilir veya
ayrı bir TCP soketi üzerinden gönderebilir. Bu depo **yalnızca sunucudur**;
istemci/mod kodu içermez.

## Kanal sözleşmesi

### İstemci -> sunucu

| Kanal | Payload |
|---|---|
| `sms:connect` | `{"uuid":"...","username":"...","game_mode":"survival","world":"overworld"}` |
| `sms:disconnect` | `{"reason":"..."}` (opsiyonel) |
| `sms:position` | `{"x":0,"y":64,"z":0,"yaw":0,"pitch":0,"on_ground":true}` |
| `sms:block_change` | `{"world":"overworld","x":1,"y":64,"z":0,"block_id":"minecraft:stone"}` |
| `sms:chunk_produced` | `{"chunk_id":"overworld:0,0","summary":"<base64 / <=2 KiB>"}` |
| `sms:chunk_request` | `{"chunk_id":"overworld:0,0"}` |
| `sms:chunk_response` | `{"chunk_id":"overworld:0,0","data":"<base64 opak chunk verisi>"}` |
| `sms:entity_update` | `{"entity_id":"e1","type":"zombie","x":1,"y":64,"z":2}` |
| `sms:attack` | `{"target_entity_id":"e1","x":1,"y":64,"z":2}` |
| `sms:world_state` | `{"time_of_day":6000,"weather":"clear","spawn_x":0,"spawn_y":64,"spawn_z":0,"border_diameter":1000}` |
| `sms:ping` | `{"time":<unix_ms>}` |
| `sms:pong` | `{"time":<sunucunun gönderdiği değer>}` |

### Sunucu -> istemci

| Kanal | Anlam |
|---|---|
| `sms:connect_ack` | Bağlantı kabul edildi; oyuncu UUID'si ve ping aralığı |
| `sms:block_log` | Bağlanan istemciye son N blok değişikliği |
| `sms:world_state` | Güncel saat/hava/spawn/border referansı |
| `sms:player_list` | Tam oyuncu listesi; join/leave sonrası herkese yayınlanır |
| `sms:position` | Başka bir oyuncunun güncel pozisyonu (`player_id`, `username` eklenmiş) |
| `sms:block_change` | Başka bir oyuncunun blok değişikliği (`actor_id`, `sequence`, zaman eklenmiş) |
| `sms:chunk_authority` | Chunk sahipliği cevabı; `status` = `accepted` veya `conflict` |
| `sms:chunk_request` | Sahibine yönlendirilmiş chunk isteği; `requester_id` eklenmiş |
| `sms:chunk_response` | Talep edene yönlendirilmiş chunk cevabı; `from_id` eklenmiş |
| `sms:entity_update` | Entity pozisyonu + `authority_id` |
| `sms:attack` | Saldırı olayı; `actor_id` eklenmiş |
| `sms:correction` | Hareket reddedildiğinde son geçerli pozisyon |
| `sms:error` | `{"code":"...","message":"...","channel":"..."}` |
| `sms:ping` | Uygulama keep-alive'ı; istemci aynı `time` ile `sms:pong` döndürmeli |
| `sms:pong` | İstemci ping'ine cevap / sunucu ping'ine cevap |

Bağlanan istemci sırasıyla `connect_ack`, `block_log`, `world_state` ve
`player_list` alır. `position`, `block_change`, `entity_update`, `attack` ve
`world_state` yayınları gönderen istemci hariç diğerlerine gider.

## State ve yaşam döngüsü

- **Oyuncu:** UUID, kullanıcı adı, oyun modu, dünya, son kabul edilen
  pozisyon/rotasyon, `on_ground`, son görülme ve ping.
- **Blok olayları:** Sınırlı ring buffer (varsayılan 512). Tüm dünya değil,
  yalnızca "kim, hangi koordinat, ne yaptı" kaydı.
- **Dünya:** saat, hava durumu, spawn, border çapı. Oyun mantığı istemcide;
  sunucu yalnızca son değeri dağıtır.
- **Chunk authority:** `chunk_id -> sahip, özet, revizyon, son güncelleme`.
  Raw chunk verisi sunucuda tutulmaz. Bir istemci çevrimdışı olursa kaydı
  `-chunk-offline-ttl` (varsayılan 5 dk) boyunca tutulur; bu sürede başka bir
  üretici chunk'ı devralabilir. Süre sonunda kayıt silinir. Tablo en fazla
  `-max-chunk-authorities` (varsayılan 16384) kayıt tutar; sınır aşılırsa önce
  çevrimdışı, yoksa en eski kayıt düşürülür.
- **Entity:** `entity_id -> tip, pozisyon, authority`. `entity_update`
  gönderen istemci o entity'nin authority'si olur. Authority istemci ayrılınca
  boşaltılır; güncellenmeyen kayıt `-entity-ttl` sonrası silinir.
- **Bekleyen chunk istekleri:** Talep sahibine yönlendirilir; cevap gelince
  talep edenlere dağıtılır. `-chunk-request-ttl` sonrası düşer. Authority
  ayrılırsa bekleyen istemcilere `authority_offline` hatası gider.

Sunucu hiçbir zaman tam chunk verisi, blok geometrisi, redstone, fizik, pathfinding
veya crafting state'i saklamaz.

## Anomali kontrolleri

Hepsi O(1)'dir ve yalnızca son örnek + geçen süre ile çalışır:

1. **Yatay/düz hareket hızı:** Öklid mesafesi / dt. Varsayılan limitler:
   survival 24, creative 120, spectator 240 blok/s; ayrıca 1 blok tolerans.
   İhlal edilen paket kabul edilmez. `-movement-violations` (varsayılan 3)
   ardışık ihlalden sonra istemciye `sms:correction` gönderilir.
2. **Dikey hareket:** Havadayken `-max-vertical-speed` (120 blok/s); iki örnek
   de yerdeyse `-max-ground-vertical` (1.5 blok) kontrolü. İstemcinin
   `on_ground` biti kullanılır.
3. **Rate limit:** İstemci başına token bucket; varsayılan 120 paket/s, 240
   burst. Aşımda paket düşürülür ve saniyede en fazla bir kez hata döner.
4. **Etkileşim mesafesi:** Blok merkezi ve saldırı hedefi, oyuncunun son
   konumundan `-max-reach` (6 blok) uzaklıkta olmalı.
5. **Saldırı cooldown:** Kabul edilen saldırılar arası en az
   `-attack-cooldown` (300 ms).

Bu kontroller güvenlik garantisi değildir; basit ve hızlı bir sapma filtresidir.

## Önemli flag'ler

```text
-addr :25566
-max-frame 2097152
-idle-timeout 90s
-ping-interval 30s
-max-reach 6
-attack-cooldown 300ms
-rate-limit 120 -rate-burst 240
-max-speed-survival 24 -max-speed-creative 120 -max-speed-spectator 240
-max-vertical-speed 120 -max-ground-vertical 1.5
-movement-violations 3
-block-log-size 512
-chunk-offline-ttl 5m -chunk-request-ttl 30s -entity-ttl 2m
-max-chunk-authorities 16384 -max-chunk-summary-bytes 2048
-outbound-queue-bytes 4194304
```

Tüm flag'ler için `-h` kullanılabilir.

## Sınırlar ve bilinçli olarak yapılmayanlar

- Vanilla Minecraft login/encryption protokolü yoktur; custom modlu istemci
  varsayılır.
- Kalıcı veritabanı, kimlik doğrulama, TLS ve gelişmiş anti-cheat yoktur.
- Chunk veri formatı bilinçli olarak opaktır; özet yalnızca referans/teşhis
  amaçlı sınırlı boyutta tutulur.
- Mod dağıtımı bu sunucunun işi değildir; istemcide modun kurulu olduğu
  varsayılır.

## Kayda geçenler

Testlere göre 5-7 MB arası RAM harcamakta (Yeni fixlerde artabilir).
