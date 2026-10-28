"""Settings for the ObservaBoard example shop (deliberately minimal)."""
import os
from pathlib import Path

import dj_database_url

BASE_DIR = Path(__file__).resolve().parent.parent

SECRET_KEY = os.environ.get("DJANGO_SECRET_KEY", "example-observaboard-do-not-use-in-production")
DEBUG = os.environ.get("DJANGO_DEBUG", "0") == "1"
ALLOWED_HOSTS = ["*"]

INSTALLED_APPS = [
    "django.contrib.contenttypes",
    "django.contrib.staticfiles",
    "store",
]

MIDDLEWARE = [
    "django.middleware.security.SecurityMiddleware",
    "django.middleware.common.CommonMiddleware",
]

ROOT_URLCONF = "shop.urls"
WSGI_APPLICATION = "shop.wsgi.application"

TEMPLATES = [
    {
        "BACKEND": "django.template.backends.django.DjangoTemplates",
        "DIRS": [],
        "APP_DIRS": True,
        "OPTIONS": {"context_processors": []},
    }
]

DATABASES = {
    "default": dj_database_url.config(
        default=f"sqlite:///{BASE_DIR / 'db.sqlite3'}",
        conn_max_age=60,
    )
}
DEFAULT_AUTO_FIELD = "django.db.models.BigAutoField"

LANGUAGE_CODE = "en-us"
TIME_ZONE = "UTC"
USE_TZ = True
STATIC_URL = "static/"

CELERY_BROKER_URL = os.environ.get("REDIS_URL", "redis://localhost:6379/0")
CELERY_TASK_ACKS_LATE = True
CELERY_WORKER_HIJACK_ROOT_LOGGER = False

CHAOS_FILE = os.environ.get("CHAOS_FILE", "/tmp/chaos/state.json")

# The trace_id added by OpenTelemetry (OTEL_PYTHON_LOG_CORRELATION=true) is written
# on every line: Grafana turns it into a link to the trace (Loki -> Tempo).
LOGGING = {
    "version": 1,
    "disable_existing_loggers": False,
    "filters": {"trace": {"()": "shop.logfilters.TraceIdDefaults"}},
    "formatters": {
        "plain": {
            "format": "%(asctime)s %(levelname)s %(name)s trace_id=%(otelTraceID)s span_id=%(otelSpanID)s %(message)s",
        }
    },
    "handlers": {
        "console": {"class": "logging.StreamHandler", "formatter": "plain", "filters": ["trace"]},
    },
    "root": {"handlers": ["console"], "level": "INFO"},
    "loggers": {
        "django.request": {"handlers": ["console"], "level": "WARNING", "propagate": False},
    },
}
