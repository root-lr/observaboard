import random

from django.core.management.base import BaseCommand

from store.models import Category, Product

CATALOG = {
    "Café": ["Espresso du Bénin", "Moka", "Arabica bio", "Robusta corsé"],
    "Thé": ["Thé vert", "Kinkéliba", "Thé à la menthe", "Rooibos"],
    "Épicerie": ["Gari", "Huile rouge", "Piment séché", "Miel de forêt", "Arachides grillées"],
    "Accessoires": ["Tasse en terre cuite", "Moulin à café", "Théière", "Filtre réutilisable"],
}


class Command(BaseCommand):
    help = "Crée le catalogue de démonstration s'il est vide."

    def handle(self, *args, **options):
        if Product.objects.exists():
            self.stdout.write("Catalogue déjà présent.")
            return
        rng = random.Random(42)
        for cat_name, items in CATALOG.items():
            cat = Category.objects.create(name=cat_name)
            for name in items:
                Product.objects.create(name=name, category=cat, price_cents=rng.randint(300, 4500))
        self.stdout.write(f"{Product.objects.count()} produits créés.")
