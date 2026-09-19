FROM python:3.12-alpine

RUN pip install --no-cache-dir pillow

COPY telegram_notifier.py /app/telegram_notifier.py

CMD ["python", "/app/telegram_notifier.py"]
