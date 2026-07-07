# Sending media (photo, video, audio, document)

How the server sends media over the WhatsMeow (`api`) workflow, and how to call it.

> Scope: implemented for the **WhatsMeow** provider. The Business/Cloud API provider
> currently has a `SendMedia` stub. See also `whatsmeow_workflow.md` §7.

## Endpoint

```
POST /messages/media          Content-Type: multipart/form-data
```

| Field     | Required | Notes |
|-----------|----------|-------|
| `to`      | yes      | Recipient in **E.164 digits, no `+`** (country code + number), e.g. `14088255555`. |
| `file`    | yes      | The media file (multipart upload). |
| `caption` | no       | Text caption (image / video / document only). |
| `kind`    | no       | `image` \| `video` \| `audio` \| `document` \| `sticker`. Omit to auto-detect from MIME type. **`sticker` must be passed explicitly** (a WebP is otherwise treated as a normal image). |

**Response** (`200`): `{ "message_id": "...", "provider": "whatsapp-api" }`

### Examples

```bash
# photo with caption
curl -F to=14088255555 -F caption=hi -F file=@photo.jpg   http://localhost:8082/messages/media
# video
curl -F to=14088255555 -F file=@clip.mp4                  http://localhost:8082/messages/media
# audio
curl -F to=14088255555 -F file=@note.ogg                  http://localhost:8082/messages/media
# PDF (auto-classified as a document, keeps the filename)
curl -F to=14088255555 -F file=@invoice.pdf               http://localhost:8082/messages/media
# sticker (WebP; kind must be explicit)
curl -F to=14088255555 -F kind=sticker -F file=@sticker.webp http://localhost:8082/messages/media
```

## How it works

```
multipart file ─▶ handler(sendMedia)                         [internal/api/handlers.go]
                    │  read bytes, resolve mimetype + kind
                    ▼
                 provider.SendMedia(ctx, to, MediaMessage)   [internal/whatsapp/whatsmeow_provider.go]
                    │  1) resolveJID(to)      → IsOnWhatsApp lookup (canonical JID/LID)
                    │  2) client.Upload(data, MediaType) → UploadResponse
                    │  3) buildMediaMessage(m, up)        → waE2E.{Image|Video|Audio|Document}Message
                    │  4) client.SendMessage(ctx, jid, msg)
                    ▼
                 { message_id, provider }
```

### 1. Handler — `internal/api/handlers.go`
- Reads `to`, `caption`, optional `kind`, and the `file` from the multipart form.
- Mimetype = the upload's `Content-Type`, falling back to `http.DetectContentType(bytes)`
  when missing/`application/octet-stream`.
- `kindFromMime` maps `image/*`→image, `video/*`→video, `audio/*`→audio, everything else
  (incl. `application/pdf`) → document.

### 2. Provider — `internal/whatsapp/whatsmeow_provider.go`
The WhatsMeow send is two calls plus a lookup:

- **`resolveJID`** — `client.IsOnWhatsApp(ctx, ["+"+to])`. Required for numbers the client
  has never messaged: it returns the canonical JID/LID and caches the mapping. Without it,
  `SendMessage` to a new number can fail with `no LID found …`.
- **`client.Upload(ctx, bytes, mediaType)`** — encrypts + uploads, returns
  `UploadResponse{URL, DirectPath, MediaKey, FileEncSHA256, FileSHA256, FileLength}`.
- **`buildMediaMessage`** — copies those 6 fields into the matching proto and sets
  `Mimetype` (+ `Caption` for image/video/document, + `FileName` for document).
- **`client.SendMessage(ctx, jid, msg)`** — same call `SendText` uses.

`MediaKind` → `whatsmeow.MediaType` mapping lives in `getWhatMeowMediaType`. Stickers upload
with `MediaImage` (WhatsApp has no separate sticker upload type) but build a `StickerMessage`.

## Gotchas

- **Mimetype is mandatory** for WhatsApp to render the file — an empty mimetype produces a
  broken/undownloadable message. The handler always sets one.
- **New recipients need JID resolution** (`resolveJID`); this is why an image can fail for a
  brand-new number but work for existing chats.
- **Number format**: E.164 digits, no `+` (e.g. US `14088255555`, not `4088255555`). A
  missing country code surfaces as `recipient … is not on WhatsApp` / `no LID found`.
- **Size limits** are enforced by WhatsApp (≈16 MB images/audio/video, up to 100 MB
  documents); the server keeps gin's default multipart handling.
- **Stickers must be WebP** (`image/webp`), ideally 512×512; other formats upload but may not
  display as a proper sticker. Pass `kind=sticker` explicitly.
- **Voice notes / PTT audio, thumbnails, and GCS `media_ref` fetching are not implemented
  yet** — audio is sent as a normal audio file.

## References

- `Client.Upload` — https://pkg.go.dev/go.mau.fi/whatsmeow#Client.Upload
  (godoc: *"copy the fields in the response to the corresponding fields in a protobuf message"*)
- `UploadResponse` — https://pkg.go.dev/go.mau.fi/whatsmeow#UploadResponse
- `MediaType` constants — https://pkg.go.dev/go.mau.fi/whatsmeow#MediaType
- `Client.SendMessage` — https://pkg.go.dev/go.mau.fi/whatsmeow#Client.SendMessage
- Message protos — https://pkg.go.dev/go.mau.fi/whatsmeow/proto/waE2E
- `Client.IsOnWhatsApp` — https://pkg.go.dev/go.mau.fi/whatsmeow#Client.IsOnWhatsApp
- Canonical example (`mdtest`) — https://github.com/tulir/whatsmeow/blob/main/mdtest/main.go
