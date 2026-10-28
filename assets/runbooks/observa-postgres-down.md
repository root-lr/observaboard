# ObservaPostgresDown — PostgreSQL injoignable

**Sévérité :** critique · **Déclenchement :** l'exporter de l'agent n'arrive plus à se connecter à la base depuis 2 minutes (`pg_up` = 0).

## Ce que signifie l'alerte

Soit PostgreSQL est arrêté ou refuse les connexions, soit l'agent ne peut plus l'atteindre (réseau `observa`, identifiants).

## Impact pour les utilisateurs

Si la base est réellement arrêtée, toutes les fonctionnalités qui en dépendent échouent : erreurs 5xx sur les API.

## Dashboards à ouvrir

1. **PostgreSQL** : *Joignable* et *Logs du service*.
2. **Service HTTP** des applications qui utilisent la base : si elles n'ont pas d'erreurs, le problème vient de la connexion de l'agent, pas de la base.

## Requêtes utiles

```logql
{project="PROJET", env="ENV", service="SERVICE"} |~ "(?i)(fatal|panic|error|could not)"
```

Sur le serveur :

```bash
docker compose ps SERVICE
docker logs --tail 50 observa-agent 2>&1 | grep -i postgres
docker exec -it <conteneur-postgres> pg_isready
```

## Causes probables

- **Base arrêtée** ou en cours de redémarrage.
- **Disque plein** : PostgreSQL s'arrête quand il ne peut plus écrire.
- **Trop de connexions** : `FATAL: sorry, too many clients already`.
- **Identifiants de l'agent invalides** : mot de passe changé, variable du `.env` de l'agent incorrecte.
- **Conteneur sorti du réseau `observa`** (override non appliqué).

## Actions

1. Base arrêtée : `docker compose up -d SERVICE` et lisez ses logs.
2. Disque plein : libérez de l'espace (voir le runbook ObservaHostDiskFull).
3. Identifiants : `observa agent env set <VARIABLE>='postgresql://...'` puis `observa agent reload`.

## Vérifier le retour à la normale

- *Joignable* repasse à 1 et les applications ne renvoient plus d'erreurs.
