import logging
import random
import time

from celery import shared_task

from . import chaos

log = logging.getLogger("store.tasks")


@shared_task
def send_confirmation(order_id):
    """Envoie l'email de confirmation d'une commande (simulé)."""
    from .models import Order

    chaos.maybe_fail("tasks")
    time.sleep(random.uniform(0.1, 0.4))
    updated = Order.objects.filter(pk=order_id, status="paid").update(status="confirmed")
    log.info("confirmation envoyée pour la commande %s", order_id)
    return updated
