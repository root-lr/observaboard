import logging
import random
import time

from celery import shared_task

from . import chaos

log = logging.getLogger("store.tasks")


@shared_task
def send_confirmation(order_id):
    """Sends the order confirmation email (simulated)."""
    from .models import Order

    chaos.maybe_fail("tasks")
    time.sleep(random.uniform(0.1, 0.4))
    updated = Order.objects.filter(pk=order_id, status="paid").update(status="confirmed")
    log.info("confirmation sent for order %s", order_id)
    return updated
