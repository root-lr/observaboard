# ObservaQueueBacklog — la file Celery grossit

**Sévérité :** avertissement · **Déclenchement :** plus de messages en attente dans la liste Redis `celery` que le seuil, pendant 10 minutes.
**Seuil :** 100 par défaut, modifiable avec `alerts.queue_length` sur le service Redis dans `observa.yaml`.

## Ce que signifie l'alerte

Les tâches arrivent plus vite que les workers ne les traitent. Seule la file par défaut `celery` (base 0) est surveillée en V1.

## Impact pour les utilisateurs

Les traitements asynchrones prennent du retard : un email part avec plusieurs minutes de décalage, un export n'arrive pas.

## Dashboards à ouvrir

1. **Worker Celery** : *File d'attente*, *Workers actifs*, *Durée p95 par type*.
2. **Redis** : mémoire (une file qui grossit consomme de la mémoire).

## Requêtes utiles

```promql
redis_key_size{project="PROJET", env="ENV", key="celery"}
```

```promql
count((time() - observa:service:last_seen_timestamp{project="PROJET", env="ENV", service="WORKER"}) < 120)
```

## Causes probables

- **Workers arrêtés** ou en échec (voir l'alerte ObservaServiceDown sur le worker).
- **Tâches devenues lentes** (dépendance lente) : la durée p95 augmente.
- **Pic de tâches** (import massif, campagne d'emails).
- **Concurrence trop faible** (`--concurrency`).

## Actions

1. Vérifiez que les workers tournent : `docker compose ps`.
2. Si les tâches sont lentes, traitez la cause (voir les traces).
3. Ajoutez des workers temporairement : `docker compose up -d --scale worker=3`.
4. Séparez les tâches lentes dans une file dédiée pour ne pas bloquer les autres.

## Vérifier le retour à la normale

- La courbe *File d'attente* redescend vers 0.
