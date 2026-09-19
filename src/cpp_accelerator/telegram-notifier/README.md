# Telegram capture notifier

Sidecar that sends every new bird-capture BMP to a Telegram chat. No changes to
the accelerator: it watches the same `captures/` volume the host already
mounts, converts BMP → JPEG (Telegram only accepts jpg/png photos) and calls
the Bot API `sendPhoto`.

## Setup (Jetson `.env`)

```
TELEGRAM_BOT_TOKEN=123456:ABC...   # from @BotFather
TELEGRAM_CHAT_ID=...               # your chat/group id
```

Empty values keep the sidecar idle (safe default for non-prod environments).

## Deploy

```
docker compose build telegram-notifier
docker compose up -d telegram-notifier
docker logs -f telegram-notifier
```

On restart it baselines to the newest existing capture — history is never
re-sent; only files newer than the last successful send go out.
