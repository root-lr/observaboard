"""Simulated failures, enabled by `python manage.py chaos <mode>`.

The state is shared between web and worker through a file (volume `chaos`),
with an expiry date: a forgotten failure stops on its own.
"""
import json
import os
import random
import time

from django.conf import settings

MODES = {
    "errors": "30% of calls to /api/products/ return a 500 error",
    "latency": "+1.5 s on product pages and orders",
    "dependency": "the external payment service stops responding (timeout)",
    "tasks": "Celery confirmation-sending tasks fail",
}


def _read():
    try:
        with open(settings.CHAOS_FILE) as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def active(mode):
    return _read().get(mode, 0) > time.time()


def enable(mode, minutes):
    state = {k: v for k, v in _read().items() if v > time.time()}
    state[mode] = time.time() + minutes * 60
    os.makedirs(os.path.dirname(settings.CHAOS_FILE), exist_ok=True)
    with open(settings.CHAOS_FILE, "w") as f:
        json.dump(state, f)


def reset():
    try:
        os.remove(settings.CHAOS_FILE)
    except FileNotFoundError:
        pass


def status():
    now = time.time()
    return {k: int(v - now) for k, v in _read().items() if v > now}


def maybe_fail(mode, probability=1.0):
    if active(mode) and random.random() < probability:
        raise RuntimeError(f"Simulated failure ({mode}): {MODES[mode]}")


def maybe_slow(mode, seconds=1.5):
    if active(mode):
        time.sleep(seconds)
