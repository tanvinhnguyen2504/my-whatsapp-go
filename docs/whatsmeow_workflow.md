# Workflow: Xây dựng WhatsApp Server với whatsmeow (số điện thoại chính chủ)

> **Phạm vi:** Tài liệu mô tả kiến trúc và quy trình xây dựng một server Go đại diện cho **một số WhatsApp cá nhân chính chủ**, dùng thư viện unofficial `go.mau.fi/whatsmeow` — nhận tin nhắn real-time (thay thế webhook) và gửi tin nhắn qua HTTP API nội bộ.
>
> **KHÔNG dùng** WhatsApp Cloud API chính thức. Xem tài liệu `whatsapp-cloud-api-webhook-workflow.md` nếu cần phương án chính thức.

---

## 1. Khái niệm nền tảng — khác biệt với Cloud API

| | Cloud API (chính thức) | whatsmeow (unofficial) |
|---|---|---|
| Bản chất | Gọi Graph API của Meta | Server **giả lập một linked device** (như WhatsApp Web) |
| Nhận tin | Meta POST vào webhook URL công khai | **Push real-time qua WebSocket** → event handler nội bộ |
| Cần HTTPS công khai? | Bắt buộc | **Không** — server có thể nằm sau NAT/private network |
| Số điện thoại | Test number / số business đăng ký WABA | Số cá nhân chính chủ, pair qua QR |
| Template / phí | Bắt buộc template ngoài cửa sổ 24h, tính phí | Không template, không phí |
| Rủi ro | Không | **Vi phạm ToS — số cá nhân có thể bị khóa** nếu hành vi bất thường |
| Scale | Ngang (stateless webhook) | **Dọc — 1 session = 1 tiến trình duy nhất** |

**Hệ quả kiến trúc quan trọng:** không có "webhook từ Meta". "Webhook" trong hệ thống này là **event handler + dispatcher nội bộ** do ta tự xây.

---

## 2. Kiến trúc tổng thể

```
[Điện thoại chính chủ]        [WhatsApp servers]           [Server Go]
  (WhatsApp app)                                        (linked device)
        |                            |                          |
        |---- pair QR (1 lần) ----->|<===== WebSocket ========>|
        |                            |      (E2E encrypted)     |
                                     |                          |
[Người nhận] ---- nhắn tin -------->|--- events.Message ------>| EventHandler
                                     |                          |     |
        <--------------------------- |<--- SendMessage ---------| Dispatcher
                                     |                          |     |
                                     |                          v     v
                                     |                   [HTTP API]  [Bot logic /
                                     |                   POST /send   internal webhook]
                                     |                          ^
                                     |                   [Media Resolver (GCS)]
```

**Các thành phần:**

| Thành phần | Vai trò |
|---|---|
| **whatsmeow Client** | Giữ WebSocket, mã hóa Signal, gửi/nhận message |
| **SQL Store** | Lưu session + khóa mã hóa (SQLite / Postgres) — mất là phải pair lại |
| **EventHandler** | Nhận mọi event, lọc, đẩy vào queue nội bộ |
| **Dispatcher** | Chuyển event → payload chuẩn hóa → forward cho bot logic |
| **HTTP Send API** | Endpoint nội bộ để hệ thống khác gửi tin qua số của ta |
| **Media Resolver** | Lấy bytes từ GCS, upload lên WhatsApp, cache theo generation |

---

## 3. Giai đoạn 1 — Pairing (thực hiện một lần)

Sau `client.Connect()`, client sẽ tự authenticate nếu store đã có session; nếu chưa, phát QR event để thiết lập liên kết mới.

**Cách A — QR code (khuyên dùng lần đầu):**

```go
if client.Store.ID == nil { // chưa có session
    qrChan, _ := client.GetQRChannel(context.Background())
    err = client.Connect()
    // ...
    for evt := range qrChan {
        if evt.Event == "code" {
            qrterminal.GenerateHalfBlock(evt.Code, qrterminal.L, os.Stdout)
        } else {
            fmt.Println("Pairing event:", evt.Event) // "success" | "timeout" | "error"
        }
    }
} else {
    err = client.Connect() // đã có session, connect thẳng
}
```

Trên điện thoại: **Settings → Linked Devices → Link a Device** → quét QR.

**Cách B — Pairing code (server headless):** dùng `client.PairPhone(phone, true, ...)` để sinh mã, nhập trên điện thoại thay vì quét QR. Lưu ý phải `Connect()` trước khi gọi (QR event vẫn phát ra nhưng bỏ qua được).

**Sau khi pair:**
- Điện thoại KHÔNG cần online liên tục (multi-device protocol).
- Điện thoại offline > ~14 ngày → linked device bị ngắt → phải pair lại.
- Server chiếm 1 slot trong tối đa 4 linked devices của tài khoản.

