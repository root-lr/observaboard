# ObservaHighErrorRate — trop de réponses 5xx

**Sévérité :** critique · **Déclenchement :** part de réponses 5xx au-dessus du seuil pendant 5 minutes, avec au moins 3 requêtes par minute.
**Seuil :** 5 % par défaut, modifiable par service avec `alerts.error_rate_pct` dans `observa.yaml`.

## Ce que signifie l'alerte

Le service répond à une part anormale de requêtes par une erreur serveur (500, 502, 503, 504…). Les erreurs 4xx (requêtes invalides, 404) ne comptent pas.

## Impact pour les utilisateurs

Des pages ou des appels d'API échouent. Selon les routes touchées, cela peut aller d'une fonctionnalité secondaire à un service entièrement inutilisable.

## Dashboards à ouvrir

1. **Service HTTP** (lien `dashboard_url` de la notification) : rangées *Trafic* et *Routes* pour voir quelles routes échouent et depuis quand.
2. **Traces → Requêtes en erreur** dans le même dashboard : ouvrez une trace pour voir l'étape qui échoue (requête SQL, appel externe, exception).
3. **Logs du service** en bas du dashboard : cherchez la pile d'appels (traceback).

## Requêtes utiles

Routes qui renvoient des 5xx (Prometheus) :

```promql
topk(10, sum by (http_route, http_response_status_code) (
  rate(http_server_request_duration_seconds_count{project="PROJET", env="ENV", service="SERVICE", http_response_status_code=~"5.."}[5m])
))
```

Exceptions dans les logs (Loki) :

```logql
{project="PROJET", env="ENV", service="SERVICE"} |~ "(?i)(traceback|exception|error)"
```

Traces en erreur (Tempo, TraceQL) :

```traceql
{resource.service.name="SERVICE" && kind=server && status=error}
```

## Causes probables

- **Déploiement récent** : une régression dans le code. Comparez l'heure de début avec celle du dernier déploiement.
- **Dépendance indisponible** : base de données, Redis, API externe. Les traces montrent un span client en erreur ou en timeout.
- **Saturation** : workers gunicorn/uvicorn tous occupés, pool de connexions SQL épuisé. La latence monte en même temps.
- **Configuration** : variable d'environnement manquante, secret expiré, migration non appliquée.

## Actions

1. Si un déploiement vient d'avoir lieu et que les erreurs ont commencé juste après : **revenez à la version précédente**, puis analysez à froid.
2. Si une dépendance est en cause : vérifiez son état (dashboards PostgreSQL ou Redis, statut du fournisseur externe).
3. Si c'est une saturation : augmentez le nombre de workers ou de réplicas, et vérifiez le pool de connexions.
4. Corrigez la cause, puis ajoutez un test qui reproduit l'erreur.

## Vérifier le retour à la normale

- La courbe *Part de réponses 5xx et seuil* repasse sous le seuil et y reste 10 minutes.
- L'alerte passe à l'état *resolved* (une notification de résolution est envoyée).
- Plus de nouvelles traces dans *Requêtes en erreur*.
