# ObservaRedisMemoryHigh — Redis proche de maxmemory

**Sévérité :** avertissement · **Déclenchement :** mémoire utilisée au-dessus du seuil de `maxmemory` pendant 10 minutes. Sans `maxmemory` configuré, l'alerte ne se déclenche pas.
**Seuil :** 90 % par défaut, modifiable avec `alerts.memory_pct` dans `observa.yaml`.

## Ce que signifie l'alerte

Redis approche sa limite. Selon `maxmemory-policy`, il va évincer des clés (`allkeys-lru`…) ou refuser les écritures (`noeviction`, défaut).

## Impact pour les utilisateurs

Avec `noeviction` : erreurs d'écriture, tâches Celery refusées. Avec éviction : cache moins efficace, sessions perdues.

## Dashboards à ouvrir

1. **Redis** : *Mémoire*, *Clés par base*, *Clés évincées / s*, *File Celery*.

## Requêtes utiles

```promql
redis_memory_used_bytes{project="PROJET", env="ENV", service="SERVICE"} / redis_memory_max_bytes{project="PROJET", env="ENV", service="SERVICE"}
```

```bash
docker exec -it <conteneur-redis> redis-cli --bigkeys
docker exec -it <conteneur-redis> redis-cli info memory
```

## Causes probables

- **File Celery qui grossit** (workers arrêtés).
- **Clés sans expiration** qui s'accumulent.
- **Grosses clés** (listes, hash).

## Actions

1. File Celery : voir le runbook ObservaQueueBacklog.
2. Ajoutez des durées d'expiration (TTL) aux clés de cache.
3. Choisissez une politique adaptée : `allkeys-lru` pour un cache pur, `noeviction` pour une file.
4. Augmentez `maxmemory` si le serveur le permet.

## Vérifier le retour à la normale

- La mémoire redescend sous le seuil et reste stable.
