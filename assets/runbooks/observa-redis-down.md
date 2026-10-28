# ObservaRedisDown — Redis injoignable

**Sévérité :** critique · **Déclenchement :** l'exporter de l'agent n'arrive plus à se connecter à Redis depuis 2 minutes (`redis_up` = 0).

## Ce que signifie l'alerte

Redis est arrêté, refuse les connexions, ou l'agent ne peut plus l'atteindre (réseau `observa`, mot de passe).

## Impact pour les utilisateurs

Selon l'usage de Redis : cache désactivé (lenteur), sessions perdues (déconnexions), file Celery bloquée (tâches non exécutées).

## Dashboards à ouvrir

1. **Redis** : *Joignable*, *Mémoire*, *Logs du service*.
2. **Worker Celery** si Redis sert de broker.

## Requêtes utiles

```logql
{project="PROJET", env="ENV", service="SERVICE"}
```

```bash
docker compose ps SERVICE
docker exec -it <conteneur-redis> redis-cli ping
```

## Causes probables

- **Redis arrêté** : crash, OOM, redémarrage du serveur.
- **Mémoire** : Redis tué par le noyau.
- **Mot de passe** changé sans mettre à jour la variable de l'agent.

## Actions

1. Relancez Redis : `docker compose up -d SERVICE`.
2. Mot de passe : `observa agent env set <VARIABLE>=<motdepasse>` puis `observa agent reload`.

## Vérifier le retour à la normale

- *Joignable* repasse à 1, la file Celery se vide.
