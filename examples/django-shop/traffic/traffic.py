"""Traffic generator: simulates visitors to the shop.

Variables: TARGET (shop URL), RATE (requests per second, default 4).
"""
import json
import os
import random
import time
import urllib.error
import urllib.request

TARGET = os.environ.get("TARGET", "http://localhost:8000").rstrip("/")
RATE = float(os.environ.get("RATE", "4"))


def call(method, path, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(TARGET + path, data=data, method=method, headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status
    except urllib.error.HTTPError as exc:
        return exc.code
    except (urllib.error.URLError, TimeoutError, ConnectionError):
        return 0


def visit():
    r = random.random()
    if r < 0.35:
        return "GET /", call("GET", "/")
    if r < 0.65:
        return "GET /api/products/", call("GET", "/api/products/")
    if r < 0.85:
        return "GET /api/products/<id>/", call("GET", f"/api/products/{random.randint(1, 19)}/")
    if r < 0.97:
        return "POST /api/orders/", call("POST", "/api/orders/", {"product_id": random.randint(1, 17), "quantity": random.randint(1, 3)})
    # a few realistic 404s
    return "GET /api/products/<unknown>/", call("GET", f"/api/products/{random.randint(500, 900)}/")


def main():
    print(f"Traffic to {TARGET} ({RATE} req/s)", flush=True)
    while call("GET", "/health/") != 200:
        time.sleep(2)
    n, errors = 0, 0
    while True:
        start = time.time()
        _, status = visit()
        n += 1
        errors += status >= 500 or status == 0
        if n % 200 == 0:
            print(f"{n} requests, {errors} server errors", flush=True)
        time.sleep(max(0.0, random.expovariate(RATE) - (time.time() - start)))


if __name__ == "__main__":
    main()
