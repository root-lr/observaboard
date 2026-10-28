# ObservaPostgresConnectionsHigh — connexions PostgreSQL presque épuisées

**Sévérité :** avertissement · **Déclenchement :** connexions ouvertes au-dessus du seuil de `max_connections` pendant 10 minutes.
**Seuil :** 80 % par défaut, modifiable avec `alerts.connections_pct` dans `observa.yaml`.

## Ce que signifie l'alerte

Le nombre de connexions approche la limite. Au-delà, PostgreSQL refuse les nouvelles connexions et les applications renvoient des erreurs.

## Impact pour les utilisateurs

Aucun pour l'instant ; erreurs franches si la limite est atteinte.

## Dashboards à ouvrir

1. **PostgreSQL** : *Connexions par état*. Beaucoup de connexions `idle` = connexions gardées ouvertes sans travail ; beaucoup de `idle in transaction` = transactions jamais terminées.
2. **Plus longue transaction active**.

## Requêtes utiles

```promql
sum by (state) (pg_stat_activity_count{project="PROJET", env="ENV", service="SERVICE"})
```

Dans la base, qui consomme les connexions :

```sql
SELECT application_name, usename, state, count(*)
FROM pg_stat_activity GROUP BY 1, 2, 3 ORDER BY 4 DESC;
```

## Causes probables

- **Trop de workers** applicatifs, chacun avec son pool de connexions.
- **Connexions persistantes** (`CONN_MAX_AGE` en Django) multipliées par le nombre de processus.
- **Fuite de connexions** ou transactions jamais fermées.

## Actions

1. Calculez le besoin : processus × connexions par processus, pour toutes les applications.
2. Réduisez `CONN_MAX_AGE` ou le nombre de workers, ou placez PgBouncer devant la base.
3. Terminez les transactions bloquées (`pg_terminate_backend`) après analyse.
4. En dernier recours, augmentez `max_connections` (coût mémoire).

## Vérifier le retour à la normale

- La part des connexions utilisées redescend sous le seuil.
