from django.db import models


class Category(models.Model):
    name = models.CharField(max_length=80, unique=True)

    def __str__(self):
        return self.name


class Product(models.Model):
    name = models.CharField(max_length=120)
    category = models.ForeignKey(Category, on_delete=models.PROTECT, related_name="products")
    price_cents = models.PositiveIntegerField()
    stock = models.PositiveIntegerField(default=100)

    def __str__(self):
        return self.name


class Order(models.Model):
    STATUS = [("pending", "Pending"), ("paid", "Paid"), ("confirmed", "Confirmed"), ("failed", "Failed")]
    product = models.ForeignKey(Product, on_delete=models.PROTECT, related_name="orders")
    quantity = models.PositiveIntegerField(default=1)
    status = models.CharField(max_length=12, choices=STATUS, default="pending")
    created_at = models.DateTimeField(auto_now_add=True)
