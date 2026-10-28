# ObservaContainerRestarting — redémarrages en boucle

**Sévérité :** avertissement · **Déclenchement :** au moins 3 redémarrages du même conteneur en 15 minutes.

## Ce que signifie l'alerte

Docker relance le conteneur (politique `restart:`) parce que son processus principal se termine. Le service est disponible par intermittence.

## Impact pour les utilisateurs

Erreurs ponctuelles, requêtes coupées, tâches interrompues, sessions perdues.

## Dashboards à ouvrir

1. **Service · conteneur** : panneau *Redémarrages*, *Mémoire* (montée jusqu'à la limite avant chaque redémarrage = OOM) et *Logs du service*.

## Requêtes utiles

```promql
changes(container_start_time_seconds{project="PROJET", env="ENV", service="SERVICE"}[1h])
```

```logql
{project="PROJET", env="ENV", service="SERVICE"} |~ "(?i)(error|exception|killed|fatal)"
```

```bash
docker inspect <conteneur> --format '{{.RestartCount}} {{.State.OOMKilled}} {{.State.ExitCode}}'
```

## Causes probables

- **Mémoire** : le conteneur atteint sa limite et est tué (`OOMKilled`).
- **Healthcheck** qui échoue et provoque un redémarrage.
- **Dépendance absente au démarrage** : l'app quitte si la base ou Redis ne répond pas encore.
- **Erreur de configuration** intermittente.

## Actions

1. Lisez les dernières lignes avant chaque redémarrage dans les logs.
2. OOM : augmentez la limite ou réduisez la consommation (nombre de workers, taille des caches).
3. Dépendance : ajoutez une attente ou des tentatives au démarrage, ou `depends_on` avec `condition: service_healthy`.

## Vérifier le retour à la normale

- Plus aucun redémarrage pendant 15 minutes.
- La mémoire reste stable sous la limite.
