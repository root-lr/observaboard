# ObservaHighLatency — latence p95 trop élevée

**Sévérité :** avertissement · **Déclenchement :** latence p95 au-dessus du seuil pendant 10 minutes, avec au moins 3 requêtes par minute.
**Seuil :** 1 000 ms par défaut, modifiable par service avec `alerts.latency_p95_ms` dans `observa.yaml`.

## Ce que signifie l'alerte

5 % des requêtes prennent plus de temps que le seuil. Le service fonctionne, mais une partie des utilisateurs attend trop longtemps.

## Impact pour les utilisateurs

Pages lentes, délais d'attente côté client ou côté proxy, et à terme des erreurs 504 si la lenteur s'aggrave.

## Dashboards à ouvrir

1. **Service HTTP** : panneaux *Latence* et *Routes les plus lentes (p95)* pour savoir si toutes les routes sont touchées ou une seule.
2. Rangée **Dépendances** : *Temps passé en base de données* et *Appels sortants* montrent où part le temps.
3. **Traces → Requêtes lentes (> 1 s)** : ouvrez une trace, la barre la plus longue est la cause.
4. **Service · conteneur** : CPU et mémoire du conteneur, pour exclure une saturation.

## Requêtes utiles

Routes les plus lentes :

```promql
topk(5, histogram_quantile(0.95, sum by (http_route, le) (
  rate(http_server_request_duration_seconds_bucket{project="PROJET", env="ENV", service="SERVICE"}[5m])
)))
```

Temps passé en base par seconde (1 = un cœur entièrement occupé à attendre la base) :

```promql
sum(rate(traces_spanmetrics_latency_sum{service="SERVICE", span_kind="SPAN_KIND_CLIENT", db_system!=""}[5m]))
```

Traces lentes :

```traceql
{resource.service.name="SERVICE" && kind=server && duration > 1s}
```

## Causes probables

- **Requêtes SQL lentes ou trop nombreuses** (N+1 sur une vue Django) : beaucoup de spans SQL dans une même trace.
- **Appel externe lent** : un span client HTTP occupe la majorité de la trace.
- **Manque de ressources** : CPU du conteneur au maximum, mémoire proche de la limite, serveur chargé.
- **Verrous en base** : voir *Plus longue transaction active* dans le dashboard PostgreSQL.
- **Pic de trafic** : la courbe *Requêtes / s* a augmenté en même temps.

## Actions

1. Identifiez la route et l'étape lente dans une trace.
2. N+1 : ajoutez `select_related` / `prefetch_related` (Django) ou regroupez les requêtes.
3. Requête lente : ajoutez l'index manquant (`EXPLAIN ANALYZE`).
4. Appel externe : ajoutez un timeout, un cache ou passez l'appel en tâche asynchrone.
5. Saturation : augmentez les ressources ou le nombre de workers.

## Vérifier le retour à la normale

- La courbe p95 repasse sous la ligne *seuil d'alerte p95*.
- Les nouvelles traces de la route concernée sont plus courtes.
