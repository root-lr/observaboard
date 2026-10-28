import json
import logging
import time
import urllib.error
import urllib.request

from django.db import transaction
from django.http import HttpResponse, JsonResponse
from django.shortcuts import get_object_or_404
from django.views.decorators.csrf import csrf_exempt
from django.views.decorators.http import require_GET, require_POST

from . import chaos
from .models import Order, Product
from .tasks import send_confirmation

log = logging.getLogger("store")


def health(request):
    return HttpResponse("ok")


@require_GET
def home(request):
    chaos.maybe_slow("latency")
    # Deliberately naive (N+1): each product triggers a query for its category,
    # visible in traces as a burst of SQL spans.
    products = Product.objects.order_by("id")[:20]
    rows = "".join(f"<li>{p.name} — {p.category.name} — {p.price_cents / 100:.2f} €</li>" for p in products)
    return HttpResponse(f"<h1>ObservaBoard Shop</h1><ul>{rows}</ul>")


@require_GET
def product_list(request):
    chaos.maybe_fail("errors", probability=0.3)
    chaos.maybe_slow("latency")
    products = Product.objects.select_related("category").order_by("id")
    return JsonResponse({"products": [
        {"id": p.id, "name": p.name, "category": p.category.name, "price": p.price_cents / 100} for p in products
    ]})


@require_GET
def product_detail(request, pk):
    p = get_object_or_404(Product.objects.select_related("category"), pk=pk)
    return JsonResponse({"id": p.id, "name": p.name, "category": p.category.name, "price": p.price_cents / 100, "stock": p.stock})


def charge_payment(order):
    """Call to an external "payment provider" (simulated)."""
    if chaos.active("dependency"):
        # The provider stops responding: wait for the timeout, then fail.
        time.sleep(3)
        raise urllib.error.URLError("payment provider timeout after 3s")
    time.sleep(0.05)


@csrf_exempt
@require_POST
def order_create(request):
    chaos.maybe_slow("latency")
    try:
        payload = json.loads(request.body or b"{}")
        product_id = int(payload.get("product_id", 0))
        quantity = max(1, int(payload.get("quantity", 1)))
    except (ValueError, TypeError):
        return JsonResponse({"error": "invalid request"}, status=400)
    product = get_object_or_404(Product, pk=product_id)
    with transaction.atomic():
        order = Order.objects.create(product=product, quantity=quantity)
    try:
        charge_payment(order)
    except urllib.error.URLError as exc:
        order.status = "failed"
        order.save(update_fields=["status"])
        log.error("payment failed for order %s: %s", order.id, exc)
        return JsonResponse({"error": "payment unavailable"}, status=502)
    order.status = "paid"
    order.save(update_fields=["status"])
    send_confirmation.delay(order.id)
    log.info("order %s paid (%s x %s)", order.id, quantity, product.name)
    return JsonResponse({"id": order.id, "status": order.status}, status=201)


@require_GET
def order_detail(request, pk):
    order = get_object_or_404(Order.objects.select_related("product"), pk=pk)
    return JsonResponse({"id": order.id, "product": order.product.name, "quantity": order.quantity, "status": order.status})
