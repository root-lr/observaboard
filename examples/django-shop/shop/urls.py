from django.urls import path

from store import views

urlpatterns = [
    path("", views.home, name="home"),
    path("health/", views.health, name="health"),
    path("api/products/", views.product_list, name="product-list"),
    path("api/products/<int:pk>/", views.product_detail, name="product-detail"),
    path("api/orders/", views.order_create, name="order-create"),
    path("api/orders/<int:pk>/", views.order_detail, name="order-detail"),
]
