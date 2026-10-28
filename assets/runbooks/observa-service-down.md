# ObservaServiceDown — un service ne tourne plus

**Sévérité :** critique · **Déclenchement :** le conteneur du service n'a plus été vu par l'agent depuis plus de 2 minutes.

## Ce que signifie l'alerte

Le conteneur portant les labels `observa.*` de ce service n'existe plus ou est arrêté sur le serveur indiqué (`host`). L'alerte reste active 24 heures si le service a été supprimé volontairement.

## Impact pour les utilisateurs

Tout ce que fournit ce service est indisponible : API, site, tâches de fond ou base de données selon le service.

## Dashboards à ouvrir

1. **Service · conteneur** : *Vu il y a* et *Redémarrages* ; les derniers logs avant l'arrêt sont dans *Logs du service*.
2. **Serveurs** : vérifiez que le serveur n'est pas saturé (mémoire, disque).

## Requêtes utiles

Dernières lignes de log avant l'arrêt :

```logql
{project="PROJET", env="ENV", service="SERVICE"}
```

Depuis quand le service n'est plus vu :

```promql
time() - observa:service:last_seen_timestamp{project="PROJET", env="ENV", service="SERVICE"}
```

Sur le serveur :

```bash
docker ps -a --filter label=observa.service=SERVICE
docker logs --tail 100 <conteneur>
docker inspect <conteneur> --format '{{.State.ExitCode}} {{.State.OOMKilled}} {{.State.Error}}'
```

## Causes probables

- **Crash au démarrage** : erreur de configuration, migration, dépendance absente. Code de sortie non nul.
- **Tué par manque de mémoire** : `OOMKilled` à `true`.
- **Arrêt manuel** ou déploiement interrompu (`docker compose down` sans `up`).
- **Serveur redémarré** sans politique `restart:` sur le service.
- **Suppression volontaire** du service.

## Actions

1. Relancez le service : `docker compose up -d SERVICE`, puis suivez ses logs.
2. OOM : augmentez la limite mémoire ou corrigez la fuite.
3. Ajoutez `restart: unless-stopped` au service si ce n'est pas déjà le cas.
4. Service supprimé volontairement : retirez-le de `observa.yaml`, relancez `observa apply`, et mettez l'alerte en silence pour 24 h (Grafana › Alerting › Silences, source Alertmanager).

## Vérifier le retour à la normale

- *Vu il y a* revient à quelques secondes.
- L'alerte est résolue après une minute.
