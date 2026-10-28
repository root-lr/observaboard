# ObservaHostDiskFull — disque presque plein

**Sévérité :** avertissement · **Déclenchement :** un système de fichiers du serveur est rempli à plus de 85 % depuis 10 minutes.

## Ce que signifie l'alerte

L'espace libre diminue. Au-delà de 95 %, les bases de données, Docker et les logs commencent à échouer.

## Impact pour les utilisateurs

Aucun tant qu'il reste de la place ; ensuite, erreurs d'écriture, base arrêtée, déploiements impossibles.

## Dashboards à ouvrir

1. **Serveurs** : *Disques remplis* pour voir la vitesse de remplissage.

## Requêtes utiles

Estimation : le disque sera-t-il plein dans 24 h ?

```promql
predict_linear(node_filesystem_avail_bytes{host="HOST", mountpoint="/"}[6h], 24 * 3600) < 0
```

Sur le serveur :

```bash
df -h
docker system df
sudo du -xh / --max-depth=2 2>/dev/null | sort -h | tail -20
```

## Causes probables

- **Images et conteneurs Docker inutilisés** accumulés au fil des déploiements.
- **Logs** de conteneurs sans rotation.
- **Base de données** qui grossit, sauvegardes locales qui s'accumulent.
- **Volumes** de données du central (si c'est le serveur central : rétention trop longue).

## Actions

1. Nettoyez Docker : `docker image prune -a` (images non utilisées), `docker builder prune`.
2. Activez la rotation des logs Docker (`max-size`, `max-file`).
3. Déplacez ou purgez les anciennes sauvegardes.
4. Sur le central : réduisez `retention` dans `central.yaml` puis `observa central reload`.
5. Agrandissez le disque si la croissance est normale.

## Vérifier le retour à la normale

- *Disques remplis* repasse sous 85 %.
