import logging


class TraceIdDefaults(logging.Filter):
    """Ensures the correlation fields are present, even without OpenTelemetry."""

    def filter(self, record: logging.LogRecord) -> bool:
        if not hasattr(record, "otelTraceID"):
            record.otelTraceID = "0"
        if not hasattr(record, "otelSpanID"):
            record.otelSpanID = "0"
        return True
