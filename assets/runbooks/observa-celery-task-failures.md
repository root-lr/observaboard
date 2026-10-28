# ObservaCeleryTaskFailures — trop de tâches Celery en échec

**Sévérité :** avertissement · **Déclenchement :** part de tâches en échec au-dessus du seuil pendant 10 minutes, avec au moins une tâche toutes les 8 minutes.
**Seuil :** 10 % par défaut, modifiable avec `alerts.task_failure_pct` sur le service Celery dans `observa.yaml`.

## Ce que signifie l'alerte

Les tâches exécutées par le worker lèvent des exceptions. Le calcul vient des traces : chaque exécution de tâche est un span de type *consumer*, en erreur si la tâche a échoué.

## Impact pour les utilisateurs

Traitements asynchrones non réalisés : emails non envoyés, paiements non confirmés, exports manquants, selon les tâches touchées.

## Dashboards à ouvrir

1. **Worker Celery** : *Échecs / s par type* pour voir quelle tâche échoue.
2. **Traces → Tâches en échec** : l'exception est enregistrée dans la trace.
3. **Logs du service** du worker.

## Requêtes utiles

```promql
sum by (span_name) (rate(traces_spanmetrics_calls_total{service="SERVICE", span_kind="SPAN_KIND_CONSUMER", status_code="STATUS_CODE_ERROR"}[10m]))
```

```traceql
{resource.service.name="SERVICE" && kind=consumer && status=error}
```

## Causes probables

- **Bug dans une tâche** après un déploiement.
- **Dépendance indisponible** : API externe, base, stockage de fichiers.
- **Données inattendues** passées à la tâche.
- **Timeout** (`soft_time_limit`, `time_limit`) dépassé.

## Actions

1. Identifiez la tâche et l'exception dans une trace.
2. Corrigez, puis relancez les tâches perdues si elles ne sont pas réessayées automatiquement.
3. Pour les erreurs passagères (réseau), ajoutez `autoretry_for` avec un délai croissant.

## Vérifier le retour à la normale

- *Tâches en échec* repasse sous le seuil.
- La file d'attente (dashboard Redis ou Celery) ne grossit pas.
