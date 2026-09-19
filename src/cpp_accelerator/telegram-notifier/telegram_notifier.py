#!/usr/bin/env python3
"""Send each new bird-capture BMP from the captures dir to a Telegram chat.

Polls CAPTURES_DIR every POLL_SECONDS, converts new *.bmp files to JPEG and
sends them via the Bot API sendPhoto. Stays idle when TELEGRAM_BOT_TOKEN or
TELEGRAM_CHAT_ID are unset. On restart it baselines to the newest existing
file without sending, so history is never re-sent.
"""

import io
import os
import sys
import time
import urllib.request
import uuid

from PIL import Image

CAPTURES_DIR = os.environ.get("CAPTURES_DIR", "/captures")
STATE_FILE = os.environ.get("STATE_FILE", "/tmp/last_sent")
POLL_SECONDS = int(os.environ.get("POLL_SECONDS", "10"))
MAX_DIMENSION = 1280
JPEG_QUALITY = 82
BOT_TOKEN = os.environ.get("TELEGRAM_BOT_TOKEN", "")
CHAT_ID = os.environ.get("TELEGRAM_CHAT_ID", "")


def log(message: str) -> None:
    print(f"[telegram-notifier] {message}", flush=True)


def send_photo(path: str) -> None:
    image = Image.open(path)
    image.thumbnail((MAX_DIMENSION, MAX_DIMENSION))
    buffer = io.BytesIO()
    image.convert("RGB").save(buffer, format="JPEG", quality=JPEG_QUALITY)
    buffer.seek(0)

    boundary = uuid.uuid4().hex
    parts = []
    for name, value in (("chat_id", CHAT_ID), ("caption", f"Bird capture: {os.path.basename(path)}")):
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{name}"\r\n\r\n{value}\r\n'.encode())
    parts.append(
        f'--{boundary}\r\nContent-Disposition: form-data; name="photo"; filename="capture.jpg"\r\n'
        f"Content-Type: image/jpeg\r\n\r\n".encode()
    )
    parts.append(buffer.getvalue())
    parts.append(f"\r\n--{boundary}--\r\n".encode())

    request = urllib.request.Request(
        f"https://api.telegram.org/bot{BOT_TOKEN}/sendPhoto",
        data=b"".join(parts),
        headers={"Content-Type": f"multipart/form-data; boundary={boundary}"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        if response.status != 200:
            raise RuntimeError(f"sendPhoto returned {response.status}")


def newest_bmp() -> str | None:
    files = [os.path.join(CAPTURES_DIR, f) for f in os.listdir(CAPTURES_DIR) if f.lower().endswith(".bmp")]
    return max(files, key=os.path.getmtime) if files else None


def pending_after(marker: str | None) -> list[str]:
    files = [os.path.join(CAPTURES_DIR, f) for f in os.listdir(CAPTURES_DIR) if f.lower().endswith(".bmp")]
    if marker is not None:
        files = [f for f in files if os.path.getmtime(f) > os.path.getmtime(marker)]
    return sorted(files, key=os.path.getmtime)


def main() -> None:
    if not BOT_TOKEN or not CHAT_ID:
        log("TELEGRAM_BOT_TOKEN/TELEGRAM_CHAT_ID not set — idle")
        while True:
            time.sleep(3600)

    baseline = newest_bmp()
    if baseline is not None:
        with open(STATE_FILE, "w") as state:
            state.write(baseline)
        log(f"baselined to existing {os.path.basename(baseline)} (no resend of history)")

    log(f"watching {CAPTURES_DIR} every {POLL_SECONDS}s")
    while True:
        try:
            marker = open(STATE_FILE).read().strip() if os.path.exists(STATE_FILE) else None
            for path in pending_after(marker if marker and os.path.exists(marker) else None):
                send_photo(path)
                with open(STATE_FILE, "w") as state:
                    state.write(path)
                log(f"sent {os.path.basename(path)}")
        except Exception as error:  # retry on the next poll without advancing the marker
            log(f"error: {error}")
        time.sleep(POLL_SECONDS)


if __name__ == "__main__":
    sys.exit(main())
