"""Pannes simulées, activées par `python manage.py chaos <mode>`.

L'état est partagé entre le web et le worker via un fichier (volume `chaos`),
avec une date d'expiration : une panne oubliée s'arrête toute seule.
"""
import json
import os
import random
import time

from django.conf import settings

MODES = {
    "errors": "30 % des appels à /api/products/ renvoient une erreur 500",
    "latency": "+1,5 s sur les pages produits et les commandes",
    "dependency": "le service de paiement externe ne répond plus (timeout)",
    "tasks": "les tâches Celery d'envoi de confirmation échouent",
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
        raise RuntimeError(f"Panne simulée ({mode}) : {MODES[mode]}")


def maybe_slow(mode, seconds=1.5):
    if active(mode):
        time.sleep(seconds)
