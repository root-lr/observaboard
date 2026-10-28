import random

from django.core.management.base import BaseCommand

from store.models import Category, Product

CATALOG = {
    "Coffee": ["Benin espresso", "Mocha", "Organic arabica", "Bold robusta"],
    "Tea": ["Green tea", "Kinkeliba", "Mint tea", "Rooibos"],
    "Grocery": ["Gari", "Red palm oil", "Dried chili", "Forest honey", "Roasted peanuts"],
    "Accessories": ["Clay mug", "Coffee grinder", "Teapot", "Reusable filter"],
}


class Command(BaseCommand):
    help = "Creates the demo catalog if it is empty."

    def handle(self, *args, **options):
        if Product.objects.exists():
            self.stdout.write("Catalog already present.")
            return
        rng = random.Random(42)
        for cat_name, items in CATALOG.items():
            cat = Category.objects.create(name=cat_name)
            for name in items:
                Product.objects.create(name=name, category=cat, price_cents=rng.randint(300, 4500))
        self.stdout.write(f"{Product.objects.count()} products created.")