---

## 4. Giai đoạn 2 — Session persistence

Whatsmeow **bắt buộc** có storage backend; không có thì mỗi lần restart phải quét QR mới. SQL store hỗ trợ SQLite và PostgreSQL.

```go
// SQLite (1 instance, đơn giản):
container, err := sqlstore.New(ctx, "sqlite3",
    "file:/data/wa-session.db?_foreign_keys=on", dbLog)
// nhớ import _ "github.com/mattn/go-sqlite3"

deviceStore, err := container.GetFirstDevice(ctx) // chỉ 1 số → GetFirstDevice
client := whatsmeow.NewClient(deviceStore, clientLog)
```

**Lựa chọn theo hạ tầng:**

| Deploy | Store | Ghi chú |
|---|---|---|
| GCE VM / GKE + PVC | SQLite trên persistent disk | Đơn giản nhất |
| Container stateless-ish | Cloud SQL Postgres (driver `pgx`) | Restart/reschedule không mất session |
| ❌ Cloud Run scale-to-zero | — | KHÔNG phù hợp: cần WebSocket sống 24/7 |

**Quy tắc vàng:**
- ⛔ Không bao giờ xóa/mất store DB → mất là pair lại, pair/unpair lặp lại là red flag với Meta.
- ⛔ Không chạy 2 tiến trình cùng trỏ vào 1 session → xung đột, có thể bị logout.
- ✅ Backup store DB định kỳ (chứa khóa mã hóa — bảo vệ như secret).

---

## 5. Giai đoạn 3 — Event loop (thay thế webhook)

Đăng ký handler qua `AddEventHandler`; mọi handler nhận **tất cả** event, switch theo type. Pattern chuẩn khi handler cần dùng Client — bọc trong struct:

```go
type WAService struct {
    WAClient  *whatsmeow.Client
    Inbox     chan *events.Message // queue nội bộ
}

func (s *WAService) handler(evt any) {
    switch v := evt.(type) {
    case *events.Message:
        // === CÁC BỘ LỌC BẮT BUỘC ===
        if v.Info.IsFromMe { return }          // tin do chính mình gửi (từ điện thoại)
        if v.Info.IsGroup { return }           // bỏ group nếu chỉ xử lý 1-1 (tùy nhu cầu)
        // Bỏ qua history sync: chỉ nhận tin real-time
        select {
        case s.Inbox <- v:                     // đẩy queue, return ngay
        default:
            // queue đầy → log + metric, không block event loop
        }
    case *events.Receipt:
        // delivered / read receipts nếu cần tracking
    case *events.Disconnected:
        // metric + log; AutoReconnect sẽ tự xử lý
    case *events.LoggedOut:
        // NGHIÊM TRỌNG: session chết, phải pair lại → alert ngay
    }
}
```

**Nguyên tắc:** handler phải **return nhanh** — mọi xử lý nặng (LLM, DB, gửi reply, resolve media) chạy ở worker goroutine đọc từ `Inbox`.

**Trích xuất nội dung tin đến:**

```go
msg := v.Message
switch {
case msg.GetConversation() != "":            text = msg.GetConversation()
case msg.GetExtendedTextMessage() != nil:    text = msg.GetExtendedTextMessage().GetText()
case msg.GetImageMessage() != nil:           data, _ = client.Download(msg.GetImageMessage())
case msg.GetAudioMessage() != nil:           data, _ = client.Download(msg.GetAudioMessage())
// ... DocumentMessage, VideoMessage tương tự
}
```

---

## 6. Giai đoạn 4 — Dispatcher ("webhook nội bộ")

Mục tiêu: **bot logic không cần biết** tin đến từ whatsmeow hay Cloud API.

Worker đọc `Inbox` → chuẩn hóa thành payload (khuyên dùng: **mô phỏng format webhook Cloud API** `entry[].changes[].value.messages[]`) → forward:

- **HTTP POST** vào endpoint nội bộ sẵn có của hệ thống, hoặc
- **Pub/Sub topic** (đã ở Google Cloud) nếu muốn decouple hoàn toàn, hoặc
- Gọi trực tiếp hàm bot logic nếu cùng tiến trình.

```
events.Message ──> normalize() ──> {from, wamid, type, text|media, ts}
                                        │
                     ┌──────────────────┼──────────────────┐
                     v                  v                  v
              POST /internal      Pub/Sub topic      direct func call
```

**Idempotency:** dedupe theo `v.Info.ID` (wamid) trước khi xử lý — retry/reconnect có thể phát lại event.

---

## 7. Giai đoạn 5 — HTTP Send API

Endpoint nội bộ để các service khác gửi tin "dưới danh nghĩa" số của ta:

