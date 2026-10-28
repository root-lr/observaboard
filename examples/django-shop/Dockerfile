FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1

WORKDIR /app
COPY requirements.txt .
RUN python -m pip install --no-cache-dir -r requirements.txt

COPY . .

RUN useradd --create-home --uid 1000 shop && mkdir -p /tmp/chaos && chown shop /tmp/chaos
USER shop

EXPOSE 8000
CMD ["gunicorn", "shop.wsgi:application", "--bind", "0.0.0.0:8000"]
