# ObservaCentralDegraded — un composant du central ne répond plus

**Sévérité :** critique · **Déclenchement :** un composant du central (Prometheus, Alertmanager, Loki, Tempo, Grafana) ne répond plus au contrôle de Prometheus depuis 2 minutes.

## Ce que signifie l'alerte

Une partie de la plateforme d'observabilité est en panne : selon le composant, les logs, les traces ou les dashboards ne sont plus disponibles. Si Prometheus ou Alertmanager tombent, cette alerte ne peut pas partir : c'est le rôle du heartbeat externe (`notifications.heartbeat_url` dans `central.yaml`).

## Impact pour les utilisateurs

Aucun sur les applications surveillées. Vous perdez de la visibilité, et des données peuvent être perdues si la panne dure plus longtemps que le tampon des agents (environ 2 heures).

## Dashboards à ouvrir

Grafana peut être lui-même en cause : travaillez depuis le serveur central.

## Requêtes utiles

```bash
cd /opt/observa/central
docker compose ps
docker compose logs --tail 100 <composant>
observa doctor --central-dir /opt/observa/central
df -h
```

## Causes probables

- **Disque plein** sur le serveur central (les composants s'arrêtent).
- **Mémoire insuffisante** : le central a besoin d'environ 3 à 4 Go.
- **Configuration invalide** après une modification de `central.yaml`.
- **Mise à jour** d'un composant qui a échoué.

## Actions

1. Disque : réduisez la rétention ou agrandissez le disque.
2. Relancez le composant : `docker compose up -d <composant>`.
3. Configuration : `observa central reload` affiche les erreurs ; corrigez `central.yaml`.

## Vérifier le retour à la normale

- `docker compose ps` affiche tous les composants `healthy`.
- Les agents renvoient leurs données mises en tampon ; vérifiez la Vue d'ensemble.