```
POST /send
{
  "to": "8490xxxxxxx",
  "type": "text" | "image" | "audio" | "document",
  "text": "...",              // khi type=text
  "media_ref": "gs://bucket/object",  // khi type=media → qua Media Resolver
  "caption": "..."
}
```

```go
jid := types.NewJID(req.To, types.DefaultUserServer) // số → JID
resp, err := client.SendMessage(ctx, jid, &waE2E.Message{
    Conversation: proto.String(req.Text),
})
```

Với media: Resolver (GCS) → check cache theo `bucket/object#generation` → `client.Upload()` → build `waE2E.ImageMessage`/... → `SendMessage`. (Chi tiết ở tài liệu Media Resolver.)

**Bảo mật bắt buộc:** endpoint này = quyền nhắn tin bằng số cá nhân của bạn.
- API key / IAM / mTLS, chỉ expose trong VPC nội bộ.
- Rate limit toàn cục (`golang.org/x/time/rate`).
- Audit log mọi request gửi.

---

## 8. Giai đoạn 6 — Vận hành & độ tin cậy

| Hạng mục | Cách làm |
|---|---|
| Reconnect | Whatsmeow có sẵn AutoReconnect với backoff; vẫn lắng nghe `events.Disconnected` để đo đếm |
| Logout | `events.LoggedOut` → alert PagerDuty/Slack ngay, cần pair lại thủ công |
| Health check | Endpoint `/healthz` trả theo `client.IsConnected()` |
| Deploy | **1 replica duy nhất**, restart policy `always`; KHÔNG scale ngang |
| Graceful shutdown | Bắt SIGTERM → `client.Disconnect()` → flush queue |
| Media hết hạn | Download tin cũ gặp 404/410 → `SendMediaRetryReceipt` yêu cầu điện thoại re-upload |
| Timeout | `SendMessage` mặc định chờ response 75s — tách context của bước GCS read và bước send |

---

## 9. Giảm rủi ro khóa số (QUAN TRỌNG — số chính chủ)

Đây là thư viện unofficial, vi phạm ToS của WhatsApp. Mọi tin bot gửi ra là "bạn" đang nhắn. Nguyên tắc hành xử:

1. **Chỉ reply, hạn chế initiate:** ưu tiên trả lời tin nhắn đến; không gửi hàng loạt tới số lạ chưa từng chat.
2. **Hành vi giống người:** gửi `client.MarkRead(...)` khi nhận tin, `client.SendChatPresence(jid, types.ChatPresenceComposing, ...)` (đang nhập...) 1–3 giây trước khi reply, delay ngẫu nhiên giữa các tin.
3. **Rate limit cứng:** cap số tin/phút toàn hệ thống, bất kể nguồn request.
4. **Không pair/unpair liên tục**, không đổi IP server thất thường (giữ IP tĩnh/NAT ổn định).
5. **Đường lui:** thiết kế interface `MessageSender` với 2 implementation (`WhatsmeowSender`, `CloudAPISender`) — khi cần chính thức hóa, chuyển sang Cloud API mà không sửa bot logic.

---

## 10. Checklist triển khai

- [ ] SQL store persistent (SQLite/PVC hoặc Cloud SQL), có backup
- [ ] Pairing thành công, session tự khôi phục sau restart (test: kill process → start → không cần QR)
- [ ] EventHandler lọc `IsFromMe`, group, history sync; đẩy queue non-blocking
- [ ] Dispatcher chuẩn hóa payload + dedupe theo wamid
- [ ] HTTP Send API có auth + rate limit, chỉ trong mạng nội bộ
- [ ] Media Resolver GCS cắm vào luồng send (cache theo generation)
- [ ] Alert cho `events.LoggedOut`; health check theo `IsConnected()`
- [ ] Deploy 1 replica, graceful shutdown, restart always
- [ ] Interface `MessageSender` trừu tượng hóa backend (whatsmeow ↔ Cloud API)

---

## Phụ lục — go.mod tối thiểu

```
require (
    go.mau.fi/whatsmeow latest
    github.com/mattn/go-sqlite3 latest        // hoặc jackc/pgx cho Postgres
    github.com/mdp/qrterminal/v3 latest       // render QR ra terminal
    google.golang.org/protobuf latest
    cloud.google.com/go/storage latest        // Media Resolver
)
```

Packages chính: `go.mau.fi/whatsmeow`, `go.mau.fi/whatsmeow/store/sqlstore`, `go.mau.fi/whatsmeow/types`, `go.mau.fi/whatsmeow/types/events`, `go.mau.fi/whatsmeow/proto/waE2E`.

---

*Cập nhật: 2026-07. Tham khảo: godoc go.mau.fi/whatsmeow; GitHub tulir/whatsmeow (mdtest example, WhatsApp protocol Q&A trong Discussions).*