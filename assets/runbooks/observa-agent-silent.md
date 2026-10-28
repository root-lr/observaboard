# ObservaAgentSilent — un serveur n'envoie plus de données

**Sévérité :** critique · **Déclenchement :** aucune métrique reçue de l'agent depuis plus de 5 minutes, alors qu'il en envoyait dans les 24 dernières heures.

## Ce que signifie l'alerte

Le central ne reçoit plus rien du serveur `host`. Les alertes ObservaServiceDown de ce serveur sont masquées tant que celle-ci est active, car elles en découlent.

## Impact pour les utilisateurs

Si le serveur est arrêté : tous ses services sont indisponibles. Si seul l'agent est en cause : aucun impact utilisateur, mais vous êtes aveugle sur ce serveur.

## Dashboards à ouvrir

1. **Serveurs** : la dernière valeur reçue indique l'heure de la coupure.
2. **Vue d'ensemble** : les autres serveurs sont-ils touchés ? Si tous le sont, regardez le central (runbook ObservaCentralDegraded).

## Requêtes utiles

Sur le serveur concerné :

```bash
observa agent status
docker logs --tail 100 observa-agent
curl -sS -u <agent>:<token> https://<central>/ingest/ping
```

## Causes probables

- **Serveur arrêté** ou injoignable (panne, redémarrage, coupure réseau).
- **Agent arrêté** : `observa-agent` absent de `docker ps`.
- **Token révoqué** : les envois reçoivent 401 (visible dans les logs de l'agent).
- **Disque plein** sur le serveur.
- **Certificat du central** non reconnu (mode IP sans `--ca-file`).

## Actions

1. Vérifiez que le serveur répond (SSH, console de l'hébergeur).
2. Relancez l'agent : `cd /opt/observa/agent && docker compose up -d`.
3. Token : créez-en un nouveau sur le central (`observa token create <agent>`), puis réinstallez l'agent avec.
4. Serveur retiré volontairement : révoquez son token et mettez l'alerte en silence 24 h (Grafana › Alerting › Silences, source Alertmanager).

## Vérifier le retour à la normale

- Les métriques du serveur réapparaissent dans le dashboard Serveurs ; l'alerte se résout en une minute.
- L'agent rattrape automatiquement les données mises en tampon pendant une coupure courte.
